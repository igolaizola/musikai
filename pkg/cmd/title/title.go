package title

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
	Debug  bool
	DBType string
	DBConn string
	Limit  int
	Type   string
	Input  string
}

type title struct {
	Type  string `json:"type" csv:"type"`
	Style string `json:"style" csv:"style"`
	Title string `json:"title" csv:"title"`
}

func Run(ctx context.Context, cfg *Config) error {
	var count int
	log.Println("title: started")
	defer func() {
		log.Printf("title: ended (%d)\n", count)
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
	var unmarshal func([]byte) ([]*title, error)
	switch ext {
	case ".json":
		unmarshal = func(b []byte) ([]*title, error) {
			var is []*title
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
				return fmt.Errorf("title: inconsistent number of fields in csv %d (%s)", i, l)
			}
		}
		unmarshal = func(b []byte) ([]*title, error) {
			var is []*title
			if err := gocsv.UnmarshalBytes(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	default:
		return fmt.Errorf("adobe: unsupported output format: %s", ext)
	}
	titles, err := unmarshal(b)
	if err != nil {
		return fmt.Errorf("draft: couldn't unmarshal input: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("process: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("process: couldn't start orm store: %w", err)
	}

	for _, t := range titles {
		if cfg.Limit > 0 && count >= cfg.Limit {
			break
		}
		js, _ := json.Marshal(t)
		debug("title: %s", string(js))
		typ := t.Type
		if typ == "" {
			typ = cfg.Type
		}
		if typ == "" {
			log.Printf("draft: type not set %s\n", string(js))
			continue
		}
		if t.Title == "" {
			log.Printf("draft: title not set %s", string(js))
			continue
		}

		// Check for duplicates
		unique := strings.ToLower(strings.ReplaceAll(t.Title, " ", ""))
		coincidences, err := store.ListTitles(ctx, 1, 1, "", storage.Where("LOWER(REPLACE(title, ' ', '')) = ?", unique))
		switch {
		case errors.Is(err, storage.ErrNotFound):
		case err != nil:
			return fmt.Errorf("title: couldn't list titles: %w", err)
		case len(coincidences) > 0:
			log.Printf("title: already exists (%s) %s\n", coincidences[0].ID, string(js))
			continue
		}

		if err := store.SetTitle(ctx, &storage.Title{
			ID:    ulid.Make().String(),
			Type:  typ,
			Style: t.Style,
			Title: t.Title,
			State: storage.Approved,
		}); err != nil {
			return fmt.Errorf("process: couldn't set title: %w", err)
		}
		count++
	}
	return nil
}
