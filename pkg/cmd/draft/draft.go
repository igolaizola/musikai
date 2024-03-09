package draft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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
	Subtitle string `json:"subtitle" csv:"subtitle"`
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
		// Check for inconsistent number of fields in csv
		lines := strings.Split(string(b), "\n")
		commas := strings.Count(lines[0], ",")
		for i, l := range lines {
			if l == "" {
				continue
			}
			if commas != strings.Count(l, ",") {
				return fmt.Errorf("draft: inconsistent number of fields in csv %d (%s)", i, l)
			}
		}
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
			log.Printf("draft: type not set %s\n", string(js))
			continue
		}
		if d.Title == "" {
			log.Printf("draft: title not set %s", string(js))
			continue
		}

		// Check for duplicates
		uniqueTitle := strings.ToLower(strings.ReplaceAll(d.Title, " ", ""))
		uniqueSubtitle := strings.ToLower(strings.ReplaceAll(d.Subtitle, " ", ""))
		coincidences, err := store.ListDrafts(ctx, 1, 1, "",
			storage.Where("LOWER(REPLACE(title, ' ', '')) = ?", uniqueTitle),
			storage.Where("LOWER(REPLACE(subtitle, ' ', '')) = ?", uniqueSubtitle),
		)
		switch {
		case errors.Is(err, storage.ErrNotFound):
		case err != nil:
			return fmt.Errorf("draft: couldn't list drafts: %w", err)
		case len(coincidences) > 0:
			log.Printf("draft: already exists (%s) %s\n", coincidences[0].ID, string(js))
			continue
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
