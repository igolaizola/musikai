package migrate

import (
	"context"
	"fmt"

	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	DBType string
	DBConn string
}

// Run launches the migration process.
func Run(ctx context.Context, cfg *Config) error {
	store, err := storage.New(cfg.DBType, cfg.DBConn, true)
	if err != nil {
		return fmt.Errorf("migrate: couldn't create: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("migrate: couldn't start: %w", err)
	}
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: couldn't migrate: %w", err)
	}
	return nil
}
