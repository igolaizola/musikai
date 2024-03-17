package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

	Cover bool  `gorm:"not null;default:false"`
	State State `gorm:"index"`
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
	q = q.Where("state != ?", Rejected)
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
	q := s.db.Where("state != ?", Rejected)
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

type DraftCovers struct {
	Draft
	Covers int `gorm:"column:covers"`
}

func (s *Store) ListDraftCovers(ctx context.Context, min, page, size int, orderBy string, filter ...Filter) ([]*DraftCovers, error) {
	vs := []*DraftCovers{}

	// Getting DB column names
	stmt := &gorm.Statement{DB: s.db}
	if err := stmt.Parse(&Draft{}); err != nil {
		return nil, fmt.Errorf("storage: couldn't parse draft: %w", err)
	}
	columns := []string{}
	for _, dbField := range stmt.Schema.DBNames {
		columns = append(columns, fmt.Sprintf("drafts.%s", dbField))
	}

	// Query to get drafts with less covers than the minimum
	q := s.db.Model(&Draft{}).Select(strings.Join(append(columns, "count(*) as covers"), ",")).
		Joins("INNER JOIN covers on drafts.title = covers.title AND covers.state = ?", Approved).
		Where("drafts.state != ?", Rejected).
		Where("(select id from albums where albums.draft_id = drafts.id) < CASE WHEN drafts.volumes = 0 THEN 1 ELSE draft.volumes END").
		Group(strings.Join(columns, ","))
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
		return nil, fmt.Errorf("storage: couldn't list draft covers: %w", err)
	}
	return vs, nil
}

type DraftSongs struct {
	Draft
	Songs int `gorm:"column:count_songs"`
}

func (s *Store) NextDraftSongs(ctx context.Context, min int, orderBy string, filter ...Filter) (*DraftSongs, error) {
	var vs []*DraftSongs

	// Getting DB column names
	stmt := &gorm.Statement{DB: s.db}
	if err := stmt.Parse(&Draft{}); err != nil {
		return nil, fmt.Errorf("storage: couldn't parse draft: %w", err)
	}
	columns := []string{}
	for _, dbField := range stmt.Schema.DBNames {
		columns = append(columns, fmt.Sprintf("drafts.%s", dbField))
	}

	// Query to get drafts with less covers than the minimum
	q := s.db.Model(&Draft{}).Select(strings.Join(append(columns, "count(*) as count_songs"), ",")).
		Joins(`LEFT JOIN "songs" ON drafts.type = songs.type AND songs.state = ?`, Approved).
		Where("EXISTS (select id from covers WHERE drafts.title = covers.title AND covers.upscaled AND covers.state = ?)", Approved).
		Where("drafts.state = ?", Approved).
		Group(strings.Join(columns, ",")).
		Having("count(*) > ?", min)
	for _, f := range filter {
		q = q.Where(f.Query, f.Args...)
	}
	// Order by
	if orderBy != "" {
		q = q.Order(orderBy)
	}
	q = q.Limit(1)
	if err := q.Scan(&vs).Error; err != nil {
		return nil, fmt.Errorf("storage: couldn't list draft covers: %w", err)
	}
	if len(vs) == 0 {
		return nil, ErrNotFound
	}
	return vs[0], nil
}
