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

	"github.com/igolaizola/musikai/pkg/spotify"
	"github.com/igolaizola/musikai/pkg/storage"
)

// RunSpotify syncs albums and songs data from spotify.
func RunSpotify(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("sync-spotify: process started")
	defer func() {
		log.Printf("sync-spotify: process ended (%d)\n", iteration)
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
		return fmt.Errorf("sync-spotify: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("sync-spotify: couldn't start orm store: %w", err)
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
		return errors.New("sync-spotify: missing spotify credentials")
	}
	spClient := spotify.New(&spotify.Config{
		Wait:         1 * time.Second,
		Debug:        cfg.Debug,
		Client:       httpClient,
		ClientID:     cfg.SpotifyID,
		ClientSecret: cfg.SpotifySecret,
	})

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("sync-spotify: total time %s, average time %s\n", total, total/time.Duration(iteration))
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
			return fmt.Errorf("sync-spotify: %w", ctx.Err())
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
				return fmt.Errorf("sync-spotify: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("sync-spotify: iteration %d\n", iteration)
			}

			// Get next albums
			filters := []storage.Filter{
				storage.Where("id > ?", currID),
				storage.Where("state = ?", storage.Used),
				storage.Where("spotify_id != ''"),
				storage.Where("exists (select 1 from songs where album_id = albums.id and spotify_analysis = '')"),
			}

			// Get next image
			if len(albums) == 0 {
				// Get a albums from the database.
				var err error
				albums, err = store.ListAlbums(ctx, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("sync-spotify: couldn't get album from database: %w", err)
				}
				if len(albums) == 0 {
					return errors.New("sync-spotify: no albums to process")
				}
				currID = albums[len(albums)-1].ID
			}
			album := albums[0]
			albums = albums[1:]

			// Launch sync in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("sync-spotify: start %s %s", album.ID, album.FullTitle())
				err := syncSpotify(ctx, spClient, store, album)
				if err != nil {
					log.Println(err)
				}
				debug("sync-spotify: end %s%s", album.ID, album.FullTitle())
				errC <- err
			}()
		}
	}
}

func syncSpotify(ctx context.Context, sp *spotify.Client, store *storage.Store, album *storage.Album) error {
	// Get songs for album
	songs, err := store.ListSongs(ctx, 1, 100, "", storage.Where("album_id = ?", album.ID))
	if err != nil {
		return fmt.Errorf("sync-spotify: couldn't get songs: %w", err)
	}

	// Order songs by track number
	sort.Slice(songs, func(i, j int) bool {
		return songs[i].Order < songs[j].Order
	})

	// Get spotify tracks
	tracks, err := sp.AlbumTracks(ctx, album.SpotifyID)
	if err != nil {
		return fmt.Errorf("sync-spotify: couldn't get album tracks: %w", err)
	}

	// Check if all tracks are in spotify
	if len(tracks) != len(songs) {
		return fmt.Errorf("sync-spotify: album %s tracks mismatch: %d != %d", album.SpotifyID, len(tracks), len(songs))
	}

	// Order tracks by track number
	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].Number < tracks[j].Number
	})

	// Set song values
	for i, song := range songs {
		if tracks[i].Name != song.Title {
			return fmt.Errorf("sync-spotify: album %s track mismatch: %s != %s", album.SpotifyID, tracks[i].Name, song.Title)
		}
		if song.SpotifyID != "" && song.SpotifyAnalysis != "" {
			continue
		}
		song.SpotifyID = tracks[i].ID
		analysis, err := sp.AudioFeatures(ctx, song.SpotifyID)
		if err != nil {
			return fmt.Errorf("sync-spotify: couldn't get song analysis (%s / %s): %w", album.FullTitle(), song.Title, err)
		}
		js, err := json.Marshal(analysis)
		if err != nil {
			return fmt.Errorf("sync-spotify: couldn't marshal song analysis: %w", err)
		}
		song.SpotifyAnalysis = string(js)

		// Update on database
		if err := store.SetSong(ctx, song); err != nil {
			return fmt.Errorf("sync-spotify: couldn't set song: %w", err)
		}
	}

	log.Println("sync-spotify: album synced", album.ID, album.FullTitle(), album.UPC, album.SpotifyID, album.AppleID)
	return nil
}
