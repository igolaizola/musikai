package musikai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/igolaizola/musikai/pkg/suno"
)

type Config struct {
	Proxy  string
	Wait   time.Duration
	Debug  bool
	Cookie string
}

// GenerateSong generates a song given a prompt.
func GenerateSong(ctx context.Context, cfg *Config, prompt, style, title string, instrumental bool, output string) error {
	if prompt != "" && style != "" {
		return fmt.Errorf("prompt and style are mutually exclusive")
	}
	log.Println("generating songs...")
	start := time.Now()
	defer func() {
		log.Printf("elapsed time: %v\n", time.Since(start))
	}()
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
	client := suno.New(&suno.Config{
		Wait:        cfg.Wait,
		Debug:       cfg.Debug,
		Client:      httpClient,
		CookieStore: suno.NewCookieStore(cfg.Cookie),
	})
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("couldn't start suno client: %w", err)
	}
	defer func() {
		if err := client.Stop(ctx); err != nil {
			log.Printf("couldn't stop suno client: %v\n", err)
		}
	}()
	songs, err := client.Generate(ctx, prompt, style, title, instrumental)
	if err != nil {
		return fmt.Errorf("couldn't generate song: %w", err)
	}

	// Print song info
	js, err := json.MarshalIndent(songs, "", "  ")
	if err != nil {
		return fmt.Errorf("couldn't marshal song: %w", err)
	}
	log.Printf("song: %s\n", js)

	// Download songs
	for _, song := range songs {
		path := filepath.Join(output, fmt.Sprintf("%s.mp3", song.ID))
		if err := download(ctx, httpClient, song.Audio, path); err != nil {
			return fmt.Errorf("couldn't download song: %w", err)
		}
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url, output string) error {
	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("couldn't create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't download video: %w", err)
	}
	defer resp.Body.Close()

	// Write video to output
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("couldn't create temp file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("couldn't write to temp file: %w", err)
	}
	return nil
}
