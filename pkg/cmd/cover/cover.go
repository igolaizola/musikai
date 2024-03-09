package cover

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

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
	Minimum     int

	Discord *imageai.Config
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

	if cfg.Template == "" {
		return errors.New("cover: template is required")
	}

	if cfg.Minimum < 1 {
		return errors.New("cover: minimum is required")
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

			// Get next songs
			filters := []storage.Filter{
				// TODO: add filters
				storage.Where("drafts.id > ?", currID),
			}
			if cfg.Type != "" {
				filters = append(filters, storage.Where("type LIKE ?", cfg.Type))
			}

			// Get next draft
			if len(drafts) == 0 {
				// Get a songs from the database.
				var err error
				draftCovers, err := store.ListDraftCovers(ctx, cfg.Minimum, 1, 100, "", filters...)
				if err != nil {
					return fmt.Errorf("process: couldn't get draft from database: %w", err)
				}
				if len(draftCovers) == 0 {
					return errors.New("process: no drafts to process")
				}
				for _, dc := range draftCovers {
					for i := dc.Covers; i < cfg.Minimum; i++ {
						drafts = append(drafts, &dc.Draft)
					}
				}
				currID = drafts[len(drafts)-1].ID
			}
			draft := drafts[0]
			drafts = drafts[1:]

			// Launch generate in a goroutine
			wg.Add(1)
			go func() {
				defer wg.Done()
				debug("cover: start (%s, %s)", draft.Type, draft.Title)
				err := generate(ctx, generator, store, draft, cfg.Template)
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

	urls, err := generator.Generate(ctx, prompt)
	var aiErr ai.Error
	if errors.As(err, &aiErr) {
		if aiErr.Fatal() {
			return fmt.Errorf("cover: fatal error: %w (%s, %s)", err, draft.ID, prompt)
		}
		if !aiErr.Temporary() {
			draft.Disabled = true
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

func addAR(prompt, ar string) string {
	// Regular expression to find the --ar flag and its value
	re := regexp.MustCompile(`(--ar\s+[\d:.]+)`)
	found := re.FindStringSubmatch(prompt)

	// If --ar is found, replace its value
	if len(found) > 0 {
		return re.ReplaceAllString(prompt, "--ar "+ar)
	}

	// If --ar is not found, append --ar and the new value to the end
	return prompt + " --ar " + ar
}
