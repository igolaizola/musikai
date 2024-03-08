package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Cover struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Type     string `gorm:"not null;default:''"`
	Title    string `gorm:"not null;default:''"`
	Template string `gorm:"not null;default:''"`
	DsURL    string `gorm:"not null;default:''"`
	MjURL    string `gorm:"not null;default:''"`

	DraftID string `gorm:"not null;default:''"`

	State State `gorm:"not null;default:0"`
}

func (s *Store) GetCover(ctx context.Context, id string) (*Cover, error) {
	var v Cover
	if err := s.db.First(&v, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get cover %s: %w", id, err)
	}
	return &v, nil
}

func (s *Store) SetCover(ctx context.Context, v *Cover) error {
	if err := s.db.Save(v).Error; err != nil {
		return fmt.Errorf("storage: failed to set cover %s: %w", v.ID, err)
	}
	return nil
}

func (s *Store) DeleteCover(ctx context.Context, id string) error {
	if err := s.db.Delete(&Cover{ID: id}, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("storage: failed to delete cover %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListCovers(ctx context.Context, page, size int, orderBy string, filter ...Filter) ([]*Cover, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Cover{}

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
		return nil, fmt.Errorf("storage: failed to list covers: %w", err)
	}
	return vs, nil
}

func (s *Store) NextCover(ctx context.Context, filter ...Filter) (*Cover, error) {
	var v Cover
	q := s.db.Where("disabled = ?", false)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	if err := q.First(&v, q).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get next cover: %w", err)
	}
	return &v, nil
}
