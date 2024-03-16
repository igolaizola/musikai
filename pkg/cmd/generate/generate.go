package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/sound/aubio"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/igolaizola/musikai/pkg/suno"
	"github.com/oklog/ulid/v2"
)

type Config struct {
	Debug       bool
	DBType      string
	DBConn      string
	Timeout     time.Duration
	Concurrency int
	WaitMin     time.Duration
	WaitMax     time.Duration
	Limit       int
	Proxy       string

	Account      string
	Type         string
	Prompt       string
	Style        string
	Instrumental bool
	Notes        string

	EndLyrics      string
	EndStyle       string
	EndStyleAppend bool
	ForceEndLyrics string
	ForceEndStyle  string
	MinDuration    time.Duration
	MaxDuration    time.Duration
	MaxExtensions  int
}

// Run launches the song generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("generate: process started")
	defer func() {
		log.Printf("generate: process ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...interface{}) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	if _, err := aubio.Version(ctx); err != nil {
		return fmt.Errorf("generate: couldn't get aubio version: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("generate: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("generate: couldn't start orm store: %w", err)
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
	generator := suno.New(&suno.Config{
		Wait:           4 * time.Second,
		Debug:          cfg.Debug,
		Client:         httpClient,
		CookieStore:    store.NewCookieStore("suno", cfg.Account),
		Parallel:       cfg.Limit == 1,
		EndLyrics:      cfg.EndLyrics,
		EndStyle:       cfg.EndStyle,
		EndStyleAppend: cfg.EndStyleAppend,
		ForceEndLyrics: cfg.ForceEndLyrics,
		ForceEndStyle:  cfg.ForceEndStyle,
		MinDuration:    cfg.MinDuration,
		MaxDuration:    cfg.MaxDuration,
		MaxExtensions:  cfg.MaxExtensions,
	})
	if err := generator.Start(ctx); err != nil {
		return fmt.Errorf("generate: couldn't start suno generator: %w", err)
	}
	defer func() {
		if err := generator.Stop(ctx); err != nil {
			log.Printf("generate: couldn't stop suno generator: %v\n", err)
		}
	}()

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("generate: total time %s, average time %s\n", total, total/time.Duration(iteration))
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
			return fmt.Errorf("generate: %w", ctx.Err())
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
				return fmt.Errorf("generate: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("generate: iteration %d\n", iteration)
			}

			// Wait for a random time.
			wait := 1 * time.Second
			if iteration > 1 {
				wait = time.Duration(rand.Int63n(int64(cfg.WaitMax-cfg.WaitMin))) + cfg.WaitMin
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("generate: %w", ctx.Err())
			case <-time.After(wait):
			}

			// Get a template
			tmpl := template{
				Type:         cfg.Type,
				Prompt:       cfg.Prompt,
				Style:        cfg.Style,
				Instrumental: cfg.Instrumental,
			}
			if tmpl.Prompt == "" && tmpl.Style == "" {
				tmpl = nextTemplate()
			}

			// Launch generate in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("generate: start %s", tmpl)
				err := generate(ctx, generator, store, tmpl, cfg.Notes)
				if err != nil {
					log.Println(err)
				}
				debug("generate: end %s", tmpl)
				errC <- err
			}()
		}
	}
}

func generate(ctx context.Context, generator *suno.Client, store *storage.Store, t template, notes string) error {
	// Generate the songs.
	songs, err := generator.Generate(ctx, t.Prompt, t.Style, t.Title, t.Instrumental)
	if err != nil {
		return fmt.Errorf("generate: couldn't generate song %s: %w", t, err)
	}

	// Save the generated songs to the database.
	for _, gens := range songs {
		if len(gens) == 0 {
			continue
		}
		song := &storage.Song{
			ID:           ulid.Make().String(),
			Type:         t.Type,
			Notes:        notes,
			Prompt:       t.Prompt,
			Style:        gens[0].Style,
			Instrumental: t.Instrumental,
		}
		if err := store.SetSong(ctx, song); err != nil {
			return fmt.Errorf("generate: couldn't save song to database: %w", err)
		}
		var firstGenID string
		for i, g := range gens {
			genID := ulid.Make().String()
			if i == 0 {
				firstGenID = genID
			}
			history, err := json.Marshal(g.History)
			if err != nil {
				return fmt.Errorf("generate: couldn't marshal history: %w", err)
			}
			if err := store.SetGeneration(ctx, &storage.Generation{
				ID:          genID,
				SongID:      &song.ID,
				SunoID:      g.ID,
				SunoAudio:   g.Audio,
				SunoImage:   g.Image,
				SunoTitle:   g.Title,
				SunoHistory: string(history),
				Duration:    g.Duration,
			}); err != nil {
				return fmt.Errorf("generate: couldn't save generation to database: %w", err)
			}
		}
		song.GenerationID = &firstGenID
		if err := store.SetSong(ctx, song); err != nil {
			return fmt.Errorf("generate: couldn't save song to database: %w", err)
		}
	}
	return nil
}
