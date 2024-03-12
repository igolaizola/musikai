package filter

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/igolaizola/musikai/pkg/filestorage/tgstore"
	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug       bool
	DBType      string
	DBConn      string
	Port        int
	Credentials map[string]string

	Proxy   string
	TGChat  int64
	TGToken string
}

//go:embed web/*
var webContent embed.FS

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

	tgStore, err := tgstore.New(cfg.TGToken, cfg.TGChat, cfg.Proxy, cfg.Debug)
	if err != nil {
		return fmt.Errorf("process: couldn't create file storage: %w", err)
	}

	// Create web static content
	webFS, err := fs.Sub(webContent, "web")
	if err != nil {
		return fmt.Errorf("filter: couldn't load web content: %w", err)
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
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: mux,
	}
	go func() {
		log.Printf("Starting server on http://localhost:%d", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("failed to start server: %v\n", err)
			cancel()
		}
	}()

	// Handler to serve the static files
	mux.Get("/*", http.StripPrefix("/", http.FileServer(http.FS(webFS))).ServeHTTP)

	// Handler to serve cached files "cache folder"
	mux.Get("/cache/*", http.StripPrefix("/cache/", http.FileServer(http.Dir(".cache"))).ServeHTTP)

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
			storage.Where("processed = ?", true),
		}
		options := []string{"flagged", "liked", "ends"}
		for _, o := range options {
			if v := r.URL.Query().Get(o); v != "" {
				b := v == "true"
				filters = append(filters, storage.Where(fmt.Sprintf("%s = ?", o), b))
			}
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

		songs, err := store.ListAllSongs(ctx, page, size, "", filters...)
		if err != nil {
			log.Println("couldn't list videos:", err)
			http.Error(w, fmt.Sprintf("couldn't list videos: %v", err), http.StatusInternalServerError)
			return
		}
		var assets []*Asset
		for _, s := range songs {
			d := time.Duration(int(s.Duration)) * time.Second
			p := fmt.Sprintf("%s %.f BPM %s", d, s.Tempo, s.Type)
			if s.Prompt != "" {
				p += " | " + s.Prompt
			}
			if s.Style != "" {
				p += " | " + s.Style
			}
			if s.Flags != "" {
				p += " " + s.Flags
			}

			audioURL := s.SunoAudio
			if _, err := os.Stat(fmt.Sprintf(".cache/%s.mp3", s.ID)); err == nil {
				audioURL = fmt.Sprintf("/cache/%s.mp3", s.ID)
			} else if s.Master != "" {
				audioURL = s.Master
				if !strings.HasPrefix(s.Master, "http") {
					audioURL, err = tgStore.Get(ctx, s.Master)
					if err != nil {
						log.Println("couldn't get master:", err)
						http.Error(w, fmt.Sprintf("couldn't get master: %v", err), http.StatusInternalServerError)
						return
					}
				}
			}

			waveURL := s.Wave
			if _, err := os.Stat(fmt.Sprintf(".cache/%s.jpg", s.ID)); err == nil {
				waveURL = fmt.Sprintf("/cache/%s.jpg", s.ID)
			} else if s.Wave != "" {
				waveURL = s.Wave
				if !strings.HasPrefix(s.Wave, "http") {
					audioURL, err = tgStore.Get(ctx, s.Wave)
					if err != nil {
						log.Println("couldn't get wave:", err)
						http.Error(w, fmt.Sprintf("couldn't get wave: %v", err), http.StatusInternalServerError)
						return
					}
				}
			}

			assets = append(assets, &Asset{
				ID:           s.ID,
				URL:          audioURL,
				ThumbnailURL: waveURL,
				Prompt:       p,
				State:        s.State,
				Liked:        s.Liked,
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
			s.State = storage.Rejected
			return s
		})
	})
	r.Put("/api/songs/{id}/like", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.State = storage.Approved
			s.Liked = true
			return s
		})
	})
	r.Put("/api/songs/{id}/dislike", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.Liked = false
			return s
		})
	})

	r.Get("/api/covers", func(w http.ResponseWriter, r *http.Request) {
		// Obtain page from query params
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			page = 1
		}
		size, err := strconv.Atoi(r.URL.Query().Get("size"))
		if err != nil {
			size = 100
		}
		filters := []storage.Filter{}
		if query := r.URL.Query().Get("query"); query != "" {
			fmt.Println("query:", query)
			filters = append(filters, storage.Where(fmt.Sprintf("type LIKE '%s'", query)))
		}

		options := []string{"liked"}
		for _, o := range options {
			if v := r.URL.Query().Get(o); v != "" {
				b := v == "true"
				filters = append(filters, storage.Where(fmt.Sprintf("%s = ?", o), b))
			}
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

		covers, err := store.ListAllCovers(ctx, page, size, "", filters...)
		if err != nil {
			log.Println("couldn't list covers:", err)
			http.Error(w, fmt.Sprintf("couldn't list covers: %v", err), http.StatusInternalServerError)
			return
		}
		var assets []*Asset
		for _, cover := range covers {
			thumbnail := strings.Replace(cover.URL(), "cdn.discordapp.com", "media.discordapp.net", 1)
			thumbnail += "?width=224&height=224"
			assets = append(assets, &Asset{
				ID:           cover.ID,
				URL:          cover.URL(),
				ThumbnailURL: thumbnail,
				Prompt:       fmt.Sprintf("%s %s", cover.Type, cover.Title), //cover.Prompt,
				State:        cover.State,
				Liked:        false,
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
			return c
		})
	})
	r.Put("/api/covers/{id}/like", func(w http.ResponseWriter, r *http.Request) {
		updateCover(w, r, store, func(s *storage.Cover) *storage.Cover {
			s.State = storage.Approved
			s.Liked = true
			return s
		})
	})
	r.Put("/api/covers/{id}/dislike", func(w http.ResponseWriter, r *http.Request) {
		updateCover(w, r, store, func(s *storage.Cover) *storage.Cover {
			s.Liked = false
			return s
		})
	})

	r.Get("/api/albums", func(w http.ResponseWriter, r *http.Request) {
		// Obtain page from query params
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			page = 1
		}
		size, err := strconv.Atoi(r.URL.Query().Get("size"))
		if err != nil {
			size = 100
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

		albums, err := store.ListAlbums(ctx, page, size, "", filters...)
		if err != nil {
			log.Println("couldn't list videos:", err)
			http.Error(w, fmt.Sprintf("couldn't list videos: %v", err), http.StatusInternalServerError)
			return
		}
		var assets []*Asset
		for _, a := range albums {
			coverURL := a.Cover
			if _, err := os.Stat(fmt.Sprintf(".cache/%s.jpg", a.ID)); err == nil {
				coverURL = fmt.Sprintf("/cache/%s.jpg", a.ID)
			} else if a.Cover != "" {
				coverURL = a.Cover
				if !strings.HasPrefix(a.Cover, "http") {
					if err := tgStore.Download(ctx, a.Cover, fmt.Sprintf(".cache/%s.jpg", a.ID)); err != nil {
						log.Println("couldn't get cover:", err)
						http.Error(w, fmt.Sprintf("couldn't get cover: %v", err), http.StatusInternalServerError)
						return
					}
				}
			}

			title := a.Title
			if a.Subtitle != "" {
				title += " - " + a.Subtitle
			}
			if a.Volume > 0 {
				title += fmt.Sprintf(" - Vol %d", a.Volume)
			}
			assets = append(assets, &Asset{
				ID:           a.ID,
				URL:          coverURL,
				ThumbnailURL: coverURL,
				Prompt:       fmt.Sprintf("%s | %s | %s | %s", a.Title, a.Artist, a.Type, a.ID),
				State:        a.State,
			})

			songs, err := store.ListSongs(ctx, 1, 1000, "\"order\" asc", storage.Where("album_id = ?", a.ID))
			if err != nil {
				log.Println("couldn't list songs:", err)
				http.Error(w, fmt.Sprintf("couldn't list songs: %v", err), http.StatusInternalServerError)
				return
			}
			for _, s := range songs {
				d := time.Duration(int(s.Duration)) * time.Second
				p := fmt.Sprintf("%d - %s | %s %.f BPM %s", s.Order, s.Title, d, s.Tempo, s.Type)

				audioURL := s.SunoAudio
				if _, err := os.Stat(fmt.Sprintf(".cache/%s.mp3", s.ID)); err == nil {
					audioURL = fmt.Sprintf("/cache/%s.mp3", s.ID)
				} else if s.Master != "" {
					audioURL = s.Master
					if !strings.HasPrefix(s.Master, "http") {
						audioURL, err = tgStore.Get(ctx, s.Master)
						if err != nil {
							log.Println("couldn't get master:", err)
							http.Error(w, fmt.Sprintf("couldn't get master: %v", err), http.StatusInternalServerError)
							return
						}
					}
				}

				waveURL := s.Wave
				if _, err := os.Stat(fmt.Sprintf(".cache/%s.jpg", s.ID)); err == nil {
					waveURL = fmt.Sprintf("/cache/%s.jpg", s.ID)
				} else if s.Wave != "" {
					waveURL = s.Wave
					if !strings.HasPrefix(s.Wave, "http") {
						audioURL, err = tgStore.Get(ctx, s.Wave)
						if err != nil {
							log.Println("couldn't get wave:", err)
							http.Error(w, fmt.Sprintf("couldn't get wave: %v", err), http.StatusInternalServerError)
							return
						}
					}
				}

				assets = append(assets, &Asset{
					ID:           s.ID,
					URL:          audioURL,
					ThumbnailURL: waveURL,
					Prompt:       p,
					State:        s.State,
					Liked:        s.Liked,
				})
			}
		}
		if err := json.NewEncoder(w).Encode(assets); err != nil {
			log.Println("couldn't encode songs:", err)
			http.Error(w, fmt.Sprintf("couldn't encode songs: %v", err), http.StatusInternalServerError)
			return
		}
	})
	r.Put("/api/albums/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		updateAlbum(w, r, store, func(c *storage.Album) *storage.Album {
			c.State = storage.Approved
			return c
		})
	})
	r.Put("/api/albums/{id}/disapprove", func(w http.ResponseWriter, r *http.Request) {
		updateAlbum(w, r, store, func(c *storage.Album) *storage.Album {
			c.State = storage.Pending
			return c
		})
	})

	<-ctx.Done()
	return nil
}

func updateSong(w http.ResponseWriter, r *http.Request, store *storage.Store, update func(s *storage.Song) *storage.Song) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
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

type Asset struct {
	ID           string        `json:"id"`
	URL          string        `json:"url"`
	ThumbnailURL string        `json:"thumbnail_url"`
	Prompt       string        `json:"prompt"`
	State        storage.State `json:"state"`
	Liked        bool          `json:"liked"`
}
