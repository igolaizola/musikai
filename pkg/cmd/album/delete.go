package album

import (
	"context"
	"fmt"
	"log"

	"github.com/igolaizola/musikai/pkg/storage"
)

type DeleteConfig struct {
	Debug  bool
	DBType string
	DBConn string
	ID     string
}

func RunDelete(ctx context.Context, cfg *DeleteConfig) error {
	log.Printf("album: delete started\n")
	defer func() {
		log.Printf("album: delete ended\n")
	}()

	debug := func(format string, args ...any) {
		if !cfg.Debug {
			return
		}
		format += "\n"
		log.Printf(format, args...)
	}

	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("album: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("album: couldn't start orm store: %w", err)
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

	songs, err := store.ListSongs(ctx, 1, 1000, "", storage.Where("album_id = ?", cfg.ID))
	if err != nil {
		return fmt.Errorf("album: couldn't list songs: %w", err)
	}
	ts := []string{}
	for _, song := range songs {
		ts = append(ts, song.Title)
	}

	titles, err := store.ListTitles(ctx, 1, len(ts), "", storage.Where("title IN ?", ts))
	if err != nil {
		return fmt.Errorf("album: couldn't list titles: %w", err)
	}

	draft, err := store.GetDraft(ctx, album.DraftID)
	if err != nil {
		return fmt.Errorf("album: couldn't get draft: %w", err)
	}

	debug("album: reenabling songs")
	for _, song := range songs {
		song.AlbumID = ""
		song.Title = ""
		song.Order = 0
		song.State = storage.Approved
		if err := store.SetSong(ctx, song); err != nil {
			return fmt.Errorf("album: couldn't update song: %w", err)
		}
	}

	debug("album: reenabling titles")
	for _, title := range titles {
		title.State = storage.Approved
		if err := store.SetTitle(ctx, title); err != nil {
			return fmt.Errorf("album: couldn't update title: %w", err)
		}
	}

	debug("album: reenabling draft")
	draft.State = storage.Approved
	if err := store.SetDraft(ctx, draft); err != nil {
		return fmt.Errorf("album: couldn't update draft: %w", err)
	}

	debug("album: reenabling cover")
	if cover != nil {
		cover.State = storage.Approved
		if err := store.SetCover(ctx, cover); err != nil {
			return fmt.Errorf("album: couldn't update cover: %w", err)
		}
	}

	debug("album: deleting album")
	if err := store.DeleteAlbum(ctx, cfg.ID); err != nil {
		return fmt.Errorf("album: couldn't delete album: %w", err)
	}
	return nil
}
