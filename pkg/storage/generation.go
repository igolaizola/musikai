package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Generation struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	SongID *string
	Song   *Song `gorm:"foreignKey:SongID"`

	SunoID      string `gorm:"not null;default:''"`
	SunoAudio   string `gorm:"not null;default:''"`
	SunoImage   string `gorm:"not null;default:''"`
	SunoTitle   string `gorm:"not null;default:''"`
	SunoHistory string `gorm:"not null;default:''"`

	Duration float32 `gorm:"not null;default:0"`
	Wave     string  `gorm:"not null;default:''"`
	Tempo    float32 `gorm:"not null;default:0"`
	Flags    string  `gorm:"not null;default:''"`
	Master   string  `gorm:"not null;default:''"`

	ProcessedAt time.Time
	Processed   bool `gorm:"index"`
	Mastered    bool `gorm:"index"`

	Ends    bool
	Flagged bool `gorm:"index"`
}

func (s *Store) GetGeneration(ctx context.Context, id string) (*Generation, error) {
	var v Generation

	// Process song
	q := s.db.Preload("Song")

	if err := q.First(&v, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get Generation %s: %w", id, err)
	}
	return &v, nil
}

func (s *Store) SetGeneration(ctx context.Context, v *Generation) error {
	if err := s.db.Save(v).Error; err != nil {
		return fmt.Errorf("storage: failed to set Generation %s: %w", v.ID, err)
	}
	return nil
}

func (s *Store) DeleteGeneration(ctx context.Context, id string) error {
	if err := s.db.Delete(&Generation{ID: id}, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("storage: failed to delete Generation %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListGenerations(ctx context.Context, page, size int, orderBy string, filter ...Filter) ([]*Generation, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Generation{}

	// Process song
	q := s.db.Preload("Song")
	q = q.Joins("INNER JOIN songs ON songs.id = generations.song_id")

	q = q.Offset(offset).Limit(size)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	// Order by
	if orderBy != "" {
		q = q.Order(orderBy)
	}
	if err := q.Find(&vs).Error; err != nil {
		return nil, fmt.Errorf("storage: failed to list Generations: %w", err)
	}
	return vs, nil
}

func (s *Store) NextGeneration(ctx context.Context, filter ...Filter) (*Generation, error) {
	var v Generation

	// Process song
	q := s.db.Preload("Song")

	q = q.Where("state != ?", Rejected)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	if err := q.First(&v, q).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get next Generation: %w", err)
	}
	return &v, nil
}
