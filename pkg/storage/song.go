package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Song struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Type string `gorm:"not null;default:''"`

	Prompt       string `gorm:"not null;default:''"`
	Style        string `gorm:"not null;default:''"`
	Instrumental bool   `gorm:"not null;default:false"`

	SunoID    string `gorm:"not null;default:''"`
	SunoAudio string `gorm:"not null;default:''"`
	SunoImage string `gorm:"not null;default:''"`
	SunoTitle string `gorm:"not null;default:''"`

	Duration float32 `gorm:"not null;default:0"`
	Wave     string  `gorm:"not null;default:''"`
	Tempo    float32 `gorm:"not null;default:0"`

	AlbumID string `gorm:"index,not null;default:''"`

	Disabled bool `gorm:"index"`
}

func (s *Store) GetSong(ctx context.Context, id string) (*Song, error) {
	var v Song
	if err := s.db.First(&v, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get Song %s: %w", id, err)
	}
	return &v, nil
}

func (s *Store) SetSong(ctx context.Context, v *Song) error {
	if err := s.db.Save(v).Error; err != nil {
		return fmt.Errorf("storage: failed to set Song %s: %w", v.ID, err)
	}
	return nil
}

func (s *Store) DeleteSong(ctx context.Context, id string) error {
	if err := s.db.Delete(&Song{ID: id}, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("storage: failed to delete Song %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListSongs(ctx context.Context, page, size int, orderBy string, filter ...Filter) ([]*Song, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Song{}

	q := s.db.Offset(offset).Limit(size)
	q = q.Where("disabled = ?", false)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	// Order by
	if orderBy != "" {
		q = q.Order(orderBy)
	}
	if err := q.Find(&vs).Error; err != nil {
		return nil, fmt.Errorf("storage: failed to list Songs: %w", err)
	}
	return vs, nil
}

func (s *Store) NextSong(ctx context.Context, filter ...Filter) (*Song, error) {
	var v Song
	q := s.db.Where("disabled = ?", false)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	if err := q.First(&v, q).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get next Song: %w", err)
	}
	return &v, nil
}
