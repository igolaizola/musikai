package storage

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
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
	Likes int   `gorm:"not null;default:0"`

	UpscaleAt time.Time
	UpscaleID string `gorm:"not null;default:''"`
	Upscaled  bool   `gorm:"not null;default:false"`
}

func (c *Cover) URL() string {
	if isExpired(c.DsURL) {
		return c.MjURL
	}
	return c.DsURL
}

func isExpired(u string) bool {
	if u == "" {
		return true
	}

	// Parse the URL
	parsedURL, err := url.Parse(u)
	if err != nil {
		panic(fmt.Errorf("error parsing URL: %w", err))
	}

	// Extract the `ex` query parameter
	values, err := url.ParseQuery(parsedURL.RawQuery)
	if err != nil {
		panic(fmt.Errorf("error parsing query parameters: %w", err))
	}
	exHex := values.Get("ex")
	if exHex == "" {
		// If `ex` is not present, the URL is not expired
		return false
	}

	// Convert `ex` from hex to int
	exInt, err := strconv.ParseInt(exHex, 16, 64)
	if err != nil {
		panic(fmt.Errorf("`ex` value conversion error: %w", err))
	}

	// Convert int to Unix time
	exTime := time.Unix(exInt, 0)

	// Check if the time is expired
	isExpired := exTime.Before(time.Now())

	return isExpired
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
	filter = append(filter, Where("state != ?", Rejected))
	return s.ListAllCovers(ctx, page, size, orderBy, filter...)
}

func (s *Store) ListAllCovers(ctx context.Context, page, size int, orderBy string, filter ...Filter) ([]*Cover, error) {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * size
	vs := []*Cover{}

	q := s.db.Offset(offset).Limit(size)
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
