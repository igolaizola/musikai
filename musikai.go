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
func GenerateSong(ctx context.Context, cfg *Config, prompt, title string, instrumental bool, output string) error {
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
	song, err := client.Generate(ctx, prompt, title, instrumental)
	if err != nil {
		return fmt.Errorf("couldn't generate song: %w", err)
	}

	// Print song info
	js, err := json.MarshalIndent(song, "", "  ")
	if err != nil {
		return fmt.Errorf("couldn't marshal song: %w", err)
	}
	log.Printf("song: %s\n", js)

	// Download song
	if output == "" {
		output = fmt.Sprintf("%s.mp3", song.ID)
	}
	// Check if output is a folder
	if fi, err := os.Stat(output); err == nil && fi.IsDir() {
		output = filepath.Join(output, fmt.Sprintf("%s.mp3", song.ID))
	}
	if err := download(ctx, httpClient, song.Audio, output); err != nil {
		return fmt.Errorf("couldn't download song: %w", err)
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
