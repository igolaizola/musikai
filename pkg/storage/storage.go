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

	m, err := s.currentMigration(init)
	if err != nil {
		return err
	}

	// Custom pre migrations
	if err := s.preMigrate(m.Version); err != nil {
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

	// Custom post migrations
	if err := s.postMigrate(m.Version); err != nil {
		return err
	}

	// Update the migration version
	m.Version = lastVersion
	if err := s.db.Save(m).Error; err != nil {
		return fmt.Errorf("storage: failed to save migration version: %w", err)
	}
	return nil
}

const lastVersion = 2

func (s *Store) currentMigration(init bool) (*Migration, error) {
	var migration Migration
	if !s.db.Migrator().HasTable(&Migration{}) {
		if err := s.db.Migrator().CreateTable(&Migration{}); err != nil {
			return nil, fmt.Errorf("storage: failed to create table migrations: %w", err)
		}
		migration.ID = ulid.Make().String()
		if init {
			migration.Version = lastVersion
		}
		if err := s.db.Save(&migration).Error; err != nil {
			return nil, fmt.Errorf("storage: failed to save migration version: %w", err)
		}
		if init {
			return &migration, nil
		}
	}

	// Get the current migration version
	if err := s.db.First(&migration).Error; err != nil {
		return nil, fmt.Errorf("storage: failed to get migration version: %w", err)
	}
	return &migration, nil
}

func (s *Store) preMigrate(version int) error {
	// Custom migrations
	for i := version + 1; i <= lastVersion; i++ {
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
		case 3:
			// TODO: Next migration here and update lastVersion
		}
	}
	return nil
}

func (s *Store) postMigrate(version int) error {
	// Custom migrations
	for i := version + 1; i <= lastVersion; i++ {
		switch i {
		case 2:
			// Update empty provider to "suno"
			log.Println("storage: migration 2: update empty provider to suno")
			if err := s.db.Exec("UPDATE songs SET provider = 'suno' WHERE provider = ''").Error; err != nil {
				return fmt.Errorf("storage: migration %d: %w", i, err)
			}
			// Update manual to true where prompt is empty
			log.Println("storage: migration 2: update manual to true where prompt is empty")
			if err := s.db.Exec("UPDATE songs SET manual = true WHERE prompt = ''").Error; err != nil {
				return fmt.Errorf("storage: migration %d: %w", i, err)
			}
			// Set prompt value equal to style value where prompt is empty
			log.Println("storage: migration 2: set prompt value equal to style value where prompt is empty")
			if err := s.db.Exec("UPDATE songs SET prompt = style WHERE prompt = ''").Error; err != nil {
				return fmt.Errorf("storage: migration %d: %w", i, err)
			}
		case 3:
			// TODO: Next migration here and update lastVersion
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
