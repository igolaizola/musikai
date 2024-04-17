package jamendo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/filestore"
	"github.com/igolaizola/musikai/pkg/jamendo"
	"github.com/igolaizola/musikai/pkg/sonoteller"
	"github.com/igolaizola/musikai/pkg/sound/ffmpeg"
	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug  bool
	DBType string
	DBConn string
	FSType string
	FSConn string
	Proxy  string

	Timeout     time.Duration
	Concurrency int
	Limit       int

	Auto       bool
	Account    string
	ArtistName string
	ArtistID   int
	Type       string
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

	if cfg.ArtistID == 0 {
		return errors.New("publish: artist ID is required")
	}
	if cfg.ArtistName == "" {
		return errors.New("publish: artist name is required")
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("publish: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("publish: couldn't start orm store: %w", err)
	}

	fs, err := filestore.New(cfg.FSType, cfg.FSConn, cfg.Proxy, cfg.Debug, store)
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
	browser := jamendo.NewBrowser(&jamendo.BrowserConfig{
		Wait:        4 * time.Second,
		Proxy:       cfg.Proxy,
		CookieStore: store.NewCookieStore("jamendo", cfg.Account),
	})
	if err := browser.Start(ctx); err != nil {
		return fmt.Errorf("publish: couldn't start jamendo browser: %w", err)
	}
	defer func() {
		if err := browser.Stop(); err != nil {
			log.Printf("publish: couldn't stop jamendo browser: %v\n", err)
		}
	}()

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
				// TODO: exit on first error for now
				//nErr += 1
				return fmt.Errorf("publish: %w", err)
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
				storage.Where("state = ?", storage.Used),
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
				err := publish(ctx, cfg, browser, store, fs, album)
				if err != nil {
					log.Println(err)
				}
				debug("publish: end %s%s", album.ID, album.Title)
				errC <- err
			}()
		}
	}
}

func publish(ctx context.Context, cfg *Config, b *jamendo.Browser, store *storage.Store, fs *filestore.Store, album *storage.Album) error {
	// Get songs for album
	songs, err := store.ListSongs(ctx, 1, 100, "", storage.Where("album_id = ?", album.ID))
	if err != nil {
		return fmt.Errorf("publish: couldn't get songs: %w", err)
	}

	// Download cover
	name := filestore.JPG(album.ID)
	cover := filepath.Join(os.TempDir(), name)
	if err := fs.GetJPG(ctx, cover, album.ID); err != nil {
		return fmt.Errorf("publish: couldn't download cover: %w", err)
	}

	// Create jamendo album data
	jmAlbum := &jamendo.Album{
		Artist:         album.Artist,
		Title:          album.Title,
		Cover:          cover,
		PrimaryGenre:   album.PrimaryGenre,
		SecondaryGenre: album.SecondaryGenre,
		ReleaseDate:    album.PublishedAt,
		UPC:            album.UPC,
	}

	// Order songs by track number
	sort.Slice(songs, func(i, j int) bool {
		return songs[i].Order < songs[j].Order
	})

	// Create jamendo song data
	// TODO: limit to 1 song for now
	songs = songs[:1]
	for _, s := range songs {
		// Download song
		name := filestore.MP3(s.ID)
		mp3 := filepath.Join(os.TempDir(), name)
		if err := fs.GetMP3(ctx, mp3, s.ID); err != nil {
			return fmt.Errorf("publish: couldn't download song: %w", err)
		}
		// Convert mp3 to wav
		wav := filepath.Join(os.TempDir(), fmt.Sprintf("%s.wav", s.ID))
		if err := ffmpeg.Convert(ctx, mp3, wav); err != nil {
			return fmt.Errorf("publish: couldn't convert mp3 to wav: %w", err)
		}

		// TODO: initialize with album genres
		var genres []string
		var tags []string
		tempo := s.Generation.Tempo
		description := s.Style

		var analysis sonoteller.Analysis
		if s.Classification != "" {
			if err := json.Unmarshal([]byte(s.Classification), &analysis); err != nil {
				return fmt.Errorf("publish: couldn't unmarshal classification: %w", err)
			}
			m := analysis.Music
			tempo = float32(m.BPM)

			instr := map[string]int{}
			for _, i := range m.Instruments {
				instr[i] = 100
			}

			var values []string
			for _, src := range sortTags(m.Genres, instr, m.Styles, m.Moods) {
				values = append(values, src)
				v, t, ok := jamendo.GetField(src)
				if !ok {
					continue
				}
				switch t {
				case jamendo.Genre:
					genres = append(genres, v)
				case jamendo.Tag:
					tags = append(tags, v)
				}
			}
			description = strings.Join(values, ", ")
		}

		if len(genres) > 2 {
			genres = genres[:2]
		}
		if len(tags) > 2 {
			tags = tags[:2]
		}

		dkSong := &jamendo.Song{
			Instrumental: s.Instrumental,
			Title:        s.Title,
			ISRC:         s.ISRC,
			File:         wav,
			Genres:       genres,
			Tags:         tags,
			BPM:          tempo,
			Description:  description,
		}
		jmAlbum.Songs = append(jmAlbum.Songs, dkSong)
	}

	// Publish album
	jmID, err := b.Publish(ctx, jmAlbum, cfg.Auto)
	if err != nil {
		return fmt.Errorf("publish: couldn't jamendo publish %s: %w", album.ID, err)
	}
	_ = jmID
	// Update album
	/*
		album.JamendoID = jmID
		album.PublishedAt = time.Now().UTC()
		album.State = storage.Used
		if err := store.SetAlbum(ctx, album); err != nil {
			return fmt.Errorf("publish: couldn't set album %s %s: %w", album.ID, dkID, err)
		}
	*/
	return nil
}

func sortTags(ms ...map[string]int) []string {
	m := make(map[string]int)
	for _, mm := range ms {
		for k, v := range mm {
			m[k] = v
		}
	}
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	// Sort from biggest to smallest
	sort.Slice(keys, func(i, j int) bool {
		return m[keys[i]] > m[keys[j]]
	})
	return keys
}
