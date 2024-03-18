package album

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gocarina/gocsv"
	"github.com/igolaizola/musikai/pkg/distrokid"
	"github.com/igolaizola/musikai/pkg/filestore"
	"github.com/igolaizola/musikai/pkg/image"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/oklog/ulid/v2"
)

type Config struct {
	Debug   bool
	DBType  string
	DBConn  string
	FSType  string
	FSConn  string
	Timeout time.Duration
	Limit   int
	Proxy   string

	Type     string
	MinSongs int
	MaxSongs int
	Artist   string
	Overlay  string
	Font     string
	Genres   string
}

type typeGenres struct {
	Type      string `json:"type" csv:"type"`
	Primary   string `json:"primary" csv:"primary"`
	Secondary string `json:"secondary" csv:"secondary"`
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

	// Check if genres file exists
	if _, err := os.Stat(cfg.Genres); err != nil {
		return fmt.Errorf("album: couldn't find genres file: %w", err)
	}
	genres, err := toGenres(cfg.Genres)
	if err != nil {
		return fmt.Errorf("album: couldn't parse genres: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("album: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("album: couldn't start orm store: %w", err)
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
		i := iteration
		if i == 0 {
			i = 1
		}
		log.Printf("album: total time %s, average time %s\n", total, total/time.Duration(i))
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

		// Get next draft
		filters := []storage.Filter{}
		if cfg.Type != "" {
			filters = append(filters, storage.Where("drafts.type LIKE ?", cfg.Type))
		}
		draft, err := store.NextDraftCandidate(ctx, cfg.MinSongs, "", filters...)
		if err != nil {
			return fmt.Errorf("album: couldn't get next draft: %w", err)
		}

		// Get primary and secondary genres
		gs, ok := genres[draft.Type]
		if !ok {
			return fmt.Errorf("album: couldn't find genre %s", draft.Type)
		}
		primaryGenre := gs[0]
		secondaryGenre := gs[1]

		// If volumes is enabled, obtain the last volume
		var cover *storage.Cover
		var volume int
		if draft.Volumes > 0 {
			volume = 1
			albumFilters := []storage.Filter{
				storage.Where("draft_id = ?", draft.ID),
			}
			albums, err := store.ListAlbums(ctx, 1, 1, "volume desc", albumFilters...)
			if err != nil {
				return fmt.Errorf("album: couldn't get last volume: %w", err)
			}
			if len(albums) > 0 {
				volume = albums[0].Volume + 1
				// Get cover from last volume
				cover, err = store.GetCover(ctx, albums[0].CoverID)
				if err != nil {
					return fmt.Errorf("album: couldn't get cover: %w", err)
				}
			}
		}

		if cover == nil {
			// Get random cover matching the draft title
			coverFilters := []storage.Filter{
				storage.Where("state = ?", storage.Approved),
				storage.Where("upscaled = ?", true),
				storage.Where("title = ?", draft.Title),
			}
			covers, err := store.ListCovers(ctx, 1, 1, "likes desc, random()", coverFilters...)
			if err != nil {
				return fmt.Errorf("album: couldn't get cover: %w", err)
			}
			if len(covers) == 0 {
				return fmt.Errorf("album: no cover found")
			}
			cover = covers[0]
		}

		if cover.Title != draft.Title {
			return fmt.Errorf("album: cover title doesn't match draft title %s %s", cover.Title, draft.Title)
		}

		// Get random songs matching the type
		songsFilters := []storage.Filter{
			storage.Where("state = ?", storage.Approved),
			storage.Where("type LIKE ?", draft.Type),
			storage.Where("album_id = ?", ""),
		}
		songs, err := store.ListSongs(ctx, 1, cfg.MaxSongs, "likes desc, random()", songsFilters...)
		if err != nil {
			return fmt.Errorf("album: couldn't get songs: %w", err)
		}
		if len(songs) < cfg.MinSongs {
			return fmt.Errorf("album: not enough songs")
		}

		// Choose randomly number of songs
		n := len(songs)
		if n > cfg.MinSongs {
			n = rand.Intn(n-cfg.MinSongs) + cfg.MinSongs
		}
		songs = songs[:n]

		// Get random titles matching the type
		titleFilters := []storage.Filter{
			storage.Where("type LIKE ?", draft.Type),
			storage.Where("state = ?", storage.Approved),
		}
		titles, err := store.ListTitles(ctx, 1, n, "random()", titleFilters...)
		if err != nil {
			return fmt.Errorf("album: couldn't get titles: %w", err)
		}
		if len(titles) < n {
			return fmt.Errorf("album: not enough titles")
		}

		debug("album: start download cover %s", cover.ID)
		name := filestore.JPG(cover.ID)
		original := filepath.Join(os.TempDir(), name)
		if err := fs.GetJPG(ctx, original, cover.ID); err != nil {
			return fmt.Errorf("album: couldn't download cover image: %w", err)
		}
		defer func() { _ = os.Remove(original) }()
		debug("album: end download cover %s", cover.ID)

		albumID := ulid.Make().String()

		input := original
		output := filepath.Join(os.TempDir(), fmt.Sprintf("%s.jpeg", albumID))
		defer func() { _ = os.Remove(output) }()

		// Add subtitle to cover
		subtitle := draft.Subtitle
		if volume > 0 {
			if subtitle != "" {
				subtitle += "\n"
			}
			subtitle = fmt.Sprintf("%sVol %d", subtitle, volume)
		}
		if subtitle != "" {
			log.Println("Adding subtitle to cover", subtitle)
			if err := image.AddText(subtitle, image.BottomLeft, cfg.Font, input, output); err != nil {
				return fmt.Errorf("album: couldn't add subtitle to cover: %w", err)
			}
			input = output
		}

		// Add overlay to cover
		if err := image.AddOverlay(cfg.Overlay, input, output); err != nil {
			return fmt.Errorf("album: couldn't add overlay to cover: %w", err)
		}

		// Upload cover to telegram
		debug("album: upload start %s", albumID)
		if err := fs.GetJPG(ctx, output, albumID); err != nil {
			return fmt.Errorf("album: couldn't upload cover image: %w", err)
		}
		debug("album: upload end %s", albumID)

		// Create the album
		album := &storage.Album{
			ID:             albumID,
			CoverID:        cover.ID,
			DraftID:        draft.ID,
			Type:           draft.Type,
			Artist:         cfg.Artist,
			Title:          draft.Title,
			Subtitle:       draft.Subtitle,
			Volume:         volume,
			PrimaryGenre:   primaryGenre,
			SecondaryGenre: secondaryGenre,
			State:          storage.Pending,
		}
		if err := store.SetAlbum(ctx, album); err != nil {
			return fmt.Errorf("album: couldn't set album: %w", err)
		}

		js, _ := json.MarshalIndent(album, "", "  ")
		fmt.Println(string(js))

		// Assign album id, order and title to songs
		for i, song := range songs {
			song.AlbumID = album.ID
			song.Title = titles[i].Title
			song.Order = i + 1
			song.State = storage.Used
			if err := store.SetSong(ctx, song); err != nil {
				return fmt.Errorf("album: couldn't set song: %w", err)
			}
		}

		// Mark titles as used
		for _, title := range titles {
			title.State = storage.Used
			if err := store.SetTitle(ctx, title); err != nil {
				return fmt.Errorf("album: couldn't set title: %w", err)
			}
		}

		// Mark draft as used if max volume is reached
		if draft.Volumes == 0 || volume >= draft.Volumes {
			draft.State = storage.Used
			if err := store.SetDraft(ctx, draft); err != nil {
				return fmt.Errorf("album: couldn't set draft: %w", err)
			}
		}

		// Mark cover as used
		if cover.State != storage.Used {
			cover.State = storage.Used
			if err := store.SetCover(ctx, cover); err != nil {
				return fmt.Errorf("album: couldn't set cover: %w", err)
			}
		}
		iteration++
	}

}

func toGenres(input string) (map[string][2]string, error) {
	b, err := os.ReadFile(input)
	if err != nil {
		return nil, fmt.Errorf("album: couldn't read input file: %w", err)
	}

	ext := filepath.Ext(input)
	var unmarshal func([]byte) ([]*typeGenres, error)
	switch ext {
	case ".json":
		unmarshal = func(b []byte) ([]*typeGenres, error) {
			var is []*typeGenres
			if err := json.Unmarshal(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	case ".csv":
		// Check for inconsistent number of fields in csv
		lines := strings.Split(string(b), "\n")
		commas := strings.Count(lines[0], ",")
		for i, l := range lines {
			if l == "" {
				continue
			}
			if commas != strings.Count(l, ",") {
				return nil, fmt.Errorf("album: inconsistent number of fields in csv %d (%s)", i, l)
			}
		}
		unmarshal = func(b []byte) ([]*typeGenres, error) {
			var is []*typeGenres
			if err := gocsv.UnmarshalBytes(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	default:
		return nil, fmt.Errorf("album: unsupported output format: %s", ext)
	}
	genres, err := unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("album: couldn't unmarshal input: %w", err)
	}

	// Load distrokid genres
	dkGenres := distrokid.Genres
	dkLookup := map[string]struct{}{}
	for _, g := range dkGenres {
		dkLookup[g] = struct{}{}
	}

	// Create lookup table
	lookup := map[string][2]string{}
	for _, g := range genres {
		// Validate genre
		if _, ok := dkLookup[g.Primary]; !ok {
			return nil, fmt.Errorf("album: invalid primary genre %s %s", g.Type, g.Primary)
		}
		if g.Secondary != "" {
			if _, ok := dkLookup[g.Secondary]; !ok {
				return nil, fmt.Errorf("album: invalid secondary genre %s %s", g.Type, g.Primary)
			}
		}
		lookup[g.Type] = [2]string{g.Primary, g.Secondary}
	}
	return lookup, nil
}
