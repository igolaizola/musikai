package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	iofs "io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/igolaizola/musikai/pkg/cmd/album"
	"github.com/igolaizola/musikai/pkg/filestore"
	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug  bool
	DBType string
	DBConn string
	FSType string
	FSConn string
	Proxy  string

	Addr        string
	Credentials map[string]string
	Volumes     map[string]string
}

//go:embed static/*
var staticContent embed.FS

// Serve starts the filter service.
func Serve(ctx context.Context, cfg *Config) error {
	log.Println("filter: server started")
	defer log.Println("filter: server ended")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	debug := func(format string, args ...interface{}) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}
	_ = debug

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("scrape: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("scrape: couldn't start orm store: %w", err)
	}

	fs, err := filestore.New(cfg.FSType, cfg.FSConn, cfg.Proxy, cfg.Debug, store)
	if err != nil {
		return fmt.Errorf("download: couldn't create file storage: %w", err)
	}

	// Create static content
	staticFS, err := iofs.Sub(staticContent, "static")
	if err != nil {
		return fmt.Errorf("filter: couldn't load static content: %w", err)
	}

	// Create router
	mux := chi.NewRouter()

	// Add middleware
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.Timeout(60 * time.Second))

	// Add BasicAuth middleware
	if len(cfg.Credentials) > 0 {
		mux.Use(middleware.BasicAuth("private", cfg.Credentials))
	}

	// Create subrouter for api endpoints
	r := mux.Group(func(r chi.Router) {
		if cfg.Debug {
			r.Use(middleware.Logger)
		}
	})

	// Create server
	split := strings.Split(cfg.Addr, ":")
	if len(split) != 2 {
		return fmt.Errorf("filter: invalid address: %s", cfg.Addr)
	}
	host := split[0]
	port, err := strconv.Atoi(split[1])
	if err != nil {
		return fmt.Errorf("filter: invalid port: %s", split[1])
	}
	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: mux,
	}
	go func() {
		note := fmt.Sprintf("http://%s:%d", host, port)
		if host == "" {
			note = fmt.Sprintf("all interfaces http://localhost:%d", port)
		}
		log.Printf("Starting server on %s", note)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("failed to start server: %v\n", err)
			cancel()
		}
	}()

	cache := ".cache"
	if cfg.FSType == "local" {
		cache = cfg.FSConn
	}
	getMP3 := func(id string) string {
		name := filestore.MP3(id)
		u := fmt.Sprintf("/cache/%s", name)
		if _, err := os.Stat(fmt.Sprintf("%s/%s", cache, name)); err == nil {
			return u
		}
		out := fmt.Sprintf("%s/%s", cache, name)
		if err := fs.GetMP3(ctx, out, id); err != nil {
			log.Println("couldn't download mp3:", err)
			return ""
		}
		return u
	}
	getJPG := func(id string) string {
		name := filestore.JPG(id)
		u := fmt.Sprintf("/cache/%s", name)
		if _, err := os.Stat(fmt.Sprintf("%s/%s", cache, name)); err == nil {
			return u
		}
		out := fmt.Sprintf("%s/%s", cache, name)
		if err := fs.GetJPG(ctx, out, id); err != nil {
			log.Println("couldn't download jpg:", err)
			return ""
		}
		return u
	}

	// Handler to serve the static files
	mux.Get("/*", http.StripPrefix("/", http.FileServer(http.FS(staticFS))).ServeHTTP)

	// Handler to serve static files defined via volumes
	if len(cfg.Volumes) > 0 {
		for local, path := range cfg.Volumes {
			path = strings.Trim(path, "/")
			path = fmt.Sprintf("/%s/", path)
			mux.Get(path+"*", http.StripPrefix(path, http.FileServer(http.Dir(local))).ServeHTTP)
		}
	}

	// Handler to serve cached files "cache folder"
	mux.Get("/cache/*", http.StripPrefix("/cache/", http.FileServer(http.Dir(cache))).ServeHTTP)

	r.Get("/api/songs", func(w http.ResponseWriter, r *http.Request) {
		// Obtain page from query params
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			page = 1
		}
		size, err := strconv.Atoi(r.URL.Query().Get("size"))
		if err != nil {
			size = 100
		}
		filters := []storage.Filter{
			storage.Where("generations.processed = ?", true),
		}
		options := []string{"flagged", "ends"}
		for _, o := range options {
			if v := r.URL.Query().Get(o); v != "" {
				b := v == "true"
				filters = append(filters, storage.Where(fmt.Sprintf("%s = ?", o), b))
			}
		}
		if v := r.URL.Query().Get("liked"); v != "" {
			c := "="
			b := v == "true"
			if b {
				c = ">"
			}
			filters = append(filters, storage.Where(fmt.Sprintf("likes %s 0", c)))
		}

		var values []int
		states := []string{"pending", "rejected", "approved"}
		for i, s := range states {
			if v := r.URL.Query().Get(s); v != "" {
				if v == "true" {
					values = append(values, i)
				}
			}
		}
		if len(values) > 0 {
			filters = append(filters, storage.Where("state IN (?)", values))
		}

		queries := []string{"prompt", "style", "type"}
		for _, q := range queries {
			if v := r.URL.Query().Get(q); v != "" {
				filters = append(filters, storage.Where(fmt.Sprintf("songs.%s LIKE '%s'", q, v)))
			}
		}

		generations, err := store.ListGenerations(ctx, page, size, "songs.id desc", filters...)
		if err != nil {
			log.Println("couldn't list songs:", err)
			http.Error(w, fmt.Sprintf("couldn't list songs: %v", err), http.StatusInternalServerError)
			return
		}

		var assets []*Song
		for _, g := range generations {
			s := g.Song
			d := time.Duration(int(g.Duration)) * time.Second
			p := fmt.Sprintf("%s %.f BPM %s", d, g.Tempo, s.Type)
			if s.Prompt != "" {
				p += " | " + s.Prompt
			}
			if s.Style != "" && s.Style != s.Prompt {
				p += " | " + s.Style
			}
			if g.Flags != "" {
				p += " " + g.Flags
			}

			audioURL := g.Audio
			if g.Processed {
				audioURL = getMP3(g.ID)
			}
			waveURL := getJPG(g.ID)
			assets = append(assets, &Song{
				ID:           s.ID,
				GenerationID: g.ID,
				URL:          audioURL,
				ThumbnailURL: waveURL,
				Prompt:       p,
				State:        s.State,
				Liked:        s.Likes > 0,
				Selected:     g.ID == *s.GenerationID,
			})
		}
		if err := json.NewEncoder(w).Encode(assets); err != nil {
			log.Println("couldn't encode songs:", err)
			http.Error(w, fmt.Sprintf("couldn't encode songs: %v", err), http.StatusInternalServerError)
			return
		}
	})

	r.Put("/api/songs/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.State = storage.Approved
			return s
		})
	})
	r.Put("/api/songs/{id}/reject", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.Likes = 0
			s.State = storage.Rejected
			return s
		})
	})
	r.Put("/api/songs/{id}/like", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.State = storage.Approved
			s.Likes = 1
			return s
		})
	})
	r.Put("/api/songs/{id}/dislike", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.Likes = 0
			return s
		})
	})
	r.Put("/api/songs/{id}/undo", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.State = storage.Pending
			s.Likes = 0
			return s
		})
	})
	r.Put("/api/songs/{id}/select/{gid}", func(w http.ResponseWriter, r *http.Request) {
		gid := chi.URLParam(r, "gid")
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.Generation.ID = gid
			return s
		})
	})

	r.Get("/api/covers", func(w http.ResponseWriter, r *http.Request) {
		// Obtain page from query params
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			page = 1
		}
		filters := []storage.Filter{}
		draftFilters := []storage.Filter{}
		typ := r.URL.Query().Get("type")
		if typ != "" {
			draftFilters = append(draftFilters, storage.Where(fmt.Sprintf("type LIKE '%s'", typ)))
		}

		if v := r.URL.Query().Get("liked"); v != "" {
			c := "="
			b := v == "true"
			if b {
				c = ">"
			}
			filters = append(filters, storage.Where(fmt.Sprintf("likes %s 0", c)))
		}

		var background bool
		if v := r.URL.Query().Get("background"); v != "" {
			background = v == "true"
		}

		var values []int
		states := []string{"pending", "rejected", "approved"}
		for i, s := range states {
			if v := r.URL.Query().Get(s); v != "" {
				if v == "true" {
					values = append(values, i)
				}
			}
		}
		if len(values) > 0 {
			filters = append(filters, storage.Where("state IN (?)", values))
		}

		var coverPage int
		coverLimit := 1000
		var draftTitle string
		if !background {
			// Paginate by drafts
			draftFilters = append(draftFilters, storage.Where("state = ?", storage.Approved))
			drafts, err := store.ListDrafts(ctx, page, 1, "", draftFilters...)
			if err != nil {
				log.Println("couldn't list drafts:", err)
				http.Error(w, fmt.Sprintf("couldn't list drafts: %v", err), http.StatusInternalServerError)
				return
			}
			if len(drafts) == 0 {
				log.Println("no drafts found")
				http.Error(w, "Not drafts found", http.StatusNotFound)
				return
			}
			draft := drafts[0]
			draftTitle = draft.Title
			filters = append(filters, storage.Where("draft_id = ?", draft.ID))
		} else {
			// Paginate by covers
			coverPage = page
			coverLimit = 50
			if typ != "" {
				filters = append(filters, storage.Where(fmt.Sprintf("type LIKE '%s'", typ)))
			}
			filters = append(filters, storage.Where("draft_id = ?", ""))
		}

		covers, err := store.ListAllCovers(ctx, coverPage, coverLimit, "", filters...)
		if err != nil {
			log.Println("couldn't list covers:", err)
			http.Error(w, fmt.Sprintf("couldn't list covers: %v", err), http.StatusInternalServerError)
			return
		}
		var assets []*Asset
		for _, cover := range covers {
			thumbnail := strings.Replace(cover.URL(), "cdn.discordapp.com", "media.discordapp.net", 1)
			thumbnail += "?width=300&height=300"
			assets = append(assets, &Asset{
				ID:           cover.ID,
				URL:          cover.URL(),
				ThumbnailURL: thumbnail,
				Prompt:       fmt.Sprintf("%s %s", cover.Type, cover.Title), //cover.Prompt,
				State:        cover.State,
				Liked:        false,
			})
		}
		if len(assets) == 0 {
			assets = append(assets, &Asset{
				Prompt: fmt.Sprintf("%s - No covers found", draftTitle),
			})
		}
		if err := json.NewEncoder(w).Encode(assets); err != nil {
			log.Println("couldn't encode covers:", err)
			http.Error(w, fmt.Sprintf("couldn't encode covers: %v", err), http.StatusInternalServerError)
			return
		}
	})

	r.Put("/api/covers/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		updateCover(w, r, store, func(c *storage.Cover) *storage.Cover {
			c.State = storage.Approved
			return c
		})
	})
	r.Put("/api/covers/{id}/reject", func(w http.ResponseWriter, r *http.Request) {
		updateCover(w, r, store, func(c *storage.Cover) *storage.Cover {
			c.State = storage.Rejected
			c.Likes = 0
			return c
		})
	})
	r.Put("/api/covers/{id}/like", func(w http.ResponseWriter, r *http.Request) {
		updateCover(w, r, store, func(c *storage.Cover) *storage.Cover {
			c.State = storage.Approved
			c.Likes = 1
			return c
		})
	})
	r.Put("/api/covers/{id}/dislike", func(w http.ResponseWriter, r *http.Request) {
		updateCover(w, r, store, func(c *storage.Cover) *storage.Cover {
			c.Likes = 0
			return c
		})
	})
	r.Put("/api/covers/{id}/undo", func(w http.ResponseWriter, r *http.Request) {
		updateCover(w, r, store, func(c *storage.Cover) *storage.Cover {
			c.State = storage.Pending
			c.Likes = 0
			return c
		})
	})

	r.Get("/api/albums", func(w http.ResponseWriter, r *http.Request) {
		// Obtain page from query params
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			page = 1
		}
		filters := []storage.Filter{}
		var values []int
		states := []string{"pending", "rejected", "approved"}
		for i, s := range states {
			if v := r.URL.Query().Get(s); v != "" {
				if v == "true" {
					values = append(values, i)
				}
			}
		}
		if len(values) > 0 {
			filters = append(filters, storage.Where("state IN (?)", values))
		}

		queries := []string{"title", "type"}
		for _, q := range queries {
			if v := r.URL.Query().Get(q); v != "" {
				filters = append(filters, storage.Where(fmt.Sprintf("albums.%s LIKE '%s'", q, v)))
			}
		}

		albums, err := store.ListAlbums(ctx, page, 1, "", filters...)
		if err != nil {
			log.Println("couldn't list videos:", err)
			http.Error(w, fmt.Sprintf("couldn't list videos: %v", err), http.StatusInternalServerError)
			return
		}
		if len(albums) == 0 {
			http.Error(w, "couldn't find albums", http.StatusNotFound)
			return
		}
		a := albums[0]
		coverURL := getJPG(a.ID)

		title := a.Title
		if a.Subtitle != "" {
			title += " - " + a.Subtitle
		}
		if a.Volume > 0 {
			title = fmt.Sprintf("%s - Vol %d", title, a.Volume)
		}

		resp := &Album{
			ID:           a.ID,
			URL:          coverURL,
			ThumbnailURL: coverURL,
			Prompt:       fmt.Sprintf("%s | %s | %s", title, a.Artist, a.Type),
			State:        a.State,
		}

		songs, err := store.ListSongs(ctx, 1, 1000, "\"order\" asc", storage.Where("album_id = ?", a.ID))
		if err != nil {
			log.Println("couldn't list songs:", err)
			http.Error(w, fmt.Sprintf("couldn't list songs: %v", err), http.StatusInternalServerError)
			return
		}
		for _, s := range songs {
			g := s.Generation
			d := time.Duration(int(g.Duration)) * time.Second
			p := fmt.Sprintf("%d - %s | %s %.f BPM %s", s.Order, s.Title, d, g.Tempo, s.Type)

			audioURL := g.Audio
			if g.Processed {
				audioURL = getMP3(g.ID)
			}
			waveURL := getJPG(g.ID)

			resp.Songs = append(resp.Songs, &AlbumSong{
				ID:           s.ID,
				URL:          audioURL,
				ThumbnailURL: waveURL,
				Prompt:       p,
				State:        s.State,
				Liked:        s.Likes > 0,
			})
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Println("couldn't encode songs:", err)
			http.Error(w, fmt.Sprintf("couldn't encode songs: %v", err), http.StatusInternalServerError)
			return
		}
	})
	r.Put("/api/albums/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := chi.URLParam(r, "id")
		if err := album.RunDelete(ctx, &album.DeleteConfig{
			Debug:  cfg.Debug,
			DBType: cfg.DBType,
			DBConn: cfg.DBConn,
			ID:     id,
		}); err != nil {
			http.Error(w, fmt.Sprintf("couldn't delete album: %v", err), http.StatusInternalServerError)
			return
		}
	})

	r.Put("/api/albums/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		updateAlbum(w, r, store, func(a *storage.Album) *storage.Album {
			a.State = storage.Approved
			return a
		})
	})
	r.Put("/api/albums/{id}/disapprove", func(w http.ResponseWriter, r *http.Request) {
		updateAlbum(w, r, store, func(a *storage.Album) *storage.Album {
			a.State = storage.Pending
			return a
		})
	})
	r.Put("/api/albums/{id}/undo", func(w http.ResponseWriter, r *http.Request) {
		updateAlbum(w, r, store, func(a *storage.Album) *storage.Album {
			a.State = storage.Pending
			return a
		})
	})
	r.Put("/api/albums/{aid}/songs/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		var title string
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			title = s.Title
			s.AlbumID = ""
			s.Title = ""
			s.Order = 0
			s.State = storage.Approved
			return s
		})
		// Get title
		titles, err := store.ListTitles(ctx, 1, 1, "", storage.Where("title = ?", title))
		if err != nil {
			http.Error(w, fmt.Sprintf("couldn't get titles: %v", err), http.StatusNotFound)
			return
		}
		if len(titles) == 0 {
			http.Error(w, "couldn't find titles", http.StatusNotFound)
			return
		}
		updateTitle(w, r, store, titles[0].ID, func(t *storage.Title) *storage.Title {
			t.State = storage.Approved
			return t
		})
	})
	r.Put("/api/albums/{aid}/songs/{id}/add", func(w http.ResponseWriter, r *http.Request) {
		aid := chi.URLParam(r, "aid")
		album, err := store.GetAlbum(ctx, aid)
		if err != nil {
			http.Error(w, fmt.Sprintf("couldn't get album: %v", err), http.StatusNotFound)
			return
		}
		songs, err := store.ListSongs(ctx, 1, 1000, "", storage.Where("album_id = ?", aid))
		if err != nil {
			http.Error(w, fmt.Sprintf("couldn't get songs: %v", err), http.StatusNotFound)
			return
		}

		id := chi.URLParam(r, "id")
		if id == "-" {
			// Get random songs matching the type
			songFilters := []storage.Filter{
				storage.Where("type LIKE ?", album.Type),
				storage.Where("state = ?", storage.Approved),
			}
			songs, err := store.ListSongs(ctx, 1, 1, "random()", songFilters...)
			if err != nil {
				http.Error(w, fmt.Sprintf("couldn't get songs: %v", err), http.StatusNotFound)
				return
			}
			if len(songs) == 0 {
				http.Error(w, "couldn't find songs", http.StatusNotFound)
				return
			}
			id = songs[0].ID
		}

		// Get random titles matching the type
		titleFilters := []storage.Filter{
			storage.Where("type LIKE ?", album.Type),
			storage.Where("state = ?", storage.Approved),
		}
		titles, err := store.ListTitles(ctx, 1, 1, "random()", titleFilters...)
		if err != nil {
			http.Error(w, fmt.Sprintf("couldn't get titles: %v", err), http.StatusNotFound)
			return
		}
		if len(titles) == 0 {
			http.Error(w, "couldn't find titles", http.StatusNotFound)
			return
		}
		title := titles[0]

		updateSongWithID(w, r, store, id, func(s *storage.Song) *storage.Song {
			s.AlbumID = aid
			s.Title = title.Title
			s.Order = len(songs) + 1
			s.State = storage.Used
			return s
		})
		updateTitle(w, r, store, title.ID, func(t *storage.Title) *storage.Title {
			t.State = storage.Used
			return t
		})
	})

	<-ctx.Done()
	return nil
}

func updateSong(w http.ResponseWriter, r *http.Request, store *storage.Store, update func(s *storage.Song) *storage.Song) {
	id := chi.URLParam(r, "id")
	updateSongWithID(w, r, store, id, update)
}

func updateSongWithID(w http.ResponseWriter, r *http.Request, store *storage.Store, id string, update func(s *storage.Song) *storage.Song) {
	ctx := r.Context()
	song, err := store.GetSong(ctx, id)
	if err != nil {
		http.Error(w, fmt.Sprintf("couldn't get song: %v", err), http.StatusNotFound)
		return
	}
	song = update(song)
	if err := store.SetSong(ctx, song); err != nil {
		log.Println("couldn't set song:", err)
		http.Error(w, fmt.Sprintf("couldn't set song: %v", err), http.StatusInternalServerError)
		return
	}
}

func updateCover(w http.ResponseWriter, r *http.Request, store *storage.Store, update func(s *storage.Cover) *storage.Cover) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	cover, err := store.GetCover(ctx, id)
	if err != nil {
		http.Error(w, fmt.Sprintf("couldn't get cover: %v", err), http.StatusNotFound)
		return
	}
	cover = update(cover)
	if err := store.SetCover(ctx, cover); err != nil {
		log.Println("couldn't set cover:", err)
		http.Error(w, fmt.Sprintf("couldn't set cover: %v", err), http.StatusInternalServerError)
		return
	}
}

func updateAlbum(w http.ResponseWriter, r *http.Request, store *storage.Store, update func(s *storage.Album) *storage.Album) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	album, err := store.GetAlbum(ctx, id)
	if err != nil {
		http.Error(w, fmt.Sprintf("couldn't get album: %v", err), http.StatusNotFound)
		return
	}
	album = update(album)
	if err := store.SetAlbum(ctx, album); err != nil {
		log.Println("couldn't set album:", err)
		http.Error(w, fmt.Sprintf("couldn't set album: %v", err), http.StatusInternalServerError)
		return
	}
}

func updateTitle(w http.ResponseWriter, r *http.Request, store *storage.Store, id string, update func(s *storage.Title) *storage.Title) {
	ctx := r.Context()
	title, err := store.GetTitle(ctx, id)
	if err != nil {
		http.Error(w, fmt.Sprintf("couldn't get title: %v", err), http.StatusNotFound)
		return
	}
	title = update(title)
	if err := store.SetTitle(ctx, title); err != nil {
		log.Println("couldn't set title:", err)
		http.Error(w, fmt.Sprintf("couldn't set title: %v", err), http.StatusInternalServerError)
		return
	}
}

type Song struct {
	ID           string        `json:"id"`
	GenerationID string        `json:"generation_id"`
	URL          string        `json:"url"`
	ThumbnailURL string        `json:"thumbnail_url"`
	Prompt       string        `json:"prompt"`
	State        storage.State `json:"state"`
	Liked        bool          `json:"liked"`
	Selected     bool          `json:"selected"`
}

type Album struct {
	ID           string        `json:"id"`
	URL          string        `json:"url"`
	ThumbnailURL string        `json:"thumbnail_url"`
	Prompt       string        `json:"prompt"`
	State        storage.State `json:"state"`
	Songs        []*AlbumSong  `json:"songs"`
}

type AlbumSong struct {
	ID           string        `json:"id"`
	URL          string        `json:"url"`
	ThumbnailURL string        `json:"thumbnail_url"`
	Prompt       string        `json:"prompt"`
	State        storage.State `json:"state"`
	Liked        bool          `json:"liked"`
}

type Asset struct {
	ID           string        `json:"id"`
	URL          string        `json:"url"`
	ThumbnailURL string        `json:"thumbnail_url"`
	Prompt       string        `json:"prompt"`
	State        storage.State `json:"state"`
	Liked        bool          `json:"liked"`
}
