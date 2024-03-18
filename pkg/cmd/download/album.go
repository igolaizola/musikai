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

	"github.com/igolaizola/musikai/pkg/filestore"
	"github.com/igolaizola/musikai/pkg/storage"
)

func RunAlbum(ctx context.Context, cfg *Config) error {
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

	albumLookup := map[string]string{}
	var lck sync.Mutex

	var currID string
	var songs []*storage.Song
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

			// Get next songs
			filters := []storage.Filter{
				storage.Where("state = ?", storage.Used),
				storage.Where("songs.id > ?", currID),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
			}

			// Get next song
			if len(songs) == 0 {
				// Get a songs from the database.
				var err error
				songs, err = store.ListAllSongs(ctx, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("download: couldn't get song from database: %w", err)
				}
				if len(songs) == 0 {
					return errors.New("download: no songs to process")
				}
				currID = songs[len(songs)-1].ID
			}
			song := songs[0]
			songs = songs[1:]

			if song.AlbumID == "" {
				continue
			}

			// Launch process in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("download: start %s", song.ID)

				lck.Lock()
				albumDir, ok := albumLookup[song.AlbumID]
				if !ok {
					albumDir, err = downloadCover(ctx, song.AlbumID, debug, store, fs, cfg.Output)
					if err != nil {
						log.Println(err)
						errC <- err
						return
					}
					albumLookup[song.AlbumID] = albumDir
				}
				lck.Unlock()

				if err := downloadSong(ctx, song, debug, fs, albumDir); err != nil {
					log.Println(err)
				}
				debug("download: end %s", song.ID)
				errC <- err
			}()
		}
	}
}

func downloadCover(ctx context.Context, albumID string, debug func(string, ...any), store *storage.Store, fs *filestore.Store, output string) (string, error) {
	album, err := store.GetAlbum(ctx, albumID)
	if err != nil {
		return "", err
	}

	name := album.Title
	if album.Subtitle != "" {
		name += " - " + album.Subtitle
	}
	if album.Volume > 0 {
		name += fmt.Sprintf(" (Vol. %d)", album.Volume)
	}
	name += " - " + album.Artist

	albumDir := filepath.Join(output, name)

	// Download the cover
	if err := os.MkdirAll(albumDir, 0755); err != nil {
		return "", fmt.Errorf("download: couldn't create output directory: %w", err)
	}
	file := filestore.JPG(album.ID)
	cover := filepath.Join(albumDir, file)
	if _, err := os.Stat(cover); err != nil {
		debug("download: start download cover %s", album.ID)
		if err := fs.GetJPG(ctx, cover, album.ID); err != nil {
			return "", fmt.Errorf("download: couldn't download master audio: %w", err)
		}
		debug("download: end download master %s", album.ID)
	}
	return albumDir, nil
}

func downloadSong(ctx context.Context, song *storage.Song, debug func(string, ...any), fs *filestore.Store, output string) error {
	name := fmt.Sprintf("%02d - %s", song.Order, song.Title)

	// Download the mastered audio
	mastered := filepath.Join(output, fmt.Sprintf("%s.mp3", name))
	if _, err := os.Stat(mastered); err != nil {
		debug("download: start download master %s", song.ID)
		if err := fs.GetMP3(ctx, mastered, song.ID); err != nil {
			return fmt.Errorf("download: couldn't download master audio: %w", err)
		}
		debug("download: end download master %s", song.ID)
	}

	return nil
}
