package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/oklog/ulid/v2"
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

type seed struct {
	ID string `gorm:"primaryKey"`
}

func (s *seed) TableName() string {
	return "songs"
}

func (s *Store) Migrate(ctx context.Context) error {
	// Initialize the database with a seed table
	init := !s.db.Migrator().HasTable(&Song{})

	// Custom migrations
	if err := s.customMigrate(init); err != nil {
		return err
	}

	// Auto migrations
	if init {
		if err := s.db.Migrator().CreateTable(&seed{}); err != nil {
			return fmt.Errorf("storage: failed to create table songs: %w", err)
		}
	}
	if err := s.db.AutoMigrate(
		&Song{},
		&Generation{},
		&Title{},
		&Draft{},
		&Cover{},
		&Album{},
		&Setting{},
		&File{},
	); err != nil {
		return fmt.Errorf("storage: failed to migrate database: %w", err)
	}
	return nil
}

func (s *Store) customMigrate(init bool) error {
	lastVersion := 1

	if !s.db.Migrator().HasTable(&Migration{}) {
		if err := s.db.Migrator().CreateTable(&Migration{}); err != nil {
			return fmt.Errorf("storage: failed to create table migrations: %w", err)
		}
		var version int
		if init {
			version = lastVersion
		}
		if err := s.db.Save(&Migration{ID: ulid.Make().String(), Version: version}).Error; err != nil {
			return fmt.Errorf("storage: failed to save migration version: %w", err)
		}
		if init {
			return nil
		}
	}

	// Get the current migration version
	var migration Migration
	if err := s.db.First(&migration).Error; err != nil {
		return fmt.Errorf("storage: failed to get migration version: %w", err)
	}

	// Custom migrations
	for i := migration.Version + 1; i <= lastVersion; i++ {
		switch i {
		case 1:
			log.Println("storage: migration 1: rename suno columns")
			if err := s.db.Migrator().RenameColumn(&Generation{}, "suno_id", "external_id"); err != nil {
				return fmt.Errorf("storage: migration %d: %w", i, err)
			}
			if err := s.db.Migrator().RenameColumn(&Generation{}, "suno_audio", "audio"); err != nil {
				return fmt.Errorf("storage: migration %d: %w", i, err)
			}
			if err := s.db.Migrator().RenameColumn(&Generation{}, "suno_image", "image"); err != nil {
				return fmt.Errorf("storage: migration %d: %w", i, err)
			}
			if err := s.db.Migrator().RenameColumn(&Generation{}, "suno_title", "title"); err != nil {
				return fmt.Errorf("storage: migration %d: %w", i, err)
			}
			if err := s.db.Migrator().RenameColumn(&Generation{}, "suno_history", "history"); err != nil {
				return fmt.Errorf("storage: migration %d: %w", i, err)
			}
		case 2:
			// TODO: Add next migration here and update lastVersion
		}
		migration.Version = i
		if err := s.db.Save(&migration).Error; err != nil {
			return fmt.Errorf("storage: failed to save migration version: %w", err)
		}
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
