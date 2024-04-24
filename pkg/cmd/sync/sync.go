package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/distrokid"
	"github.com/igolaizola/musikai/pkg/spotify"
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

	SpotifyID     string
	SpotifySecret string
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

	if cfg.SpotifyID == "" || cfg.SpotifySecret == "" {
		return errors.New("sync: missing spotify credentials")
	}
	spClient := spotify.New(&spotify.Config{
		Wait:         1 * time.Second,
		Debug:        cfg.Debug,
		Client:       httpClient,
		ClientID:     cfg.SpotifyID,
		ClientSecret: cfg.SpotifySecret,
	})

	dkClient := distrokid.New(&distrokid.Config{
		Wait:        4 * time.Second,
		Debug:       cfg.Debug,
		Client:      httpClient,
		CookieStore: store.NewCookieStore("distrokid", cfg.Account),
	})
	if err := dkClient.Start(ctx); err != nil {
		return fmt.Errorf("sync: couldn't start distrokid client: %w", err)
	}
	defer func() {
		if err := dkClient.Stop(context.Background()); err != nil {
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
				storage.Where("spotify_id = ''"),
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
				err := syncAlbum(ctx, dkClient, spClient, store, album)
				if err != nil {
					log.Println(err)
				}
				debug("sync: end %s%s", album.ID, album.Title)
				errC <- err
			}()
		}
	}
}

func syncAlbum(ctx context.Context, dk *distrokid.Client, sp *spotify.Client, store *storage.Store, album *storage.Album) error {
	resp, err := dk.Album(ctx, album.DistrokidID)
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

	// Get spotify tracks
	tracks, err := sp.AlbumTracks(ctx, resp.SpotifyID)
	if err != nil {
		return fmt.Errorf("sync: couldn't get album tracks: %w", err)
	}

	// Check if all tracks are in spotify
	if len(tracks) != len(songs) {
		return fmt.Errorf("sync: album %s tracks mismatch: %d != %d", album.SpotifyID, len(tracks), len(songs))
	}

	// Order tracks by track number
	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].Number < tracks[j].Number
	})

	// Set song values
	for i, song := range songs {
		if tracks[i].Name != song.Title {
			return fmt.Errorf("sync: album %s track mismatch: %s != %s", album.SpotifyID, tracks[i].Name, song.Title)
		}
		song.SpotifyID = tracks[i].ID
		song.ISRC = resp.ISRCs[i]
		analysis, err := sp.AudioFeatures(ctx, song.SpotifyID)
		if err != nil {
			return fmt.Errorf("sync: couldn't get song analysis (%s / %s): %w", album.FullTitle(), song.Title, err)
		}
		js, err := json.Marshal(analysis)
		if err != nil {
			return fmt.Errorf("sync: couldn't marshal song analysis: %w", err)
		}
		song.SpotifyAnalysis = string(js)
	}

	// Update on database
	for _, song := range songs {
		if err := store.SetSong(ctx, song); err != nil {
			return fmt.Errorf("sync: couldn't set song: %w", err)
		}
	}
	album.UPC = resp.UPC
	album.SpotifyID = resp.SpotifyID
	album.AppleID = resp.AppleID
	if err := store.SetAlbum(ctx, album); err != nil {
		return fmt.Errorf("sync: couldn't set album: %w", err)
	}
	log.Println("sync: album synced", album.ID, album.Title, album.UPC, album.SpotifyID, album.AppleID)
	return nil
}
