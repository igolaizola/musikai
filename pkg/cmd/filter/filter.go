package filter

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug  bool
	DBType string
	DBConn string
	Port   int

	Disabled bool
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
			// TODO: Add filters
			storage.Where("disabled = ?", cfg.Disabled),
			storage.Where("processed = ?", true),
			storage.Where("flags != ?", ""),
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

			assets = append(assets, &Asset{
				ID:           s.ID,
				URL:          s.SunoAudio,
				ThumbnailURL: s.Wave,
				Prompt:       p,
			})
		}
		if err := json.NewEncoder(w).Encode(assets); err != nil {
			log.Println("couldn't encode songs:", err)
			http.Error(w, fmt.Sprintf("couldn't encode songs: %v", err), http.StatusInternalServerError)
			return
		}
	})

	r.Delete("/api/songs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		song, err := store.GetSong(ctx, id)
		if err != nil {
			http.Error(w, fmt.Sprintf("couldn't get song: %v", err), http.StatusNotFound)
			return
		}
		song.Disabled = true
		if err := store.SetSong(ctx, song); err != nil {
			log.Println("couldn't set song:", err)
			http.Error(w, fmt.Sprintf("couldn't set song: %v", err), http.StatusInternalServerError)
			return
		}
	})

	<-ctx.Done()
	return nil
}

type Asset struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Prompt       string `json:"prompt"`
}
