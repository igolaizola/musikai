package musikai

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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
func GenerateSong(ctx context.Context, cfg *Config, prompt string, output string) error {
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
	songs, err := client.GenerateV2(ctx, prompt)
	if err != nil {
		return fmt.Errorf("couldn't generate song: %w", err)
	}
	for _, song := range songs {
		log.Println("id:", song.ID)
		log.Println("title:", song.Title)
		log.Println("url:", song.AudioURL)
		log.Println("image:", song.ImageURL)
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
