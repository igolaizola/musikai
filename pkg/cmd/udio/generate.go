package udio

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gocarina/gocsv"
	"github.com/igolaizola/musikai/pkg/sound/aubio"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/igolaizola/musikai/pkg/udio"
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
	Input        string
	Type         string
	Prompt       string
	Style        string
	Instrumental bool
	Notes        string

	MinDuration   time.Duration
	MaxDuration   time.Duration
	MaxExtensions int

	NopechaKey string
}

type input struct {
	Weight       int    `json:"weight" csv:"weight"`
	Type         string `json:"type" csv:"type"`
	Prompt       string `json:"prompt" csv:"prompt"`
	Style        string `json:"style" csv:"style"`
	Instrumental bool   `json:"instrumental" csv:"instrumental"`
}

// Run launches the song generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("udio: process started")
	defer func() {
		log.Printf("udio: process ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...interface{}) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	if _, err := aubio.Version(ctx); err != nil {
		return fmt.Errorf("udio: couldn't get aubio version: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("udio: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("udio: couldn't start orm store: %w", err)
	}

	// Get the template function
	var fn func() template
	if cfg.Input != "" {
		fn, err = toTemplateFunc(cfg.Input)
		if err != nil {
			return err
		}
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
	generator := udio.New(&udio.Config{
		Wait:          4 * time.Second,
		Debug:         cfg.Debug,
		Client:        httpClient,
		CookieStore:   store.NewCookieStore("udio", cfg.Account),
		MinDuration:   cfg.MinDuration,
		MaxDuration:   cfg.MaxDuration,
		MaxExtensions: cfg.MaxExtensions,
		NopechaKey:    cfg.NopechaKey,
	})
	if err := generator.Start(ctx); err != nil {
		return fmt.Errorf("udio: couldn't start udio generator: %w", err)
	}
	defer func() {
		if err := generator.Stop(ctx); err != nil {
			log.Printf("udio: couldn't stop udio generator: %v\n", err)
		}
	}()

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("udio: total time %s, average time %s\n", total, total/time.Duration(iteration))
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
			return fmt.Errorf("udio: %w", ctx.Err())
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
				return fmt.Errorf("udio: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("udio: iteration %d\n", iteration)
			}

			// Wait for a random time.
			wait := 1 * time.Second
			if iteration > 1 {
				wait = time.Duration(rand.Int63n(int64(cfg.WaitMax-cfg.WaitMin))) + cfg.WaitMin
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("udio: %w", ctx.Err())
			case <-time.After(wait):
			}

			// Get a template
			var tmpl template
			if fn != nil {
				tmpl = fn()
			} else {
				tmpl = template{
					Type:         cfg.Type,
					Prompt:       cfg.Prompt,
					Style:        cfg.Style,
					Instrumental: cfg.Instrumental,
				}
				if tmpl.Prompt == "" && tmpl.Style == "" {
					tmpl = nextTemplate()
				}
			}

			// Launch generate in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("udio: start %s", tmpl)
				err := generate(ctx, generator, store, tmpl, cfg.Notes)
				if err != nil {
					log.Println(err)
				}
				debug("udio: end %s", tmpl)
				errC <- err
			}()
		}
	}
}

func generate(ctx context.Context, generator *udio.Client, store *storage.Store, t template, notes string) error {
	// Generate the songs.
	resp, err := generator.Generate(ctx, "pop rock", "")
	if err != nil {
		return fmt.Errorf("udio: couldn't generate song %s: %w", t, err)
	}
	js, _ := json.MarshalIndent(resp, "", "  ")
	log.Printf("udio: %s\n", js)

	_ = store
	_ = notes
	return nil
}

func toTemplateFunc(file string) (func() template, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("udio: couldn't read input file: %w", err)
	}

	ext := filepath.Ext(file)
	var unmarshal func([]byte) ([]*input, error)
	switch ext {
	case ".json":
		unmarshal = func(b []byte) ([]*input, error) {
			var is []*input
			if err := json.Unmarshal(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	case ".csv":
		unmarshal = func(b []byte) ([]*input, error) {
			var is []*input
			if err := gocsv.UnmarshalBytes(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	default:
		return nil, fmt.Errorf("udio: unsupported output format: %s", ext)
	}
	inputs, err := unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("udio: couldn't unmarshal input: %w", err)
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("udio: no inputs found in file")
	}
	var opts []template
	for _, i := range inputs {
		w := i.Weight
		if w <= 0 {
			w = 1
		}
		if i.Prompt == "" && i.Style == "" {
			log.Println("udio: skipping empty input")
			continue
		}
		opts = append(opts, options(w, template{
			Type:         i.Type,
			Prompt:       i.Prompt,
			Style:        i.Style,
			Instrumental: i.Instrumental,
		})...)
	}
	return func() template {
		return opts[rand.Intn(len(opts))]
	}, nil
}
