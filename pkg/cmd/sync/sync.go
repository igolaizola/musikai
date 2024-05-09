package sync

import (
	"context"
	"log"
	"time"
)

type Config struct {
	Debug  bool
	DBType string
	DBConn string
	Proxy  string

	Timeout     time.Duration
	Concurrency int
	WaitMin     time.Duration
	WaitMax     time.Duration
	Limit       int
	Account     string

	SpotifyID     string
	SpotifySecret string

	YoutubeKey string
	Channels   string
	From       string
}

func Run(ctx context.Context, cfg *Config) error {
	if err := RunDistrokid(ctx, cfg); err != nil {
		log.Println(err)
	}
	if err := RunSpotify(ctx, cfg); err != nil {
		log.Println(err)
	}
	if err := RunYoutube(ctx, cfg); err != nil {
		log.Println(err)
	}
	return nil
}
