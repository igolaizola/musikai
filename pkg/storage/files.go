package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type File struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Ref       string
}

func (s *Store) GetFileRef(ctx context.Context, id string) (string, error) {
	var v File
	if err := s.db.First(&v, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("storage: failed to get file %s: %w", id, err)
	}
	return v.Ref, nil
}

func (s *Store) SetFileRef(ctx context.Context, id, ref string) error {
	v := &File{
		ID:        id,
		Ref:       ref,
		UpdatedAt: time.Now().UTC(),
	}
	err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},                             // Unique columns
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}), // Columns to update
	}).Create(v).Error
	if err != nil {
		return fmt.Errorf("storage: failed to set file %s: %w", id, err)
	}
	return nil
}

func (s *Store) DeleteFile(ctx context.Context, id string) error {
	if err := s.db.Delete(&File{ID: id}, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("storage: failed to delete file %s: %w", id, err)
	}
	return nil
}
