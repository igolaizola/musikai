package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// State custom type for our enum
type State int

// Enum values for State
const (
	Pending  State = 0
	Rejected State = 1
	Approved State = 2
	Used     State = 3
)

type Song struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Type  string `gorm:"not null;default:''"`
	Notes string `gorm:"not null;default:''"`

	Prompt       string `gorm:"not null;default:''"`
	Style        string `gorm:"not null;default:''"`
	Instrumental bool   `gorm:"not null;default:false"`

	GenerationID *string
	Generation   *Generation `gorm:"foreignKey:GenerationID"`

	Title   string `gorm:"not null;default:''"`
	AlbumID string `gorm:"index,not null;default:''"`
	Order   int    `gorm:"not null;default:0"`

	Likes int   `gorm:"not null;default:0"`
	State State `gorm:"not null;default:0"`
}

func (s *Store) GetSong(ctx context.Context, id string) (*Song, error) {
	// Process song
	q := s.db.Preload("Generation")

	var v Song
	if err := q.First(&v, "id = ?", id).Error; err != nil {
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
	filter = append(filter, Where("state != ?", Rejected))
	return s.ListAllSongs(ctx, page, size, orderBy, filter...)
}

func (s *Store) ListAllSongs(ctx context.Context, page, size int, orderBy string, filter ...Filter) ([]*Song, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Song{}

	// Process song
	q := s.db.Preload("Generation")
	q = q.Joins("INNER JOIN generations ON songs.generation_id = generations.id")

	q = q.Offset(offset).Limit(size)
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

	// Process song
	q := s.db.Preload("Generation")

	q = q.Where("state != ?", Rejected)
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
