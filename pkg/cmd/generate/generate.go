package generate

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/aubio"
	"github.com/igolaizola/musikai/pkg/s3"
	"github.com/igolaizola/musikai/pkg/sound"
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
	Output      string

	S3Bucket string
	S3Region string
	S3Key    string
	S3Secret string

	Account        string
	Type           string
	Prompt         string
	Style          string
	Instrumental   bool
	EndPrompt      string
	EndStyle       string
	EndStyleAppend bool
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

	aub := aubio.New("aubio")
	if _, err := aub.Version(ctx); err != nil {
		return fmt.Errorf("generate: couldn't get aubio version: %w", err)
	}

	if cfg.Output != "" {
		// Create output folder if it doesn't exist.
		if err := os.MkdirAll(cfg.Output, 0755); err != nil {
			return fmt.Errorf("generate: couldn't create output folder: %w", err)
		}
	}

	fstore := s3.New(cfg.S3Key, cfg.S3Secret, cfg.S3Region, cfg.S3Bucket, cfg.Debug)
	if err := fstore.Start(ctx); err != nil {
		return fmt.Errorf("generate: couldn't start s3 store: %w", err)
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
		Client:         http.DefaultClient,
		CookieStore:    store.NewCookieStore("suno", cfg.Account),
		Parallel:       cfg.Limit == 1,
		EndPrompt:      cfg.EndPrompt,
		EndStyle:       cfg.EndStyle,
		EndStyleAppend: cfg.EndStyleAppend,
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
				err := generate(ctx, generator, store, fstore, aub, httpClient, cfg.Output, tmpl)
				if err != nil {
					log.Println(err)
				}
				debug("generate: end %s", tmpl)
				errC <- err
			}()
		}
	}
}

func generate(ctx context.Context, generator *suno.Client, store *storage.Store, fstore *s3.Store, aub *aubio.App, client *http.Client, output string, t template) error {
	// Generate the songs.
	songs, err := generator.Generate(ctx, t.Prompt, t.Style, t.Title, t.Instrumental)
	if err != nil {
		return fmt.Errorf("generate: couldn't generate song %s: %w", t, err)
	}

	// Save the generated songs to the database.
	for _, s := range songs {
		// Download the audio file
		b, err := download(ctx, client, s.Audio)
		if err != nil {
			return fmt.Errorf("generate: couldn't download song audio: %w", err)
		}
		if output != "" {
			path := filepath.Join(output, fmt.Sprintf("%s.mp3", s.ID))
			if err := os.WriteFile(path, b, 0644); err != nil {
				return fmt.Errorf("generate: couldn't save song audio: %w", err)
			}
		}

		// Generate the wave image
		a, err := sound.NewAnalyzerBytes(b)
		if err != nil {
			return fmt.Errorf("generate: couldn't create analyzer: %w", err)
		}
		waveBytes, err := a.PlotWave()
		if err != nil {
			return fmt.Errorf("generate: couldn't plot wave: %w", err)
		}
		waveName := fmt.Sprintf("%s-wave.jpg", s.ID)
		if err := fstore.SetImage(ctx, waveName, waveBytes); err != nil {
			return fmt.Errorf("generate: couldn't save wave image to s3: %w", err)
		}
		waveURL := fstore.URL(waveName)

		// Get the tempo
		tempo, err := aub.Tempo(ctx, s.Audio)
		if err != nil {
			return fmt.Errorf("generate: couldn't get tempo: %w", err)
		}

		if err := store.SetSong(ctx, &storage.Song{
			ID:        ulid.Make().String(),
			Type:      t.Type,
			Prompt:    t.Prompt,
			Style:     s.Style,
			Duration:  s.Duration,
			SunoID:    s.ID,
			SunoAudio: s.Audio,
			SunoImage: s.Image,
			SunoTitle: s.Title,
			Wave:      waveURL,
			Tempo:     float32(tempo),
		}); err != nil {
			return fmt.Errorf("generate: couldn't save song to database: %w", err)
		}
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("couldn't create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't download video: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("couldn't read response body: %w", err)
	}
	return b, nil
}
