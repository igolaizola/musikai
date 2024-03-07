package title

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

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
		return fmt.Errorf("process: couldn't read input file: %w", err)
	}
	lines := strings.Split(string(b), "\n")

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("process: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("process: couldn't start orm store: %w", err)
	}

	for _, line := range lines {
		if cfg.Limit > 0 && count >= cfg.Limit {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		debug("title: %s", line)
		if err := store.SetTitle(ctx, &storage.Title{
			ID:    ulid.Make().String(),
			Type:  cfg.Type,
			Title: line,
		}); err != nil {
			return fmt.Errorf("process: couldn't set title: %w", err)
		}
		count++
	}
	return nil
}
