package draft

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/gocarina/gocsv"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/oklog/ulid/v2"
)

type Config struct {
	Debug   bool
	DBType  string
	DBConn  string
	Limit   int
	Input   string
	Type    string
	Volumes int
}

type draft struct {
	Type     string `json:"type" csv:"type"`
	Title    string `json:"title" csv:"title"`
	Subtitle string `json:"keywords" csv:"subtitle"`
	Volumes  int    `json:"volumes" csv:"volumes"`
}

func Run(ctx context.Context, cfg *Config) error {
	var count int
	log.Println("draft: started")
	defer func() {
		log.Printf("draft: ended (%d)\n", count)
	}()

	debug := func(format string, args ...interface{}) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	b, err := os.ReadFile(cfg.Input)
	if err != nil {
		return fmt.Errorf("draft: couldn't read input file: %w", err)
	}

	ext := filepath.Ext(cfg.Input)
	var unmarshal func([]byte) ([]*draft, error)
	switch ext {
	case ".json":
		unmarshal = func(b []byte) ([]*draft, error) {
			var is []*draft
			if err := json.Unmarshal(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	case ".csv":
		unmarshal = func(b []byte) ([]*draft, error) {
			var is []*draft
			if err := gocsv.UnmarshalBytes(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	default:
		return fmt.Errorf("adobe: unsupported output format: %s", ext)
	}
	drafts, err := unmarshal(b)
	if err != nil {
		return fmt.Errorf("draft: couldn't unmarshal input: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("draft: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("draft: couldn't start orm store: %w", err)
	}

	for _, d := range drafts {
		if cfg.Limit > 0 && count >= cfg.Limit {
			break
		}
		js, _ := json.Marshal(d)
		debug("draft: %s", string(js))
		typ := d.Type
		if typ == "" {
			typ = cfg.Type
		}
		volumes := d.Volumes
		if volumes == 0 {
			volumes = cfg.Volumes
		}
		if typ == "" {
			return fmt.Errorf("draft: type not set %s", string(js))
		}
		if d.Title == "" {
			return fmt.Errorf("draft: title not set %s", string(js))
		}
		if err := store.SetDraft(ctx, &storage.Draft{
			ID:       ulid.Make().String(),
			Type:     typ,
			Title:    d.Title,
			Subtitle: d.Subtitle,
			Volumes:  volumes,
		}); err != nil {
			return fmt.Errorf("draft: couldn't set draft: %w", err)
		}
		count++
	}
	return nil
}
