package filestore

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/igolaizola/musikai/pkg/filestore/local"
	"github.com/igolaizola/musikai/pkg/filestore/s3"
	"github.com/igolaizola/musikai/pkg/filestore/tgstore"
	"github.com/igolaizola/musikai/pkg/storage"
)

type fs interface {
	Upload(ctx context.Context, path, name string) error
	Download(ctx context.Context, path, name string) error
}

type Store struct {
	fs fs
}

func (s *Store) SetMP3(ctx context.Context, path, id string) error {
	return s.fs.Upload(ctx, path, MP3(id))
}

func (s *Store) SetJPG(ctx context.Context, path, id string) error {
	return s.fs.Upload(ctx, path, JPG(id))
}

func (s *Store) GetMP3(ctx context.Context, path, id string) error {
	return s.fs.Download(ctx, path, MP3(id))
}

func (s *Store) GetJPG(ctx context.Context, path, id string) error {
	return s.fs.Download(ctx, path, JPG(id))
}

func New(typ, conn, proxy string, debug bool, store *storage.Store) (*Store, error) {
	var fs fs
	switch typ {
	case "telegram":
		split := strings.Split(conn, "@")
		if len(split) != 2 {
			return nil, fmt.Errorf("filestore: invalid telegram connection string %q", conn)
		}
		token := split[0]
		chat, err := strconv.ParseInt(split[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("filestore: invalid telegram chat id %q: %w", split[1], err)
		}
		candidate, err := tgstore.New(token, chat, proxy, debug, store)
		if err != nil {
			return nil, fmt.Errorf("filestore: %w", err)
		}
		fs = candidate
	case "s3":
		split := strings.Split(conn, "@")
		if len(split) != 2 {
			return nil, fmt.Errorf("filestore: invalid s3 connection string %q", conn)
		}
		auth := strings.Split(split[0], ":")
		if len(auth) != 2 {
			return nil, fmt.Errorf("filestore: invalid s3 auth string %q", conn)
		}
		key := auth[0]
		secret := auth[1]
		loc := strings.Split(split[1], ".")
		if len(loc) != 2 {
			return nil, fmt.Errorf("filestore: invalid s3 location string %q", conn)
		}
		bucket := loc[0]
		region := loc[1]
		candidate, err := s3.New(key, secret, region, bucket, debug)
		if err != nil {
			return nil, fmt.Errorf("filestore: %w", err)
		}
		fs = candidate
	case "local":
		fs = local.New(conn, debug)
	default:
		return nil, fmt.Errorf("filestore: unknown file storage type %q", typ)
	}
	return &Store{fs: fs}, nil
}

func JPG(id string) string {
	return id + ".jpg"
}

func MP3(id string) string {
	return id + ".mp3"
}
