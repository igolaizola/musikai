package setting

import (
	"context"
	"fmt"

	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug  bool
	DBType string
	DBConn string

	Service string
	Account string
	Value   string
	Type    string
}

func Run(ctx context.Context, cfg *Config) error {
	store, err := storage.New(cfg.DBType, cfg.DBConn, cfg.Debug)
	if err != nil {
		return fmt.Errorf("setting: couldn't create orm store: %w", err)
	}
	if err := store.Start(ctx); err != nil {
		return fmt.Errorf("setting: couldn't start orm store: %w", err)
	}

	if cfg.Account == "" {
		return fmt.Errorf("setting: account is empty")
	}
	if cfg.Value == "" {
		return fmt.Errorf("setting: value is empty")
	}

	switch cfg.Type {
	case "cookie":
	default:
		return fmt.Errorf("setting: unknown type: %s", cfg.Type)
	}

	switch cfg.Service {
	case "distrokid", "suno", "discord", "udio", "jamendo":
	default:
		return fmt.Errorf("setting: unknown service: %s", cfg.Service)
	}

	id := fmt.Sprintf("%s/%s/%s", cfg.Service, cfg.Account, cfg.Type)
	s := storage.Setting{
		ID:    id,
		Value: cfg.Value,
	}
	if err := store.SetSetting(ctx, &s); err != nil {
		return fmt.Errorf("setting: couldn't save cookie: %w", err)
	}
	return nil
}
