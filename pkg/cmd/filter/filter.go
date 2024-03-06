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
	Debug  bool
	DBType string
	DBConn string
	Port   int

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
	r := mux.Group(func(r chi.Router) {
		r.Use(middleware.Logger)
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
		options := []string{"approved", "flagged", "disabled", "ends"}
		for _, o := range options {
			if v := r.URL.Query().Get(o); v != "" {
				b := v == "true"
				filters = append(filters, storage.Where(fmt.Sprintf("%s = ?", o), b))
			}
		}

		if query := r.URL.Query().Get("query"); query != "" {
			fmt.Println("query:", query)
			filters = append(filters, storage.Where(fmt.Sprintf("songs.type LIKE '%s'", query)))
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
				p += ", " + s.Prompt
			}
			if s.Style != "" {
				p += ", " + s.Style
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
				Approved:     s.Approved,
				Disabled:     s.Disabled,
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
			s.Approved = true
			return s
		})
	})

	r.Put("/api/songs/{id}/disapprove", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.Approved = false
			return s
		})
	})

	r.Put("/api/songs/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.Disabled = true
			return s
		})
	})

	r.Put("/api/songs/{id}/enable", func(w http.ResponseWriter, r *http.Request) {
		updateSong(w, r, store, func(s *storage.Song) *storage.Song {
			s.Disabled = false
			return s
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

type Asset struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Prompt       string `json:"prompt"`
	Approved     bool   `json:"approved"`
	Disabled     bool   `json:"disabled"`
}
