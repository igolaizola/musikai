package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Draft struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	Type string `gorm:"not null;default:''"`

	Title    string `gorm:"not null;default:''"`
	Subtitle string `gorm:"not null;default:''"`
	Volumes  int    `gorm:"not null;default:0"`

	Cover    bool `gorm:"not null;default:false"`
	Disabled bool `gorm:"index"`
}

type DraftCovers struct {
	Draft  *Draft
	Covers int
}

func (s *Store) GetDraft(ctx context.Context, id string) (*Draft, error) {
	var v Draft
	if err := s.db.First(&v, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get draft %s: %w", id, err)
	}
	return &v, nil
}

func (s *Store) SetDraft(ctx context.Context, v *Draft) error {
	if err := s.db.Save(v).Error; err != nil {
		return fmt.Errorf("storage: failed to set draft %s: %w", v.ID, err)
	}
	return nil
}

func (s *Store) DeleteDraft(ctx context.Context, id string) error {
	if err := s.db.Delete(&Draft{ID: id}, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("storage: failed to delete draft %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListDrafts(ctx context.Context, page, size int, orderBy string, filter ...Filter) ([]*Draft, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Draft{}

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
		return nil, fmt.Errorf("storage: failed to list drafts: %w", err)
	}
	return vs, nil
}

func (s *Store) NextDraft(ctx context.Context, filter ...Filter) (*Draft, error) {
	var v Draft
	q := s.db.Where("disabled = ?", false)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	if err := q.First(&v, q).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get next draft: %w", err)
	}
	return &v, nil
}

func (s *Store) ListDraftCovers(ctx context.Context, min, page, size int, orderBy string, filter ...Filter) ([]*DraftCovers, error) {
	vs := []*DraftCovers{}

	// Adjust the join condition based on your actual foreign key and relationship
	q := s.db.Model(&Draft{}).Select("drafts.*, count(*) as count").
		Joins("inner join covers on drafts.title = covers.title").
		Where("drafts.disabled = ?", false).
		Where("covers.state IN ?", []State{Pending, Approved}).
		Having("count < (drafts.Volumes+1) * ?", min)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	// Order by
	if orderBy != "" {
		q = q.Order(orderBy)
	}
	// Paginate
	offset := (page - 1) * size
	q = q.Offset(offset).Limit(size)
	if err := q.Scan(&vs).Error; err != nil {
		return nil, fmt.Errorf("orm: couldn't list videos and images: %w", err)
	}
	return vs, nil
}
