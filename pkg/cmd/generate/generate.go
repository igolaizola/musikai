package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gocarina/gocsv"
	"github.com/igolaizola/musikai/pkg/music"
	"github.com/igolaizola/musikai/pkg/ngrok"
	"github.com/igolaizola/musikai/pkg/sound/aubio"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/igolaizola/musikai/pkg/suno"
	"github.com/igolaizola/musikai/pkg/udio"
	"github.com/oklog/ulid/v2"
	"github.com/smarty/cproxy/v2"
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
	Provider     string
	Input        string
	Type         string
	Prompt       string
	Manual       bool
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

	CaptchaProvider string
	CaptchaKey      string
	CaptchaProxy    string
	UdioKey         string
}

type input struct {
	Weight       int    `json:"weight" csv:"weight"`
	Type         string `json:"type" csv:"type"`
	Prompt       string `json:"prompt" csv:"prompt"`
	Manual       bool   `json:"manual" csv:"manual"`
	Instrumental bool   `json:"instrumental" csv:"instrumental"`
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
	var generator music.Generator
	switch cfg.Provider {
	case "suno":
		generator = suno.New(&suno.Config{
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
	case "udio":
		proxy := cfg.Proxy
		if proxy == "" {
			// Start a connect proxy server on a random port
			handler := cproxy.New(
				cproxy.Options.Logger(logger{}),
				cproxy.Options.LogConnections(true),
			)
			listener, err := net.Listen("tcp", ":0")
			if err != nil {
				return fmt.Errorf("generate: couldn't create listener: %w", err)
			}
			defer func() {
				_ = listener.Close()
			}()
			port := listener.Addr().(*net.TCPAddr).Port
			proxy = fmt.Sprintf("http://localhost:%d", port)
			go func() {
				_ = http.Serve(listener, handler)
			}()
			log.Println("generate: running udio proxy on", proxy)
		}
		capthaProxy := cfg.CaptchaProxy
		if capthaProxy == "" {
			// Start a ngrok tunnel to the proxy
			u, err := url.Parse(proxy)
			if err != nil {
				return fmt.Errorf("invalid proxy URL: %w", err)
			}
			candidate, cancel, err := ngrok.Run(ctx, "tcp", u.Port())
			if err != nil {
				return fmt.Errorf("generate: couldn't start ngrok: %w", err)
			}
			capthaProxy = candidate
			log.Printf("generate: ngrok started %s => %s\n", capthaProxy, u.Port())
			defer cancel()
		}
		generator, err = udio.New(&udio.Config{
			Wait:            4 * time.Second,
			Debug:           cfg.Debug,
			Client:          httpClient,
			CookieStore:     store.NewCookieStore("udio", cfg.Account),
			Parallel:        cfg.Limit == 1,
			MinDuration:     cfg.MinDuration,
			MaxDuration:     cfg.MaxDuration,
			MaxExtensions:   cfg.MaxExtensions,
			CaptchaKey:      cfg.CaptchaKey,
			CaptchaProvider: cfg.CaptchaProvider,
			CaptchaProxy:    capthaProxy,
			Key:             cfg.UdioKey,
		})
		if err != nil {
			return fmt.Errorf("generate: couldn't create udio generator: %w", err)
		}
	default:
		return fmt.Errorf("generate: unknown provider: %s", cfg.Provider)
	}
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
			var tmpl template
			if fn != nil {
				tmpl = fn()
			} else {
				tmpl = template{
					Type:         cfg.Type,
					Prompt:       cfg.Prompt,
					Manual:       cfg.Manual,
					Instrumental: cfg.Instrumental,
				}
				if tmpl.Prompt == "" {
					tmpl = nextTemplate()
				}
			}

			// Launch generate in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("generate: start %s", tmpl)
				err := generate(ctx, cfg.Account, cfg.Provider, generator, store, tmpl, cfg.Notes)
				if err != nil {
					log.Println(err)
				}
				debug("generate: end %s", tmpl)
				errC <- err
			}()
		}
	}
}

func generate(ctx context.Context, account, provider string, generator music.Generator, store *storage.Store, t template, notes string) error {
	// Generate the songs.
	songs, err := generator.Generate(ctx, t.Prompt, t.Manual, t.Instrumental)
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
			Manual:       t.Manual,
			Style:        gens[0].Style,
			Instrumental: t.Instrumental,
			Provider:     provider,
			Account:      account,
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
			if err := store.SetGeneration(ctx, &storage.Generation{
				ID:         genID,
				SongID:     &song.ID,
				ExternalID: g.ID,
				Audio:      g.Audio,
				Image:      g.Image,
				Title:      g.Title,
				History:    g.History,
				Duration:   g.Duration,
				Lyrics:     g.Lyrics,
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

func toTemplateFunc(file string) (func() template, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("generate: couldn't read input file: %w", err)
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
		return nil, fmt.Errorf("generate: unsupported output format: %s", ext)
	}
	inputs, err := unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("generate: couldn't unmarshal input: %w", err)
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("generate: no inputs found in file")
	}
	var opts []template
	for _, i := range inputs {
		w := i.Weight
		if w <= 0 {
			w = 1
		}
		if i.Prompt == "" {
			log.Println("generate: skipping empty input")
			continue
		}
		opts = append(opts, options(w, template{
			Type:         i.Type,
			Prompt:       i.Prompt,
			Manual:       i.Manual,
			Instrumental: i.Instrumental,
		})...)
	}
	return func() template {
		return opts[rand.Intn(len(opts))]
	}, nil
}

type logger struct{}

func (logger) Printf(format string, args ...interface{}) {
	log.Printf(format, args...)
}
