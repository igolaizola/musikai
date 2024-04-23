package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Album struct {
	ID        string `gorm:"primarykey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	DraftID string `gorm:"not null;default:''"`
	CoverID string `gorm:"not null;default:''"`

	Type           string `gorm:"not null;default:''"`
	Title          string `gorm:"not null;default:''"`
	Subtitle       string `gorm:"not null;default:''"`
	Volume         int    `gorm:"not null;default:0"`
	Artist         string `gorm:"not null;default:''"`
	PrimaryGenre   string `gorm:"not null;default:''"`
	SecondaryGenre string `gorm:"not null;default:''"`

	DistrokidID string `gorm:"not null;default:''"`
	UPC         string `gorm:"not null;default:''"`
	SpotifyID   string `gorm:"not null;default:''"`
	AppleID     string `gorm:"not null;default:''"`
	JamendoID   string `gorm:"not null;default:''"`
	JamendoAt   time.Time
	PublishedAt time.Time

	State State `gorm:"index"`
}

func (s *Store) GetAlbum(ctx context.Context, id string) (*Album, error) {
	var v Album
	if err := s.db.First(&v, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get album %s: %w", id, err)
	}
	return &v, nil
}

func (s *Store) SetAlbum(ctx context.Context, v *Album) error {
	if err := s.db.Save(v).Error; err != nil {
		return fmt.Errorf("storage: failed to set album %s: %w", v.ID, err)
	}
	return nil
}

func (s *Store) DeleteAlbum(ctx context.Context, id string) error {
	if err := s.db.Delete(&Album{ID: id}, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("storage: failed to delete album %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListAlbums(ctx context.Context, page, size int, orderBy string, filter ...Filter) ([]*Album, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Album{}

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
		return nil, fmt.Errorf("storage: failed to list albums: %w", err)
	}
	return vs, nil
}

func (s *Store) NextAlbum(ctx context.Context, filter ...Filter) (*Album, error) {
	var v Album
	q := s.db.Where("state != ?", Rejected)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	if err := q.First(&v, q).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: failed to get next album: %w", err)
	}
	return &v, nil
}
