package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Setting struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Value     string
}

func (s *Store) GetSetting(ctx context.Context, id string) (*Setting, error) {
	var v Setting
	if err := s.db.First(&v, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get setting %s: %w", id, err)
	}
	return &v, nil
}

func (s *Store) SetSetting(ctx context.Context, v *Setting) error {
	if err := s.db.Save(v).Error; err != nil {
		return fmt.Errorf("storage: failed to set setting %s: %w", v.ID, err)
	}
	return nil
}

func (s *Store) DeleteSetting(ctx context.Context, id string) error {
	if err := s.db.Delete(&Setting{ID: id}, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("storage: failed to delete setting %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListSettings(ctx context.Context, page, size int) ([]*Setting, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Setting{}

	q := s.db.Offset(offset).Limit(size)
	if err := q.Find(&vs).Error; err != nil {
		return nil, fmt.Errorf("storage: failed to list settings: %w", err)
	}
	return vs, nil
}
