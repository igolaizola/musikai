package album

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/igolaizola/musikai/pkg/filestorage/tgstore"
	"github.com/igolaizola/musikai/pkg/storage"
)

type CoverConfig struct {
	Debug   bool
	DBType  string
	DBConn  string
	Proxy   string
	TGChat  int64
	TGToken string
	ID      string
	Cover   string
}

func RunCover(ctx context.Context, cfg *CoverConfig) error {
	log.Printf("album: cover started\n")
	defer func() {
		log.Printf("album: cover ended\n")
	}()

	debug := func(format string, args ...any) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	if cfg.ID == "" {
		return fmt.Errorf("album: id is empty")
	}
	if cfg.Cover == "" {
		return fmt.Errorf("album: cover file is empty")
	}
	if _, err := os.Stat(cfg.Cover); err != nil {
		return fmt.Errorf("album: cover file doesn't exist: %w", err)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("album: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("album: couldn't start orm store: %w", err)
	}

	tgStore, err := tgstore.New(cfg.TGToken, cfg.TGChat, cfg.Proxy, cfg.Debug)
	if err != nil {
		return fmt.Errorf("download: couldn't create file storage: %w", err)
	}

	album, err := store.GetAlbum(ctx, cfg.ID)
	if err != nil {
		return fmt.Errorf("album: couldn't get album: %w", err)
	}

	coverMatches, err := store.ListAlbums(ctx, 1, 1000, "", storage.Where("cover_id = ?", album.CoverID))
	if err != nil {
		return fmt.Errorf("album: couldn't list covers: %w", err)
	}
	var cover *storage.Cover
	if len(coverMatches) == 1 {
		cover, err = store.GetCover(ctx, album.CoverID)
		if err != nil {
			return fmt.Errorf("album: couldn't get cover: %w", err)
		}
	}

	// Upload cover to telegram
	debug("album: cover upload start %s", cfg.Cover)
	uploadID, err := tgStore.Set(ctx, cfg.Cover)
	if err != nil {
		return fmt.Errorf("album: couldn't upload cover image: %w", err)
	}
	debug("album: cover upload end %s", cfg.Cover)

	debug("album: updating album")
	album.CoverID = uploadID
	if err := store.SetAlbum(ctx, album); err != nil {
		return fmt.Errorf("album: couldn't update album: %w", err)
	}

	debug("album: reenabling cover")
	if cover != nil {
		cover.State = storage.Approved
		if err := store.SetCover(ctx, cover); err != nil {
			return fmt.Errorf("album: couldn't update cover: %w", err)
		}
	}

	return nil
}
