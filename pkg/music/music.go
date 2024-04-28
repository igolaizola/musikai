package music

import "context"

type Song struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Style        string  `json:"style"`
	Audio        string  `json:"audio"`
	Image        string  `json:"image"`
	Video        string  `json:"video"`
	Duration     float32 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	History      string  `json:"history"`
}

type Generator interface {
	Generate(ctx context.Context, prompt string, manual, instrumental bool) ([][]Song, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
