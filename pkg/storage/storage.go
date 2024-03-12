package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	open   gorm.Dialector
	db     *gorm.DB
	logger logger.Interface
}

func New(dbType, dbConn string, debug bool) (*Store, error) {
	var open gorm.Dialector
	switch dbType {
	case "postgres":
		open = postgres.Open(dbConn)
	case "mysql":
		open = mysql.Open(dbConn)
	case "sqlite":
		open = sqlite.Open(dbConn)
	default:
		return nil, fmt.Errorf("storage: unknown db type: %s", dbType)
	}
	l := logger.Default.LogMode(logger.Silent)
	if debug {
		l = logger.Default.LogMode(logger.Warn)
	}
	return &Store{
		open:   open,
		logger: l,
	}, nil
}

func (s *Store) Start(ctx context.Context) error {
	// Launch the database connection in a goroutine so we can timeout if it
	// takes too long.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	errC := make(chan error, 1)
	defer close(errC)
	go func() {
		db, err := gorm.Open(s.open, &gorm.Config{
			Logger: s.logger,
		})
		if err != nil {
			errC <- fmt.Errorf("storage: failed to open database: %w", err)
		}
		s.db = db
		errC <- nil
	}()
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("storage: timed out opening database: %w", ctx.Err())
		}
		return ctx.Err()
	case err := <-errC:
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Migrate(ctx context.Context) error {
	if err := s.db.AutoMigrate(
		&Song{},
		&Title{},
		&Draft{},
		&Cover{},
		&Album{},
		&Setting{},
	); err != nil {
		return fmt.Errorf("storage: failed to migrate database: %w", err)
	}
	return nil
}

type Filter struct {
	Query interface{}
	Args  []interface{}
}

func Where(query interface{}, args ...interface{}) Filter {
	return Filter{
		Query: query,
		Args:  args,
	}
}
