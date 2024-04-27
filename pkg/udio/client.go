package udio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/igolaizola/musikai/pkg/nopecha"
	"github.com/igolaizola/musikai/pkg/ratelimit"
	"github.com/igolaizola/musikai/pkg/session"
)

const (
	hcaptchaSiteKey = "2945592b-1928-43a9-8473-7e7fed3d752e"
)

type Client struct {
	client        *http.Client
	debug         bool
	ratelimit     ratelimit.Lock
	cookieStore   CookieStore
	minDuration   float32
	maxDuration   float32
	maxExtensions int
	nopechaClient *nopecha.Client
	parallel      bool
}

type Config struct {
	Wait          time.Duration
	Debug         bool
	Client        *http.Client
	CookieStore   CookieStore
	MinDuration   time.Duration
	MaxDuration   time.Duration
	MaxExtensions int
	Parallel      bool
	NopechaKey    string
}

type cookieStore struct {
	path string
}

func (c *cookieStore) GetCookie(ctx context.Context) (string, error) {
	b, err := os.ReadFile(c.path)
	if err != nil {
		return "", fmt.Errorf("udio: couldn't read cookie: %w", err)
	}
	return string(b), nil
}

func (c *cookieStore) SetCookie(ctx context.Context, cookie string) error {
	if err := os.WriteFile(c.path, []byte(cookie), 0644); err != nil {
		return fmt.Errorf("udio: couldn't write cookie: %w", err)
	}
	return nil
}

func NewCookieStore(path string) CookieStore {
	return &cookieStore{
		path: path,
	}
}

type CookieStore interface {
	GetCookie(context.Context) (string, error)
	SetCookie(context.Context, string) error
}

func New(cfg *Config) *Client {
	wait := cfg.Wait
	if wait == 0 {
		wait = 1 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{
			Timeout: 2 * time.Minute,
		}
	}
	minDuration := defaultMinDuration
	if cfg.MinDuration > 0 {
		minDuration = cfg.MinDuration
	}
	maxDuration := defaultMaxDuration
	if cfg.MaxDuration > 0 {
		maxDuration = cfg.MaxDuration
	}
	maxExtensions := defaultMaxExtensions
	if cfg.MaxExtensions > 0 {
		maxExtensions = cfg.MaxExtensions
	}
	nopechaClient := nopecha.New(&nopecha.Config{
		Wait:   100 * time.Millisecond,
		Key:    cfg.NopechaKey,
		Client: client,
		Debug:  cfg.Debug,
	})

	return &Client{
		client:        client,
		ratelimit:     ratelimit.New(wait),
		debug:         cfg.Debug,
		cookieStore:   cfg.CookieStore,
		minDuration:   float32(minDuration.Seconds()),
		maxDuration:   float32(maxDuration.Seconds()),
		maxExtensions: maxExtensions,
		nopechaClient: nopechaClient,
		parallel:      cfg.Parallel,
	}
}

func (c *Client) Start(ctx context.Context) error {
	// Create log folder if it doesn't exist
	if _, err := os.Stat("logs"); os.IsNotExist(err) {
		if err := os.Mkdir("logs", 0755); err != nil {
			return fmt.Errorf("udio: couldn't create logs folder: %w", err)
		}
	}

	// Get cookie
	cookie, err := c.cookieStore.GetCookie(ctx)
	if err != nil {
		return err
	}
	if cookie == "" {
		return fmt.Errorf("udio: cookie is empty")
	}
	if err := session.SetCookies(c.client, "https://www.udio.com", cookie, nil); err != nil {
		return fmt.Errorf("udio: couldn't set cookie: %w", err)
	}

	// Authenticate
	if err := c.Auth(ctx); err != nil {
		return err
	}

	return nil
}

func (c *Client) Stop(ctx context.Context) error {
	cookie, err := session.GetCookies(c.client, "https://www.udio.com")
	if err != nil {
		return fmt.Errorf("udio: couldn't get cookie: %w", err)
	}
	if cookie != "" {
		if err := c.cookieStore.SetCookie(ctx, cookie); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) log(format string, args ...interface{}) {
	if c.debug {
		format += "\n"
		log.Printf(format, args...)
	}
}

func (c *Client) Auth(ctx context.Context) error {
	if err := c.CheckLimit(ctx); err != nil {
		return err
	}
	return nil
}

var backoff = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	2 * time.Minute,
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) ([]byte, error) {
	maxAttempts := 3
	attempts := 0
	var err error
	for {
		if err != nil {
			log.Println("retrying...", err)
		}
		var b []byte
		b, err = c.doAttempt(ctx, method, path, in, out)
		if err == nil {
			return b, nil
		}
		// Increase attempts and check if we should stop
		attempts++
		if attempts >= maxAttempts {
			return nil, err
		}
		// If the error is temporary retry
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			continue
		}

		// Check if we should retry after waiting
		var retry bool
		var wait bool

		// Check status code
		var errStatus errStatusCode
		if errors.As(err, &errStatus) {
			switch int(errStatus) {
			case http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusTooManyRequests, 520:
				// Retry on these status codes
				retry = true
				wait = true
			case http.StatusUnauthorized:
				// Retry on unauthorized
				if err := c.Auth(ctx); err != nil {
					return nil, err
				}
				retry = true
			default:
				return nil, err
			}
		}
		if !retry {
			return nil, err
		}

		// Wait before retrying
		if wait {
			idx := attempts - 1
			if idx >= len(backoff) {
				idx = len(backoff) - 1
			}
			waitTime := backoff[idx]
			c.log("server seems to be down, waiting %s before retrying\n", wait)
			t := time.NewTimer(waitTime)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-t.C:
			}
		}
	}
}

type errStatusCode int

func (e errStatusCode) Error() string {
	return fmt.Sprintf("%d", e)
}

func (c *Client) doAttempt(ctx context.Context, method, path string, in, out any) ([]byte, error) {
	var body []byte
	var reqBody io.Reader
	if in != nil {
		var err error
		body, err = json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("udio: couldn't marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(body)
	}
	logBody := string(body)
	if len(logBody) > 100 {
		logBody = logBody[:100] + "..."
	}
	c.log("udio: do %s %s %s", method, path, logBody)

	// Check if path is absolute
	u := fmt.Sprintf("https://www.udio.com/api/%s", path)
	if strings.HasPrefix(path, "http") {
		u = path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("udio: couldn't create request: %w", err)
	}
	c.addHeaders(req)

	unlock := c.ratelimit.Lock(ctx)
	defer unlock()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("udio: couldn't %s %s: %w", method, u, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("udio: couldn't read response body: %w", err)
	}
	c.log("udio: response %s %s %d %s", method, path, resp.StatusCode, string(respBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMessage := string(respBody)
		if len(errMessage) > 100 {
			errMessage = errMessage[:100] + "..."
		}
		_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
		return nil, fmt.Errorf("udio: %s %s returned (%s): %w", method, u, errMessage, errStatusCode(resp.StatusCode))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			// Write response body to file for debugging.
			_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
			return nil, fmt.Errorf("udio: couldn't unmarshal response body (%T): %w", out, err)
		}
	}
	return respBody, nil
}

func (c *Client) addHeaders(req *http.Request) {
	req.Header.Set("accept", "application/json, text/plain, */*")
	req.Header.Set("accept-language", "es-ES,es;q=0.9,en;q=0.8")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("referer", "https://www.udio.com/")
	req.Header.Set("origin", "https://www.udio.com")
	req.Header.Set("sec-ch-ua", `"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("user-agent", `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36`)
}
