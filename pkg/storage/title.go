package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Title struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Type  string `gorm:"not null;default:''"`
	Title string `gorm:"not null;default:''"`
	State State  `gorm:"index"`
}

func (s *Store) GetTitle(ctx context.Context, id string) (*Title, error) {
	var v Title
	if err := s.db.First(&v, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get title %s: %w", id, err)
	}
	return &v, nil
}

func (s *Store) SetTitle(ctx context.Context, v *Title) error {
	if err := s.db.Save(v).Error; err != nil {
		return fmt.Errorf("storage: failed to set title %s: %w", v.ID, err)
	}
	return nil
}

func (s *Store) DeleteTitle(ctx context.Context, id string) error {
	if err := s.db.Delete(&Title{ID: id}, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("storage: failed to delete title %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListTitles(ctx context.Context, page, size int, orderBy string, filter ...Filter) ([]*Title, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Title{}

	q := s.db.Offset(offset).Limit(size)
	q = q.Where("state != ?", Rejected)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	// Order by
	if orderBy != "" {
		q = q.Order(orderBy)
	}
	if err := q.Find(&vs).Error; err != nil {
		return nil, fmt.Errorf("storage: failed to list titles: %w", err)
	}
	return vs, nil
}

func (s *Store) NextTitle(ctx context.Context, filter ...Filter) (*Title, error) {
	var v Title
	q := s.db.Where("state != ?", Rejected)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	if err := q.First(&v, q).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get next title: %w", err)
	}
	return &v, nil
}
