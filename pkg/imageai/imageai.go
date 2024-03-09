package imageai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	fhttp "github.com/Danny-Dasilva/fhttp"
	"github.com/igolaizola/bulkai/pkg/ai"
	"github.com/igolaizola/bulkai/pkg/ai/bluewillow"
	"github.com/igolaizola/bulkai/pkg/ai/midjourney"
	"github.com/igolaizola/bulkai/pkg/discord"
	"github.com/igolaizola/bulkai/pkg/http"
	"github.com/igolaizola/musikai/pkg/storage"
)

type Config struct {
	Debug          bool    `yaml:"debug"`
	Bot            string  `yaml:"bot"`
	Proxy          string  `yaml:"proxy"`
	Channel        string  `yaml:"channel"`
	ReplicateToken string  `yaml:"replicate-token"`
	SessionFile    string  `yaml:"session"`
	Session        Session `yaml:"-"`
}

type Session struct {
	JA3             string `yaml:"ja3"`
	UserAgent       string `yaml:"user-agent"`
	Language        string `yaml:"language"`
	Token           string `yaml:"token"`
	SuperProperties string `yaml:"super-properties"`
	Locale          string `yaml:"locale"`
	Cookie          string `yaml:"cookie"`
}

type Generator struct {
	cfg           *Config
	client        ai.Client
	store         *storage.Store
	account       string
	discordClient *discord.Client
	httpClient    *fhttp.Client
	stop          func() error
}

func New(cfg *Config, store *storage.Store) (*Generator, error) {
	return &Generator{
		cfg:   cfg,
		store: store,
	}, nil
}

func (g *Generator) setup(ctx context.Context) error {
	cfg := g.cfg
	if cfg.Bot == "" {
		return errors.New("generator: missing bot name")
	}
	if cfg.Session.Token == "" {
		return errors.New("generator: missing token")
	}
	if cfg.Session.JA3 == "" {
		return errors.New("generator: missing ja3")
	}
	if cfg.Session.UserAgent == "" {
		return errors.New("generator: missing user agent")
	}
	if cfg.Session.Language == "" {
		return errors.New("generator: missing language")
	}
	if cfg.Bot != "bluewillow" && cfg.Bot != "midjourney" {
		return fmt.Errorf("generator: unknown bot: %s", cfg.Bot)
	}

	// Create http client
	httpClient, err := http.NewClient(cfg.Session.JA3, cfg.Session.UserAgent, cfg.Session.Language, cfg.Proxy)
	if err != nil {
		return fmt.Errorf("generator: couldn't create http client: %w", err)
	}

	// Set proxy
	if cfg.Proxy != "" {
		p := strings.TrimPrefix(cfg.Proxy, "http://")
		p = strings.TrimPrefix(p, "https://")
		os.Setenv("HTTPS_PROXY", p)
		os.Setenv("HTTP_PROXY", p)
	}

	// Set account
	b, err := base64.RawStdEncoding.DecodeString(strings.SplitN(cfg.Session.Token, ".", 2)[0])
	if err != nil {
		return fmt.Errorf("discordai: couldn't decode token: %w", err)
	}
	g.account = string(b)

	// Load cookie
	cookie := cfg.Session.Cookie
	setting, err := g.store.GetSetting(ctx, fmt.Sprintf("discord/%s/cookie", g.account))
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("generator: couldn't get cookie: %w", err)
	}
	if err == nil {
		cookie = setting.Value
	}

	if err := http.SetCookies(httpClient, "https://discord.com", cookie); err != nil {
		return fmt.Errorf("generator: couldn't set cookies: %w", err)
	}

	// TODO: update cookies periodically

	// Create discord client
	discordClient, err := discord.New(ctx, &discord.Config{
		Token:           cfg.Session.Token,
		SuperProperties: cfg.Session.SuperProperties,
		Locale:          cfg.Session.Locale,
		UserAgent:       cfg.Session.UserAgent,
		HTTPClient:      httpClient,
		Debug:           cfg.Debug,
	})
	if err != nil {
		return fmt.Errorf("generator: couldn't create discord client: %w", err)
	}

	g.httpClient = httpClient
	g.discordClient = discordClient
	return nil
}

func (g *Generator) Start(ctx context.Context) error {
	// Setup
	if err := g.setup(ctx); err != nil {
		return err
	}

	// Start discord client
	if err := g.discordClient.Start(ctx); err != nil {
		return fmt.Errorf("generator: couldn't start discord client: %w", err)
	}
	g.stop = func() error {
		defer func() {
			cookie, err := http.GetCookies(g.httpClient, "https://discord.com")
			if err != nil {
				log.Printf("generator: couldn't get cookies: %v\n", err)
			}
			if err := g.store.SetSetting(ctx, &storage.Setting{
				ID:    fmt.Sprintf("discord/%s/cookie", g.account),
				Value: cookie,
			}); err != nil {
				log.Printf("generator: couldn't save cookie: %v\n", err)
			}
		}()
		return g.discordClient.Stop()
	}

	// Check ai bot
	var newCli func(*discord.Client, string, bool) (ai.Client, error)
	switch strings.ToLower(g.cfg.Bot) {
	case "bluewillow":
		newCli = func(c *discord.Client, s string, b bool) (ai.Client, error) {
			return bluewillow.New(c, &bluewillow.Config{
				ChannelID: s,
				Debug:     b,
			})
		}
	case "midjourney":
		newCli = func(c *discord.Client, s string, b bool) (ai.Client, error) {
			return midjourney.New(c, &midjourney.Config{
				ChannelID:      s,
				Debug:          b,
				ReplicateToken: g.cfg.ReplicateToken,
				Timeout:        20 * time.Minute,
				QueuedTimeout:  30 * time.Minute,
			})
		}
	default:
		return fmt.Errorf("generator: unsupported bot: %s", g.cfg.Bot)
	}

	// Start ai client
	aiClient, err := newCli(g.discordClient, g.cfg.Channel, g.cfg.Debug)
	if err != nil {
		return fmt.Errorf("generator: couldn't create %s client: %w", g.cfg.Bot, err)
	}
	if err := aiClient.Start(ctx); err != nil {
		return fmt.Errorf("generator: couldn't start ai client: %w", err)
	}
	g.client = aiClient
	return nil
}

func (g *Generator) Stop() error {
	return g.stop()
}

func (g *Generator) HTTPClient() *fhttp.Client {
	return g.httpClient
}

func (g *Generator) Generate(ctx context.Context, text string) ([][]string, error) {
	preview, err := g.client.Imagine(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("generator: couldn't imagine: %w", err)
	}

	indexes := indexes()

	urlC := make(chan []string)
	defer close(urlC)

	var wg sync.WaitGroup
	wg.Add(len(indexes))
	defer wg.Wait()

	for _, i := range indexes {
		i := i
		// Wait a random amount of time between 800 and 2100 ms.
		time.Sleep(time.Duration(rand.Intn(1300)+800) * time.Millisecond)
		go func() {
			defer wg.Done()
			u, err := g.client.Upscale(ctx, preview, i)
			if err != nil {
				log.Println(fmt.Errorf("generator: couldn't upscale: %w", err))
			}
			select {
			case <-ctx.Done():
				return
			case urlC <- u:
			}
		}()
	}

	var urls [][]string
	for range indexes {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case url := <-urlC:
			if len(url) == 0 {
				continue
			}
			urls = append(urls, url)
		}
	}
	return urls, nil
}

func indexes() []int {
	// Create a slice of integers.
	indexes := []int{0, 1, 2, 3}

	// Seed the random number generator.
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Shuffle the slice.
	for i := len(indexes) - 1; i > 0; i-- {
		// Generate a random index between 0 and i.
		randomIndex := r.Intn(i + 1)

		// Swap the elements at the current index and the random index.
		indexes[i], indexes[randomIndex] = indexes[randomIndex], indexes[i]
	}

	rnd := rand.Intn(100)
	switch {
	case rnd < 2:
		// Remove 2 indexes from the slice
		indexes = indexes[:len(indexes)-2]
	case rnd < 10:
		// Remove 1 index from the slice
		indexes = indexes[:len(indexes)-1]
	}

	// Return the shuffled slice.
	return indexes
}

func (g *Generator) Download(ctx context.Context, u, output string) error {
	if g.discordClient == nil {
		if err := g.setup(ctx); err != nil {
			return err
		}
	}
	if err := g.discordClient.Download(ctx, u, output); err != nil {
		return fmt.Errorf("generator: couldn't download: %w", err)
	}
	return nil
}
