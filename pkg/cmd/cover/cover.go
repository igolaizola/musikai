package cover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gocarina/gocsv"
	"github.com/igolaizola/bulkai/pkg/ai"
	"github.com/igolaizola/musikai/pkg/imageai"
	"github.com/igolaizola/musikai/pkg/storage"
	"github.com/oklog/ulid/v2"
)

type Config struct {
	Debug       bool
	DBType      string
	DBConn      string
	Timeout     time.Duration
	Concurrency int
	WaitMin     time.Duration
	WaitMax     time.Duration
	Limit       int
	Type        string
	Template    string
	Input       string
	Minimum     int

	Discord *imageai.Config
}

type input struct {
	Type     string `json:"type" csv:"type"`
	Template string `json:"template" csv:"template"`
}

// Run launches the image generation process.
func Run(ctx context.Context, cfg *Config) error {
	var iteration int
	log.Println("cover: process started")
	defer func() {
		log.Printf("cover: process ended (%d)\n", iteration)
	}()

	debug := func(format string, args ...interface{}) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	if cfg.Template == "" && cfg.Input == "" {
		return errors.New("cover: template or input is required")
	}

	if cfg.Minimum < 1 {
		return errors.New("cover: minimum is required")
	}

	defaultTemplate := cfg.Template
	lookup := map[string]string{}
	if cfg.Input != "" {
		candidate, err := toTemplateLookup(cfg.Input)
		if err != nil {
			return fmt.Errorf("cover: couldn't get template lookup: %w", err)
		}
		lookup = candidate
	}

	var err error
	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("cover: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("cover: couldn't start orm store: %w", err)
	}

	generator, err := imageai.New(cfg.Discord, store)
	if err != nil {
		return fmt.Errorf("cover: couldn't create discord generator: %w", err)
	}
	if err := generator.Start(ctx); err != nil {
		return fmt.Errorf("cover: couldn't start discord generator: %w", err)
	}
	defer func() {
		if err := generator.Stop(); err != nil {
			log.Printf("cover: couldn't stop discord generator: %v\n", err)
		}
	}()

	nErr := 0
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 24 * time.Hour
	}
	ticker := time.NewTicker(timeout)
	last := time.Now()
	defer ticker.Stop()

	// Concurrency settings
	concurrency := cfg.Concurrency
	if concurrency == 0 {
		concurrency = 1
	}
	errC := make(chan error, concurrency)
	defer close(errC)
	for i := 0; i < concurrency; i++ {
		errC <- nil
	}
	var wg sync.WaitGroup
	defer wg.Wait()

	var drafts []*storage.Draft
	var currID string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("cover: %w", ctx.Err())
		case <-ticker.C:
			return nil
		case err := <-errC:
			if err != nil {
				nErr += 1
			} else {
				nErr = 0
			}

			// Check exit conditions
			if nErr > 10 {
				return fmt.Errorf("cover: too many consecutive errors: %w", err)
			}
			if cfg.Limit > 0 && iteration >= cfg.Limit {
				return nil
			}

			iteration++
			if time.Since(last) > 60*time.Minute {
				last = time.Now()
				log.Printf("cover: iteration %d\n", iteration)
			}

			// Wait for a random time.
			wait := 1 * time.Second
			if iteration > 1 {
				wait = time.Duration(rand.Int63n(int64(cfg.WaitMax-cfg.WaitMin))) + cfg.WaitMin
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("cover: %w", ctx.Err())
			case <-time.After(wait):
			}

			// Get next drafts
			filters := []storage.Filter{
				storage.Where("drafts.id > ?", currID),
				storage.Where("drafts.state = ?", storage.Approved),
				storage.Where(`(
	(
		drafts.volumes > 0 AND 
		(select count(*) from covers where covers.state IN (?,?) and covers.draft_id = drafts.id) < drafts.volumes 
	)
OR 
	(
		drafts.volumes = 0 AND
		(select count(*) from covers where covers.state = ? and covers.draft_id = drafts.id) < 1
	)
)`, storage.Approved, storage.Used, storage.Approved),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("drafts.type LIKE ?", cfg.Type))
			}

			// Get next draft
			if len(drafts) == 0 {
				// Get a drafts from the database.
				var err error
				draftCovers, err := store.ListDraftCovers(ctx, cfg.Minimum, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("process: couldn't get draft from database: %w", err)
				}
				if len(draftCovers) == 0 {
					return errors.New("process: no drafts to process")
				}
				for _, dc := range draftCovers {
					for i := dc.Covers; i < cfg.Minimum; i += 4 {
						drafts = append(drafts, &dc.Draft)
					}
				}
				currID = drafts[len(drafts)-1].ID
			}
			draft := drafts[0]
			drafts = drafts[1:]

			template, ok := lookup[draft.Type]
			switch {
			case !ok && defaultTemplate != "":
				template = defaultTemplate
			case !ok:
				return fmt.Errorf("cover: couldn't find template for (%s, %s)", draft.Type, draft.Title)
			}

			// Launch generate in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("cover: start (%s, %s)", draft.Type, draft.Title)

				err := generate(ctx, generator, store, draft, template)
				if err != nil {
					log.Println(err)
				}
				errC <- err
				debug("cover: end (%s, %s)", draft.Type, draft.Title)
			}()
		}
	}
}

func generate(ctx context.Context, generator *imageai.Generator, store *storage.Store, draft *storage.Draft, template string) error {
	// Generate the images.
	prompt := strings.ReplaceAll(template, "{title}", draft.Title)
	prompt = strings.ReplaceAll(prompt, "{TITLE}", strings.ToUpper(draft.Title))

	urls, err := generator.Generate(ctx, prompt)
	var aiErr ai.Error
	if errors.As(err, &aiErr) {
		if aiErr.Fatal() {
			return fmt.Errorf("cover: fatal error: %w (%s, %s)", err, draft.ID, prompt)
		}
		if !aiErr.Temporary() {
			draft.State = storage.Rejected
			if err := store.SetDraft(ctx, draft); err != nil {
				return fmt.Errorf("describe: couldn't update draft: %w", err)
			}
			log.Printf("cover: draft disabled %s\n", draft.ID)
		}
	}
	if err != nil {
		return fmt.Errorf("cover: couldn't generate images for (%s, %s): %w", draft.ID, prompt, err)
	}

	// Save the generated images to the database.
	for _, u := range urls {
		if err := store.SetCover(ctx, &storage.Cover{
			ID:       ulid.Make().String(),
			Type:     draft.Type,
			Title:    draft.Title,
			Template: template,
			DsURL:    u[0],
			MjURL:    u[1],
			DraftID:  draft.ID,
			State:    storage.Pending,
		}); err != nil {
			return fmt.Errorf("cover: couldn't save image to database: %w", err)
		}
	}
	return nil
}

func toTemplateLookup(file string) (map[string]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("generate: couldn't read input file: %w", err)
	}

	ext := filepath.Ext(file)
	var unmarshal func([]byte) ([]*input, error)
	switch ext {
	case ".json":
		unmarshal = func(b []byte) ([]*input, error) {
			var is []*input
			if err := json.Unmarshal(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	case ".csv":
		unmarshal = func(b []byte) ([]*input, error) {
			var is []*input
			if err := gocsv.UnmarshalBytes(b, &is); err != nil {
				return nil, fmt.Errorf("couldn't unmarshal items: %w", err)
			}
			return is, nil
		}
	default:
		return nil, fmt.Errorf("generate: unsupported output format: %s", ext)
	}
	inputs, err := unmarshal(b)
	if err != nil {
		return nil, fmt.Errorf("generate: couldn't unmarshal input: %w", err)
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("generate: no inputs found in file")
	}
	lookup := map[string]string{}
	for _, i := range inputs {
		lookup[i.Type] = i.Template
	}
	return lookup, nil
}
