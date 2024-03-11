package album

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/igolaizola/musikai/pkg/filestorage/tgstore"
	"github.com/igolaizola/musikai/pkg/image"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/oklog/ulid/v2"
)

type Config struct {
	Debug   bool
	DBType  string
	DBConn  string
	Timeout time.Duration
	Limit   int
	Proxy   string

	TGChat  int64
	TGToken string

	Type     string
	MinSongs int
	MaxSongs int
	Artist   string
	Overlay  string
	Font     string
}

// Run launches the song generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Printf("album: album started\n")
	defer func() {
		log.Printf("album: album ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...any) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	if cfg.MinSongs == 0 {
		return fmt.Errorf("album: min songs not set")
	}
	if cfg.MaxSongs < cfg.MinSongs {
		return fmt.Errorf("album: max songs must equal or greater than min songs")
	}
	if cfg.Artist == "" {
		return fmt.Errorf("album: artist not set")
	}
	if cfg.Overlay == "" {
		return fmt.Errorf("album: overlay file not set")
	}

	// Check if overlay file exists
	if _, err := os.Stat(cfg.Overlay); err != nil {
		return fmt.Errorf("album: couldn't find overlay file: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("album: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("album: couldn't start orm store: %w", err)
	}

	tgStore, err := tgstore.New(cfg.TGToken, cfg.TGChat, cfg.Proxy, cfg.Debug)
	if err != nil {
		return fmt.Errorf("album: couldn't create file storage: %w", err)
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
		log.Printf("album: total time %s, average time %s\n", total, total/time.Duration(iteration))
	}()

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 24 * time.Hour
	}
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("album: %w", ctx.Err())
		case <-ticker.C:
			return nil
		default:
		}

		// Check exit conditions
		if cfg.Limit > 0 && iteration >= cfg.Limit {
			return nil
		}

		iteration++

		// Get next draft
		filters := []storage.Filter{}
		if cfg.Type != "" {
			filters = append(filters, storage.Where("drafts.type LIKE ?", cfg.Type))
		}
		next, err := store.NextDraftCoverSongs(ctx, cfg.MinSongs, "", filters...)
		if err != nil {
			return fmt.Errorf("album: couldn't get next draft: %w", err)
		}

		// Choose randomly number of songs
		min := cfg.MinSongs
		max := cfg.MaxSongs
		if max > next.Songs {
			max = next.Songs
		}
		n := rand.Intn(max-min) + min

		// Get random songs matching the type
		songsFilters := []storage.Filter{
			storage.Where("state = ?", storage.Approved),
			storage.Where("type LIKE ?", next.Draft.Type),
			storage.Where("album_id = ?", ""),
		}
		songs, err := store.ListSongs(ctx, 1, n, "random()", songsFilters...)
		if err != nil {
			return fmt.Errorf("album: couldn't get songs: %w", err)
		}
		if len(songs) < min {
			return fmt.Errorf("album: not enough songs")
		}
		n = len(songs)

		// Get random titles matching the type
		titleFilters := []storage.Filter{
			storage.Where("type LIKE ?", next.Draft.Type),
			storage.Where("used = ?", false),
		}
		titles, err := store.ListTitles(ctx, 1, n, "random()", titleFilters...)
		if err != nil {
			return fmt.Errorf("album: couldn't get titles: %w", err)
		}
		if len(titles) < n {
			return fmt.Errorf("album: not enough titles")
		}

		// Get random cover matching the draft title
		coverFilters := []storage.Filter{
			storage.Where("state = ?", storage.Approved),
			storage.Where("upscaled = ?", true),
			storage.Where("title = ?", next.Draft.Title),
		}
		covers, err := store.ListCovers(ctx, 1, 1, "random()", coverFilters...)
		if err != nil {
			return fmt.Errorf("album: couldn't get cover: %w", err)
		}
		if len(covers) == 0 {
			return fmt.Errorf("album: no cover found")
		}
		cover := covers[0]

		debug("album: start download cover %s", cover.ID)
		original := filepath.Join(os.TempDir(), fmt.Sprintf("%s.jpg", cover.ID))
		if err := tgStore.Download(ctx, cover.UpscaleID, original); err != nil {
			return fmt.Errorf("album: couldn't download cover image: %w", err)
		}
		debug("album: end download cover %s", cover.ID)

		albumID := ulid.Make().String()

		edited := filepath.Join(os.TempDir(), fmt.Sprintf("%s.jpg", albumID))

		// Add subtitle to cover
		subtitle := next.Draft.Subtitle
		if subtitle != "" {
			if err := image.AddText(original, image.BottomLeft, cfg.Font, subtitle, edited); err != nil {
				return fmt.Errorf("album: couldn't add subtitle to cover: %w", err)
			}
		}

		// Add overlay to cover
		if err := image.AddOverlay(cfg.Overlay, edited, edited); err != nil {
			return fmt.Errorf("album: couldn't add overlay to cover: %w", err)
		}

		// Upload cover to telegram
		debug("album: upload start %s", albumID)
		upscaledID, err := tgStore.Set(ctx, edited)
		if err != nil {
			return fmt.Errorf("album: couldn't upload cover image: %w", err)
		}
		debug("album: upload end %s", albumID)

		// Create the album
		album := &storage.Album{
			ID:       albumID,
			Type:     next.Draft.Type,
			Artist:   cfg.Artist,
			Title:    next.Draft.Title,
			Subtitle: next.Draft.Subtitle,
			Volume:   0,
			Cover:    upscaledID,
			State:    storage.Pending,
		}
		if err := store.SetAlbum(ctx, album); err != nil {
			return fmt.Errorf("album: couldn't set album: %w", err)
		}
		return nil

		// Assign album id and title to songs
		for i, song := range songs {
			song.AlbumID = album.ID
			song.Title = titles[i].ID
			if err := store.SetSong(ctx, song); err != nil {
				return fmt.Errorf("album: couldn't set song: %w", err)
			}
		}

		// Mark titles as used
		for _, title := range titles {
			title.Used = true
			if err := store.SetTitle(ctx, title); err != nil {
				return fmt.Errorf("album: couldn't set title: %w", err)
			}
		}
	}

}
