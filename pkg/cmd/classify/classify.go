package classify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/sonoteller"
	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug       bool
	DBType      string
	DBConn      string
	Timeout     time.Duration
	Concurrency int
	Limit       int
	Proxy       string

	Type string
}

// Run launches the classification process
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Printf("classify: process started\n")
	defer func() {
		log.Printf("classify: process ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...any) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("classify: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("classify: couldn't start orm store: %w", err)
	}

	// Create a sonoteller client
	sonoClient, err := sonoteller.New(&sonoteller.Config{
		Wait:  1 * time.Second,
		Debug: cfg.Debug,
		Proxy: cfg.Proxy,
	})
	if err != nil {
		return fmt.Errorf("classify: couldn't create sonoteller client: %w", err)
	}

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("classify: total time %s, average time %s\n", total, total/time.Duration(iteration))
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

	var songs []*storage.Song
	var currID string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("classify: %w", ctx.Err())
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
				return fmt.Errorf("classify: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("classify: iteration %d\n", iteration)
			}

			// Get next song
			filters := []storage.Filter{
				storage.Where("classified = ?", false),
				storage.Where("state = ?", storage.Used),
				storage.Where("youtube_id != ''"),
				storage.Where("songs.id > ?", currID),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
			}

			// Get next song
			if len(songs) == 0 {
				// Get a song
				songs, err = list(ctx, store, currID, filters...)
				if err != nil {
					return err
				}
				currID = songs[len(songs)-1].ID
			}
			song := songs[0]
			songs = songs[1:]

			// Launch process in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("classify: start %s", song.ID)
				err := classify(ctx, song, debug, store, sonoClient)
				if err != nil {
					log.Println(err)
				}
				debug("classify: end %s", song.ID)
				errC <- err
			}()
		}
	}
}

func list(ctx context.Context, store *storage.Store, currID string, filters ...storage.Filter) ([]*storage.Song, error) {
	// Get a song
	next := append(filters, storage.Where("songs.id > ?", currID))
	songs, err := store.ListSongs(ctx, 1, 100, "", next...)
	if err != nil {
		return nil, fmt.Errorf("classify: couldn't get song from database: %w", err)
	}
	if len(songs) > 0 {
		return songs, nil
	}
	songs, err = store.ListSongs(ctx, 1, 100, "", filters...)
	if err != nil {
		return nil, fmt.Errorf("classify: couldn't get song from database: %w", err)
	}
	if len(songs) == 0 {
		return nil, errors.New("classify: no songs to process")
	}
	return songs, nil
}

func classify(ctx context.Context, song *storage.Song, debug func(string, ...any), store *storage.Store, sonoClient *sonoteller.Client) error {
	if song.YoutubeID == "" {
		return fmt.Errorf("classify: song %s has no youtube id", song.ID)
	}
	analysis, err := sonoClient.Analyze(ctx, song.YoutubeID)
	if err != nil {
		return fmt.Errorf("classify: couldn't analyze song %s: %w", song.ID, err)
	}
	js, err := json.Marshal(analysis)
	if err != nil {
		return fmt.Errorf("classify: couldn't marshal analysis %v: %w", analysis, err)
	}
	debug("classify: %s", js)
	song.Classification = string(js)
	song.Classified = true
	if err := store.SetSong(ctx, song); err != nil {
		return fmt.Errorf("classify: couldn't update song: %w", err)
	}
	return nil
}
