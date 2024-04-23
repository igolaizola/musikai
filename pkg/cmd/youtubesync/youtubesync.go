package youtubesync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/igolaizola/musikai/pkg/youtube"
)

type Config struct {
	Debug  bool
	DBType string
	DBConn string
	Proxy  string

	Timeout     time.Duration
	Concurrency int

	Key      string
	Channels string
	From     string
}

// Run launches the song generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("youtubesync: process started")
	defer func() {
		log.Printf("youtubesync: process ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...interface{}) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	from := time.Now().AddDate(-1, 0, 0)
	if cfg.From != "" {
		var err error
		from, err = time.Parse("2006-01-02", cfg.From)
		if err != nil {
			return fmt.Errorf("youtubesync: couldn't parse from date: %w", err)
		}
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("youtubesync: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("youtubesync: couldn't start orm store: %w", err)
	}

	// Create youtube client
	client, err := youtube.New(ctx, cfg.Key, cfg.Debug)
	if err != nil {
		return fmt.Errorf("youtubesync: couldn't create youtube client: %w", err)
	}

	// Get channels
	channels := strings.Split(cfg.Channels, ",")
	if len(channels) == 0 {
		return errors.New("youtubesync: no channels to process")
	}
	for i := 0; i < len(channels); i++ {
		channels[i] = strings.TrimSpace(channels[i])
	}

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("youtubesync: total time %s, average time %s\n", total, total/time.Duration(iteration))
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

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("youtubesync: %w", ctx.Err())
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
				return fmt.Errorf("youtubesync: too many consecutive errors: %w", err)
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("youtubesync: iteration %d\n", iteration)
			}

			// Get next channel
			if len(channels) == 0 {
				return errors.New("download: no more channels to process")
			}
			channel := channels[0]
			channels = channels[1:]

			// Launch sync in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("youtubesync: start %s", channel)
				err := syncChannel(ctx, client, store, from, channel)
				if err != nil {
					log.Println(err)
				}
				debug("youtubesync: end %s", channel)
				errC <- err
			}()
		}
	}
}

func syncChannel(ctx context.Context, c *youtube.Client, store *storage.Store, from time.Time, channel string) error {
	videos, err := c.GetVideos(ctx, channel, from)
	if err != nil {
		return fmt.Errorf("youtubesync: couldn't get videos: %w", err)
	}
	for _, video := range videos {
		songs, err := store.ListSongs(ctx, 1, 1, "",
			storage.Where("title = ?", video.Title),
			storage.Where("state = ?", storage.Used),
		)
		if err != nil {
			return fmt.Errorf("youtubesync: couldn't list songs: %w", err)
		}
		if len(songs) == 0 {
			log.Printf("youtubesync: song not found %q\n", video.Title)
			continue
		}
		song := songs[0]
		if song.YoutubeID != "" {
			if song.YoutubeID != video.ID {
				log.Printf("youtubesync: song %q has different youtube id %q != %q\n", song.Title, song.YoutubeID, video.ID)
			}
			continue
		}
		song.YoutubeID = video.ID
		if err := store.SetSong(ctx, song); err != nil {
			return fmt.Errorf("youtubesync: couldn't update song: %w", err)
		}
	}
	return nil
}
