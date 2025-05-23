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
	"os"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/igolaizola/musikai/pkg/fhttp"
	"github.com/igolaizola/musikai/pkg/nopecha"
	"github.com/igolaizola/musikai/pkg/ratelimit"
	"github.com/igolaizola/musikai/pkg/twocaptcha"
)

const (
	hcaptchaSiteKey = "2945592b-1928-43a9-8473-7e7fed3d752e"
)

type Client struct {
	client         fhttp.Client
	debug          bool
	ratelimit      ratelimit.Lock
	cookieStore    CookieStore
	expiration     time.Time
	minDuration    float32
	maxDuration    float32
	maxExtensions  int
	intro          bool
	resolveCaptcha func(context.Context) (string, error)
	parallel       bool
}

type Config struct {
	Wait            time.Duration
	Debug           bool
	Proxy           string
	CookieStore     CookieStore
	MinDuration     time.Duration
	MaxDuration     time.Duration
	MaxExtensions   int
	Parallel        bool
	CaptchaKey      string
	CaptchaProvider string
	CaptchaProxy    string
	SkipIntro       bool
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

func New(cfg *Config) (*Client, error) {
	wait := cfg.Wait
	if wait == 0 {
		wait = 1 * time.Second
	}
	client := fhttp.NewClient(2*time.Minute, true, cfg.Proxy)
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

	intro := !cfg.SkipIntro
	if intro {
		if maxExtensions <= 1 || minDuration <= 30*time.Second || maxDuration <= 30*time.Second {
			return nil, fmt.Errorf("udio: intro requires at least 2 extensions and 30 seconds duration")
		}
		maxExtensions -= 1
		maxDuration -= 30 * time.Second
		minDuration -= 30 * time.Second
	}

	// Set up captcha resolver
	if cfg.CaptchaKey == "" {
		return nil, fmt.Errorf("udio: captcha key is empty")
	}
	var resolveCaptcha func(context.Context) (string, error)
	switch cfg.CaptchaProvider {
	case "2captcha":
		cli := twocaptcha.NewClient(cfg.CaptchaKey)
		resolveCaptcha = func(ctx context.Context) (string, error) {
			req := (&twocaptcha.HCaptcha{
				SiteKey: hcaptchaSiteKey,
				Url:     "https://www.udio.com/",
			}).ToRequest()
			if cfg.CaptchaProxy != "" {
				proxy := strings.TrimPrefix(cfg.CaptchaProxy, "http://")
				req.SetProxy("http", proxy)
			}
			code, err := cli.Solve(req)
			if err != nil {
				return "", fmt.Errorf("udio: couldn't solve 2captcha: %w", err)
			}
			return code, nil
		}
	case "nopecha":
		cli, err := nopecha.New(&nopecha.Config{
			Wait:  1 * time.Second,
			Key:   cfg.CaptchaKey,
			Debug: false,
			Proxy: cfg.CaptchaProxy,
		})
		if err != nil {
			return nil, fmt.Errorf("udio: couldn't create nopecha client: %w", err)
		}
		resolveCaptcha = func(ctx context.Context) (string, error) {
			code, err := cli.Token(ctx, "hcaptcha", hcaptchaSiteKey, "https://www.udio.com/")
			if err != nil {
				return "", fmt.Errorf("udio: couldn't solve nopecha: %w", err)
			}
			return code, nil
		}
	default:
		return nil, fmt.Errorf("udio: invalid captcha provider: %s", cfg.CaptchaProvider)
	}

	return &Client{
		client:         client,
		ratelimit:      ratelimit.New(wait),
		debug:          cfg.Debug,
		cookieStore:    cfg.CookieStore,
		minDuration:    float32(minDuration.Seconds()),
		maxDuration:    float32(maxDuration.Seconds()),
		maxExtensions:  maxExtensions,
		resolveCaptcha: resolveCaptcha,
		parallel:       cfg.Parallel,
		intro:          true,
	}, nil
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
	if err := c.client.SetRawCookies("https://www.udio.com", cookie, nil); err != nil {
		return fmt.Errorf("udio: couldn't set cookie: %w", err)
	}

	// Authenticate
	if err := c.Auth(ctx); err != nil {
		return err
	}

	return nil
}

func (c *Client) Stop(ctx context.Context) error {
	cookie, err := c.client.GetRawCookies("https://www.udio.com")
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
	// Check if we need to refresh the token
	if c.expiration.IsZero() || time.Now().After(c.expiration) {
		if err := c.refresh(ctx); err != nil {
			return err
		}
	}
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
		var appErr appError
		if errors.As(err, &errStatus) {
			switch int(errStatus) {
			case http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusTooManyRequests, 520:
				// Retry on these status codes
				retry = true
				wait = true
			case http.StatusUnauthorized:
				// Retry on unauthorized
				if err := c.refresh(ctx); err != nil {
					return nil, err
				}
				retry = true
			default:
				return nil, err
			}
		} else if errors.As(err, &appErr) {
			msg := strings.ToLower(appErr.Message)
			if msg == "unauthorized" {
				// Retry on unauthorized
				if err := c.refresh(ctx); err != nil {
					return nil, err
				}
				retry = true
			} else {
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
			c.log("server seems to be down, waiting %s before retrying\n", waitTime)
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

type appError struct {
	Message string `json:"error"`
}

func (e appError) Error() string {
	return fmt.Sprintf("udio: %s", e.Message)
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
	c.addHeaders(req, path)

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
	var appErr appError
	if err := json.Unmarshal(respBody, &appErr); err == nil && appErr.Message != "" {
		return nil, &appErr
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

// Public API Key for api.udio.com
const apiKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6Im1mbXB4amVtYWNzaGZjcHpvc2x1Iiwicm9sZSI6ImFub24iLCJpYXQiOjE3MTAzNjAxNTcsImV4cCI6MjAyNTkzNjE1N30.YcGEN_n6AfHlfh4PIe4nTEe_PeC9WFU9A7vda7qMJH0"

func (c *Client) addHeaders(req *http.Request, path string) {
	switch {
	case strings.HasPrefix(path, "https://api.udio.com/auth"):
		req.Header.Set("accept", "*/*")
		req.Header.Set("accept-language", "es-ES,es;q=0.9,en;q=0.8")
		req.Header.Set("apikey", apiKey)
		req.Header.Set("authorization", fmt.Sprintf("Bearer %s", apiKey))
		req.Header.Set("content-type", "application/json;charset=UTF-8")
		req.Header.Set("origin", "https://www.udio.com")
		req.Header.Set("priority", "u=1, i")
		req.Header.Set("referer", "https://www.udio.com/")
		req.Header.Set("sec-ch-ua", `"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`)
		req.Header.Set("sec-ch-ua-mobile", "?0")
		req.Header.Set("sec-ch-ua-platform", `"Windows"`)
		req.Header.Set("sec-fetch-dest", "empty")
		req.Header.Set("sec-fetch-site", "same-site")
		req.Header.Set("user-agent", `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36`)
		req.Header.Set("x-client-info", "@supabase/auth-helpers-nextjs@0.8.7")
	default:
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
}
