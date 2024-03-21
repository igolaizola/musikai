package sync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/distrokid"
	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug  bool
	DBType string
	DBConn string
	Proxy  string

	Timeout     time.Duration
	Concurrency int
	WaitMin     time.Duration
	WaitMax     time.Duration
	Limit       int
	Account     string
}

// Run launches the song generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("sync: process started")
	defer func() {
		log.Printf("sync: process ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...interface{}) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("sync: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("sync: couldn't start orm store: %w", err)
	}

	httpClient := &http.Client{
		Timeout: 2 * time.Minute,
	}
	if cfg.Proxy != "" {
		u, err := url.Parse(cfg.Proxy)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}
		httpClient.Transport = &http.Transport{
			Proxy: http.ProxyURL(u),
		}
	}
	client := distrokid.New(&distrokid.Config{
		Wait:        4 * time.Second,
		Debug:       cfg.Debug,
		Client:      httpClient,
		CookieStore: store.NewCookieStore("distrokid", cfg.Account),
	})
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("sync: couldn't start distrokid client: %w", err)
	}
	defer func() {
		if err := client.Stop(context.Background()); err != nil {
			log.Printf("sync: couldn't stop distrokid client: %v\n", err)
		}
	}()

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("sync: total time %s, average time %s\n", total, total/time.Duration(iteration))
	}()

	nErr := 0
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 24 * time.Hour
	}
	ticker := time.NewTicker(timeout)
	last := time.Now()
	defer ticker.Stop()

	// Concurrency settings
	concurrency := cfg.Concurrency
	if concurrency == 0 {
		concurrency = 1
	}
	errC := make(chan error, concurrency)
	defer close(errC)
	for i := 0; i < concurrency; i++ {
		errC <- nil
	}
	var wg sync.WaitGroup
	defer wg.Wait()

	var albums []*storage.Album
	var currID string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("sync: %w", ctx.Err())
		case <-ticker.C:
			return nil
		case err := <-errC:
			if err != nil {
				nErr += 1
			} else {
				nErr = 0
			}

			// Check exit conditions
			if nErr > 10 {
				return fmt.Errorf("sync: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("sync: iteration %d\n", iteration)
			}

			// Get next albums
			filters := []storage.Filter{
				storage.Where("id > ?", currID),
				storage.Where("state = ?", storage.Used),
				storage.Where("upc = ''"),
			}

			// Get next image
			if len(albums) == 0 {
				// Get a albums from the database.
				var err error
				albums, err = store.ListAlbums(ctx, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("sync: couldn't get album from database: %w", err)
				}
				if len(albums) == 0 {
					return errors.New("sync: no albums to process")
				}
				currID = albums[len(albums)-1].ID
			}
			album := albums[0]
			albums = albums[1:]

			// Launch sync in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("sync: start %s %s", album.ID, album.Title)
				err := syncAlbum(ctx, client, store, album)
				if err != nil {
					log.Println(err)
				}
				debug("sync: end %s%s", album.ID, album.Title)
				errC <- err
			}()
		}
	}
}

func syncAlbum(ctx context.Context, c *distrokid.Client, store *storage.Store, album *storage.Album) error {
	resp, err := c.Album(ctx, album.DistrokidID)
	if err != nil {
		return fmt.Errorf("sync: album %s: %w", album.DistrokidID, err)
	}

	// Get songs for album
	songs, err := store.ListSongs(ctx, 1, 100, "", storage.Where("album_id = ?", album.ID))
	if err != nil {
		return fmt.Errorf("sync: couldn't get songs: %w", err)
	}

	// Order songs by track number
	sort.Slice(songs, func(i, j int) bool {
		return songs[i].Order < songs[j].Order
	})

	// Check if all songs are in distrokid
	if len(songs) != len(resp.ISRCs) {
		return fmt.Errorf("sync: album %s songs mismatch: %d != %d", album.DistrokidID, len(songs), len(resp.ISRCs))
	}

	for i, song := range songs {
		song.ISRC = resp.ISRCs[i]
		if err := store.SetSong(ctx, song); err != nil {
			return fmt.Errorf("sync: couldn't set song: %w", err)
		}
	}
	album.UPC = resp.UPC
	if err := store.SetAlbum(ctx, album); err != nil {
		return fmt.Errorf("sync: couldn't set album: %w", err)
	}
	log.Println("sync: album synced", album.ID, album.Title, album.UPC)
	return nil
}
