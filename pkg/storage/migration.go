package storage

type Migration struct {
	ID        string `gorm:"primarykey"`
	CreatedAt int64
	UpdatedAt int64

	Version int `gorm:"not null;default:0"`
}
