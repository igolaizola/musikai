package publish

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/distrokid"
	"github.com/igolaizola/musikai/pkg/storage"
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
}

// Run launches the song generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("publish: process started")
	defer func() {
		log.Printf("publish: process ended (%d)\n", iteration)
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
		return fmt.Errorf("publish: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("publish: couldn't start orm store: %w", err)
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
	/*
		client := distrokid.New(&distrokid.Config{
			Wait:        4 * time.Second,
			Debug:       cfg.Debug,
			Client:      httpClient,
			CookieStore: store.NewCookieStore("distrokid", cfg.Account),
		})
		if err := client.Start(ctx); err != nil {
			return fmt.Errorf("publish: couldn't start distrokid: %w", err)
		}
		defer func() {
			if err := client.Stop(ctx); err != nil {
				log.Printf("publish: couldn't stop distrokid: %v\n", err)
			}
		}()
		if err := client.New(ctx); err != nil {
			return fmt.Errorf("publish: couldn't distrokid new: %w", err)
		}
	*/
	browser := distrokid.NewBrowser(&distrokid.BrowserConfig{
		Wait:        4 * time.Second,
		Proxy:       cfg.Proxy,
		CookieStore: store.NewCookieStore("distrokid", cfg.Account),
	})
	if err := browser.Start(ctx); err != nil {
		return fmt.Errorf("publish: couldn't start distrokid browser: %w", err)
	}
	defer func() {
		if err := browser.Stop(); err != nil {
			log.Printf("publish: couldn't stop distrokid browser: %v\n", err)
		}
	}()
	album := &distrokid.Album{
		Artist:    "My Music",
		Title:     "New Album",
		FirstName: "John",
		LastName:  "Doe",
		Songs: []distrokid.Song{
			{
				Title: "Song 1",
				Path:  "",
			},
			{
				Title: "Song 2",
				Path:  "",
			},
		},
	}
	if err := browser.Publish(ctx, album); err != nil {
		return fmt.Errorf("publish: couldn't distrokid publish: %w", err)
	}
	return nil

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("publish: total time %s, average time %s\n", total, total/time.Duration(iteration))
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

	var albums []*storage.Album
	var currID string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("publish: %w", ctx.Err())
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
				return fmt.Errorf("publish: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("publish: iteration %d\n", iteration)
			}

			// Get next albums
			filters := []storage.Filter{
				storage.Where("state = ?", storage.Approved),
				storage.Where("id > ?", currID),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
			}

			// Get next image
			if len(albums) == 0 {
				// Get a albums from the database.
				var err error
				albums, err = store.ListAlbums(ctx, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("process: couldn't get album from database: %w", err)
				}
				if len(albums) == 0 {
					return errors.New("process: no albums to process")
				}
				currID = albums[len(albums)-1].ID
			}
			album := albums[0]
			albums = albums[1:]

			// Launch publish in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("publish: start %s %s", album.ID, album.Title)
				err := publish(ctx, album, browser, store)
				if err != nil {
					log.Println(err)
				}
				debug("publish: end %s%s", album.ID, album.Title)
				errC <- err
			}()
		}
	}
}

func publish(ctx context.Context, album *storage.Album, b *distrokid.Browser, store *storage.Store) error {
	// TODO: Implement the publish function
	return nil
}
