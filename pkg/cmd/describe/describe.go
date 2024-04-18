package describe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/openai"
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

	Type  string
	Key   string
	Model string
}

// Run launches the classification process
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Printf("describe: process started\n")
	defer func() {
		log.Printf("describe: process ended (%d)\n", iteration)
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
		return fmt.Errorf("describe: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("describe: couldn't start orm store: %w", err)
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

	// Create a sonoteller client
	openaiClient := openai.New(&openai.Config{
		Debug: cfg.Debug,
		Token: cfg.Key,
		Model: cfg.Model,
	})

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("describe: total time %s, average time %s\n", total, total/time.Duration(iteration))
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
			return fmt.Errorf("describe: %w", ctx.Err())
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
				return fmt.Errorf("describe: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("describe: iteration %d\n", iteration)
			}

			// Get next song
			filters := []storage.Filter{
				storage.Where("described = ?", false),
				storage.Where("classified = ?", true),
				storage.Where("state = ?", storage.Used),
				storage.Where("songs.id > ?", currID),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
			}

			// Get next song
			if len(songs) == 0 {
				// Get a song
				songs, err = store.ListSongs(ctx, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("describe: couldn't get song from database: %w", err)
				}
				if len(songs) == 0 {
					return errors.New("describe: no songs to process")
				}
				currID = songs[len(songs)-1].ID
			}
			song := songs[0]
			songs = songs[1:]

			// Launch process in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("describe: start %s", song.ID)
				err := describe(ctx, song, debug, store, openaiClient)
				if err != nil {
					log.Println(err)
				}
				debug("describe: end %s", song.ID)
				errC <- err
			}()
		}
	}
}

type metadata struct {
	Style       string         `json:"style"`
	BPM         int            `json:"bpm"`
	SubStyles   map[string]int `json:"substyles"`
	Instruments []string       `json:"instruments"`
	Moods       map[string]int `json:"moods"`
	Genres      map[string]int `json:"genres"`
}

func describe(ctx context.Context, song *storage.Song, debug func(string, ...any), store *storage.Store, openaiClient *openai.Client) error {
	if song.Youtube == "" {
		return fmt.Errorf("describe: song %s has no youtube id", song.ID)
	}
	var analysis sonoteller.Analysis
	if err := json.Unmarshal([]byte(song.Classification), &analysis); err != nil {
		return fmt.Errorf("describe: couldn't unmarshal analysis %s: %w", song.Classification, err)
	}
	music := analysis.Music
	md := metadata{
		Style:       song.Style,
		BPM:         int(music.BPM),
		SubStyles:   music.Styles,
		Instruments: music.Instruments,
		Moods:       music.Moods,
		Genres:      music.Genres,
	}
	js, err := json.MarshalIndent(md, "", "  ")
	if err != nil {
		return fmt.Errorf("describe: couldn't marshal metadata %v: %w", md, err)
	}
	msg := fmt.Sprintf("Use this song metadata keywords to generate a song metadata description of max 2 sentences:\n%s", js)
	description, err := openaiClient.ChatCompletion(ctx, msg)
	if err != nil {
		return fmt.Errorf("describe: couldn't create chat completion: %w", err)
	}
	debug("describe: %s", description)
	song.Description = description
	if err := store.SetSong(ctx, song); err != nil {
		return fmt.Errorf("describe: couldn't update song: %w", err)
	}
	return nil
}
