package upscale

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/igolaizola/musikai/pkg/filestore"
	"github.com/igolaizola/musikai/pkg/ratelimit"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/igolaizola/musikai/pkg/upscale"
)

type Config struct {
	Debug       bool
	Limit       int
	Timeout     time.Duration
	Concurrency int
	Type        string

	// Database parameters
	DBType string
	DBConn string
	FSType string
	FSConn string
	Proxy  string

	// Upscale parameters
	UpscaleType       string
	UpscaleBin        string
	UploadConcurrency int
}

// Run runs the upscale process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("upscale: process started")
	defer func() {
		log.Printf("upscale: process ended (%d)\n", iteration)
	}()

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("upscale: couldn't create storage store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("upscale: couldn't start storage store: %w", err)
	}

	upscaler, err := upscale.New(cfg.UpscaleType, cfg.UpscaleBin)
	if err != nil {
		return fmt.Errorf("upscale: couldn't create upscale client: %w", err)
	}

	fs, err := filestore.New(cfg.FSType, cfg.FSConn, cfg.Proxy, cfg.Debug, store)
	if err != nil {
		return fmt.Errorf("download: couldn't create file storage: %w", err)
	}

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("upscale: total time %s, average time %s\n", total, total/time.Duration(iteration))
	}()
	var totalTime time.Duration
	var upscaleTime time.Duration
	addTime := func(t, u time.Duration) {
		totalTime += t
		upscaleTime += u
	}
	defer func() {
		log.Printf("upscale: sum time %s, upscale %s (%.2f%%)\n", totalTime, upscaleTime, float64(upscaleTime)/float64(totalTime)*100)
	}()

	var nErr int
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 24 * time.Hour
	}
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()
	last := time.Now()

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

	var uploads int32
	var uploadErr int32
	var rlimits []ratelimit.Lock
	upConcurrency := cfg.UploadConcurrency
	if upConcurrency == 0 {
		upConcurrency = concurrency
	}
	for i := 0; i < upConcurrency; i++ {
		rlimits = append(rlimits, ratelimit.New(50*time.Millisecond))
	}

	var covers []*storage.Cover
	var currID string
	for {
		var err error
		select {
		case <-ctx.Done():
			return fmt.Errorf("upscale: %w", ctx.Err())
		case <-ticker.C:
			return nil
		case err = <-errC:
		}
		if err != nil {
			nErr++
		} else {
			nErr = 0
		}

		// Check exit conditions
		if nErr > 5 || uploadErr > 5 {
			return fmt.Errorf("upscale: too many consecutive errors: %w", err)
		}
		if cfg.Limit > 0 && iteration >= cfg.Limit {
			return nil
		}
		iteration++

		if time.Since(last) > 30*time.Minute {
			elapsed := time.Since(start)
			log.Printf("upscale: iteration %d uploads %d elapsed %s average %s\n", iteration, uploads, elapsed, elapsed/time.Duration(iteration))
			last = time.Now()
		}

		// Get next cover
		filters := []storage.Filter{
			storage.Where("upscaled = ?", false),
			storage.Where("state = ?", storage.Approved),
			storage.Where("id > ?", currID),
		}
		if cfg.Type != "" {
			filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
		}

		// Get next cover
		if len(covers) == 0 {
			// Get a covers from the database.
			var err error
			covers, err = store.ListAllCovers(ctx, 1, 100, "", filters...)
			if err != nil {
				return fmt.Errorf("upscale: couldn't get cover from database: %w", err)
			}
			if len(covers) == 0 {
				return fmt.Errorf("upscale: no covers to upscale")
			}
			currID = covers[len(covers)-1].ID
		}
		cover := covers[0]
		covers = covers[1:]

		// Launch upscale in a goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			rlimit := rlimits[iteration%len(rlimits)]
			err := upscaleCover(ctx, cfg.Debug, &wg, store, fs, rlimit, upscaler, &uploads, &uploadErr, addTime, cover)
			if err != nil {
				log.Println(err)
			}
			errC <- err
		}()
	}
}

func upscaleCover(ctx context.Context, isDebug bool, wg *sync.WaitGroup, store *storage.Store, fs *filestore.Store, rlimit ratelimit.Lock, upscaler *upscale.Upscaler, uploads *int32, nErr *int32, addTime func(t, u time.Duration), cover *storage.Cover) error {
	start := time.Now()
	var upscaleTime time.Duration
	defer func() {
		addTime(time.Since(start), upscaleTime)
	}()

	debug := func(msg string, args ...interface{}) {
		if !isDebug {
			return
		}
		msg += "\n"
		log.Printf(msg, args...)
	}

	// Obtain extension from cover URL
	u := cover.URL()
	ext := filepath.Ext(strings.Split(u, "?")[0])

	// Generate a temporary file name path using the cover id it must work on any OS
	name := fmt.Sprintf("%s%s", cover.ID, ext)
	original := filepath.Join(os.TempDir(), name)

	// Download cover
	debug("upscale: download start %s", name)
	if err := download(ctx, isDebug, u, original); err != nil {
		return fmt.Errorf("upscale: couldn't download cover: %w", err)
	}
	debug("upscale: download end %s", name)

	// Create a upscale directory on a temporary directory
	upscaleDir := filepath.Join(os.TempDir(), "upscaled")
	if err := os.MkdirAll(upscaleDir, 0755); err != nil {
		return fmt.Errorf("upscale: couldn't create upscale directory: %w", err)
	}

	// Upscale cover
	debug("upscale: upscale start %s", name)
	upscaleStart := time.Now()
	upscaled, err := upscaler.Upscale(ctx, original, upscaleDir)
	if err != nil {
		return fmt.Errorf("upscale: couldn't upscale cover: %w", err)
	}
	upscaleTime += time.Since(upscaleStart)
	debug("upscale: upscale end %s", name)

	// Remove original cover
	if err := os.Remove(original); err != nil {
		log.Printf("upscale: couldn't remove original cover: %v\n", err)
	}

	// Check if the upscaled cover is too small
	info, err := os.Stat(upscaled)
	if err != nil {
		return fmt.Errorf("upscale: couldn't get upscaled cover info: %w", err)
	}
	if info.Size() < 1024*1024 {
		return fmt.Errorf("upscale: upscaled cover %s is too small (%d KB)", upscaled, info.Size()/1024)
	}

	// Wait for uploads to be less than 100
	for *uploads > 100 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	// Launch a goroutine to upload the upscaled cover and update the cover
	wg.Add(1)
	go func() {
		defer wg.Done()

		atomic.AddInt32(uploads, 1)
		defer atomic.AddInt32(uploads, -1)

		unlock := rlimit.Lock(ctx)
		defer unlock()

		// Upload upscaled cover
		debug("upscale: upload start %s", name)
		if err := fs.SetJPG(ctx, upscaled, cover.ID); err != nil {
			log.Println(fmt.Errorf("upscale: couldn't upload cover: %w", err))
			atomic.AddInt32(nErr, 1)
			return
		}
		debug("upscale: upload end %s", name)

		// Remove upscaled cover
		if err := os.Remove(upscaled); err != nil {
			log.Printf("upscale: couldn't remove upscaled cover: %v\n", err)
		}

		// Update cover
		cover.Upscaled = true
		cover.UpscaleAt = time.Now().UTC()
		if err := store.SetCover(ctx, cover); err != nil {
			log.Println(fmt.Errorf("upscale: couldn't update cover: %w", err))
			atomic.AddInt32(nErr, 1)
			return
		}
		atomic.StoreInt32(nErr, 0)
	}()
	return nil
}

var backoff = []time.Duration{
	15 * time.Second,
	30 * time.Second,
	1 * time.Minute,
}

func download(ctx context.Context, debug bool, u, output string) error {
	// Download file
	maxAttempts := 3
	attempts := 0
	for {
		err := downloadAttempt(ctx, u, output)
		if err == nil {
			break
		}

		// Increase attempts and check if we should stop
		attempts++
		if attempts >= maxAttempts {
			return err
		}
		idx := attempts - 1
		if idx >= len(backoff) {
			idx = len(backoff) - 1
		}
		wait := backoff[idx]
		t := time.NewTimer(wait)
		if debug {
			log.Printf("%v (retrying in %s)\n", err, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

func downloadAttempt(ctx context.Context, u, output string) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("upscale: couldn't create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/112.0.0.0 Safari/537.36 Edg/112.0.1722.58")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upscale: couldn't download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upscale: bad status: %s", resp.Status)
	}

	// Write the response body to the output file.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("upscale: couldn't read body: %w", err)
	}
	if err := os.WriteFile(output, body, 0644); err != nil {
		return fmt.Errorf("upscale: couldn't write file: %w", err)
	}
	return nil
}
