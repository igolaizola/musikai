package single

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/filestore"
	"github.com/igolaizola/musikai/pkg/image"
	"github.com/igolaizola/musikai/pkg/inkpic"
	"github.com/igolaizola/musikai/pkg/sound/ffmpeg"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/igolaizola/musikai/pkg/youtube"
)

type Config struct {
	Debug  bool
	DBType string
	DBConn string
	FSType string
	FSConn string
	Proxy  string
	Chrome string

	Timeout     time.Duration
	Concurrency int
	Limit       int

	Auto        bool
	Account     string
	ChannelName string
	ChannelID   string
	Type        string

	Overlay  string
	Font     string
	FontSize string
}

// Run launches the song generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("single: process started")
	defer func() {
		log.Printf("single: process ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...interface{}) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	if cfg.ChannelID == "" {
		return errors.New("single: channel ID is required")
	}
	if cfg.ChannelName == "" {
		return errors.New("single: channel name is required")
	}

	// Check if overlay file exists
	if _, err := os.Stat(cfg.Overlay); err != nil {
		return fmt.Errorf("album: couldn't find overlay file: %w", err)
	}
	// Check if font file exists
	if _, err := os.Stat(cfg.Font); err != nil {
		return fmt.Errorf("album: couldn't find font file: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("single: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("single: couldn't start orm store: %w", err)
	}

	fs, err := filestore.New(cfg.FSType, cfg.FSConn, cfg.Proxy, cfg.Debug, store)
	if err != nil {
		return fmt.Errorf("download: couldn't create file storage: %w", err)
	}

	ink, err := inkpic.New(ctx)
	if err != nil {
		return fmt.Errorf("single: couldn't create inkpic client: %w", err)
	}

	cookieStore := store.NewCookieStore("youtube", cfg.Account)

	browser := youtube.NewBrowser(&youtube.BrowserConfig{
		Wait:        1 * time.Second,
		Proxy:       cfg.Proxy,
		CookieStore: cookieStore,
		BinPath:     cfg.Chrome,
		ChannelID:   cfg.ChannelID,
		ChannelName: cfg.ChannelName,
	})
	if err := browser.Start(ctx); err != nil {
		return fmt.Errorf("single: couldn't start youtube browser: %w", err)
	}
	defer func() {
		if err := browser.Stop(); err != nil {
			log.Printf("single: couldn't stop youtube browser: %v\n", err)
		}
	}()

	// Print time stats
	start := time.Now()
	defer func() {
		total := time.Since(start)
		log.Printf("single: total time %s, average time %s\n", total, total/time.Duration(iteration))
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

	var songs []*storage.Song
	var currID string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("single: %w", ctx.Err())
		case <-ticker.C:
			return nil
		case err := <-errC:
			if err != nil {
				// TODO: exit on first error for now
				//nErr += 1
				return fmt.Errorf("single: %w", err)
			} else {
				nErr = 0
			}

			// Check exit conditions
			if nErr > 10 {
				return fmt.Errorf("single: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("single: iteration %d\n", iteration)
			}

			// Get next song
			filters := []storage.Filter{
				storage.Where("state = ?", storage.Approved),
				storage.Where("songs.id > ?", currID),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
			}

			// Get next song
			if len(songs) == 0 {
				// Get songs from the database.
				var err error
				songs, err = store.ListSongs(ctx, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("process: couldn't get album from database: %w", err)
				}
				if len(songs) == 0 {
					return errors.New("process: no songs to process")
				}
				currID = songs[len(songs)-1].ID
			}
			song := songs[0]
			songs = songs[1:]

			// Launch process in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("single: start %s %s", song.ID, song.Title)
				err := process(ctx, browser, store, fs, ink, cfg.Font, cfg.FontSize, cfg.Overlay, song)
				if err != nil {
					log.Println(err)
				}
				debug("single: end %s%s", song.ID, song.Title)
				errC <- err
			}()
		}
	}
}

func process(ctx context.Context, b *youtube.Browser, store *storage.Store, fs *filestore.Store, ink *inkpic.Client, font, fontSize, overlay string, song *storage.Song) error {
	// Choose a song title
	if song.Title == "" {
		// Get random title matching the type
		titleFilters := []storage.Filter{
			storage.Where("type LIKE ?", song.Type),
			storage.Where("state = ?", storage.Approved),
		}
		// Order so the titles with a matching style are first
		orderBy := fmt.Sprintf("CASE WHEN style = '%s' THEN 1 ELSE 2 END, random()", song.Style)
		resp, err := store.ListTitles(ctx, 1, 1, orderBy, titleFilters...)
		if err != nil {
			return fmt.Errorf("single: couldn't get titles: %w", err)
		}
		if len(resp) == 0 {
			return fmt.Errorf("single: not enough titles")
		}
		song.Title = resp[0].Title
	}

	// Choose a random cover
	covers, err := store.ListCovers(ctx, 1, 1, "RANDOM()",
		storage.Where("state = ?", storage.Approved),
		storage.Where("type = ?", song.Type),
		storage.Where("draft_id = ''"),
		storage.Where("title = ''"),
	)
	if err != nil {
		return fmt.Errorf("single: couldn't get cover from database: %w", err)
	}
	if len(covers) == 0 {
		return errors.New("single: no cover found")
	}
	bg := covers[0]

	// Download background image
	bgName := filestore.JPG(bg.ID)
	bgPath := filepath.Join(os.TempDir(), bgName)
	if err := fs.GetJPG(ctx, bgPath, bg.ID); err != nil {
		return fmt.Errorf("single: couldn't download cover image: %w", err)
	}
	defer func() { _ = os.Remove(bgPath) }()

	// Add overlay to cover
	overlayedPath := filepath.Join(os.TempDir(), filestore.JPG(song.ID+"-overlayed"))
	if err := image.AddOverlay(overlay, bgPath, overlayedPath); err != nil {
		return fmt.Errorf("single: couldn't add overlay to cover: %w", err)
	}
	defer func() { _ = os.Remove(overlayedPath) }()

	// Add text to cover
	coverPath := filepath.Join(os.TempDir(), filestore.JPG(song.ID))
	text := strings.ToUpper(song.Title)
	if err := ink.AddText(ctx, overlayedPath, text, font, fontSize, coverPath); err != nil {
		return fmt.Errorf("single: couldn't add text to cover: %w", err)
	}
	defer func() { _ = os.Remove(coverPath) }()

	// Download song
	songName := filestore.MP3(*song.GenerationID)
	songPath := filepath.Join(os.TempDir(), songName)
	if err := fs.GetMP3(ctx, songPath, *song.GenerationID); err != nil {
		return fmt.Errorf("single: couldn't download song: %w", err)
	}
	defer func() { _ = os.Remove(songPath) }()

	// Create video from cover and song
	videoPath := filepath.Join(os.TempDir(), song.ID+".mp4")
	if err := ffmpeg.StaticVideo(ctx, coverPath, songPath, videoPath); err != nil {
		return fmt.Errorf("single: couldn't create video: %w", err)
	}
	defer func() { _ = os.Remove(videoPath) }()

	// Upload video to youtube
	_ = b
	return errors.New("single: not implemented yet")
}
