package download

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/filestorage/tgstore"
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

	Output string

	TGChat  int64
	TGToken string

	Type string
}

// Run launches the gen generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Printf("download: started\n")
	defer func() {
		log.Printf("download: ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...any) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	if err := os.MkdirAll(cfg.Output, 0755); err != nil {
		return fmt.Errorf("download: couldn't create output directory: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("download: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("download: couldn't start orm store: %w", err)
	}

	tgStore, err := tgstore.New(cfg.TGToken, cfg.TGChat, cfg.Proxy, cfg.Debug)
	if err != nil {
		return fmt.Errorf("download: couldn't create file storage: %w", err)
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

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("download: total time %s, average time %s\n", total, total/time.Duration(iteration))
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

	// Search last id in the output directory to avoid downloading the same files
	files, err := os.ReadDir(cfg.Output)
	if err != nil {
		return fmt.Errorf("download: couldn't read output directory: %w", err)
	}
	var currID string
	for _, file := range files {
		if filepath.Ext(file.Name()) == ".mp3" {
			currID = file.Name()[:len(file.Name())-4]
		}
	}

	var gens []*storage.Generation
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("download: %w", ctx.Err())
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
				return fmt.Errorf("download: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("download: iteration %d\n", iteration)
			}

			// Get next gens
			filters := []storage.Filter{
				//storage.Where("processed = ?", cfg.Reprocess),
				storage.Where("generations.id > ?", currID),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
			}

			// Get next gen
			if len(gens) == 0 {
				// Get a gens from the database.
				var err error
				gens, err = store.ListGenerations(ctx, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("download: couldn't get gen from database: %w", err)
				}
				if len(gens) == 0 {
					return errors.New("download: no gens to download")
				}
				currID = gens[len(gens)-1].ID
			}
			gen := gens[0]
			gens = gens[1:]

			// Launch process in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("download: start %s", gen.ID)

				if err := download(ctx, gen, debug, tgStore, cfg.Output); err != nil {
					log.Println(err)
				}
				debug("download: end %s", gen.ID)
				errC <- err
			}()
		}
	}
}

func download(ctx context.Context, gen *storage.Generation, debug func(string, ...any), tgStore *tgstore.Store, output string) error {
	// Download the mastered audio
	mastered := filepath.Join(output, fmt.Sprintf("%s.mp3", gen.ID))
	if _, err := os.Stat(mastered); err != nil {
		debug("download: start download master %s", gen.ID)
		if err := tgStore.Download(ctx, gen.Master, mastered); err != nil {
			return fmt.Errorf("download: couldn't download master audio: %w", err)
		}
		debug("download: end download master %s", gen.ID)
	}
	wave := filepath.Join(output, fmt.Sprintf("%s.jpg", gen.ID))
	if _, err := os.Stat(wave); err != nil {
		debug("download: start download wave %s", gen.ID)
		if err := tgStore.Download(ctx, gen.Wave, wave); err != nil {
			return fmt.Errorf("download: couldn't download wave: %w", err)
		}
		debug("download: end download wave %s", gen.ID)
	}
	return nil
}
