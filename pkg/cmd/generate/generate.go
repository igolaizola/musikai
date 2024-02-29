package generate

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"sync"
	"time"

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

	Account string
	Type    string
	Prompt  string
	Style   string
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
		Wait:        4 * time.Second,
		Debug:       cfg.Debug,
		Client:      http.DefaultClient,
		CookieStore: store.NewCookieStore("suno", cfg.Account),
	})
	if err := generator.Start(ctx); err != nil {
		return fmt.Errorf("generate: couldn't start suno generator: %w", err)
	}
	defer func() {
		if err := generator.Stop(ctx); err != nil {
			log.Printf("generate: couldn't stop suno generator: %v\n", err)
		}
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
				Type:   cfg.Type,
				Prompt: cfg.Prompt,
				Style:  cfg.Style,
			}
			if tmpl.Prompt == "" && tmpl.Style == "" {
				tmpl = nextTemplate()
			}

			// Launch generate in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("generate: start %s", tmpl)
				err := generate(ctx, generator, store, tmpl)
				if err != nil {
					log.Println(err)
				}
				debug("generate: end %s", tmpl)
				errC <- err
			}()
		}
	}
}

func generate(ctx context.Context, generator *suno.Client, store *storage.Store, t template) error {
	// Generate the songs.
	songs, err := generator.Generate(ctx, t.Prompt, t.Style, t.Title, t.Instrumental)
	if err != nil {
		return fmt.Errorf("generate: couldn't generate song %s: %w", t, err)
	}

	// Save the generated songs to the database.
	for _, s := range songs {
		if err := store.SetSong(ctx, &storage.Song{
			ID:        ulid.Make().String(),
			Type:      t.Type,
			Prompt:    t.Prompt,
			Style:     t.Style,
			Title:     t.Title,
			SunoID:    s.ID,
			SunoAudio: s.Audio,
			SunoImage: s.Image,
		}); err != nil {
			return fmt.Errorf("generate: couldn't save song to database: %w", err)
		}
	}
	return nil
}
