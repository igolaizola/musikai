package sonoteller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/igolaizola/musikai/pkg/fhttp"
	"github.com/igolaizola/musikai/pkg/ratelimit"
)

var token = "i95evCQoyT8gmwTmXHRewXB7cwXH2X69"

type Client struct {
	getClient func() fhttp.Client
	client    fhttp.Client
	debug     bool
	ratelimit ratelimit.Lock
}

type Config struct {
	Wait  time.Duration
	Proxy string
	Debug bool
}

func New(cfg *Config) (*Client, error) {
	wait := cfg.Wait
	if wait == 0 {
		wait = 1 * time.Second
	}

	if _, err := url.Parse(cfg.Proxy); err != nil {
		return nil, fmt.Errorf("sonoteller: couldn't parse proxy URL %q: %w", cfg.Proxy, err)
	}
	getClient := func() fhttp.Client {
		return fhttp.NewClient(2*time.Minute, true, cfg.Proxy)
	}
	return &Client{
		client:    getClient(),
		getClient: getClient,
		ratelimit: ratelimit.New(wait),
		debug:     cfg.Debug,
	}, nil
}

func (c *Client) Start(ctx context.Context) error {
	// Create log folder if it doesn't exist
	if _, err := os.Stat("logs"); os.IsNotExist(err) {
		if err := os.Mkdir("logs", 0755); err != nil {
			return fmt.Errorf("sonoteller: couldn't create logs folder: %w", err)
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
		var appErr *appError
		if errors.As(err, &errStatus) {
			switch int(errStatus) {
			case http.StatusBadGateway, http.StatusGatewayTimeout, 520:
				// Retry on these status codes
				retry = true
				wait = true
			case http.StatusForbidden, http.StatusTooManyRequests:
				maxAttempts = 1000
				c.client = c.getClient()
				err = nil
				retry = true
			default:
				return nil, err
			}
		} else if errors.As(err, &appErr) {
			msg := strings.ToLower(appErr.Message)
			if strings.Contains(msg, "quota") {
				c.client = c.getClient()
				err = nil
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

type appError struct {
	Message string `json:"error"`
}

func (e *appError) Error() string {
	return fmt.Sprintf("sonoteller: %s", e.Message)
}

func (c *Client) doAttempt(ctx context.Context, method, path string, in, out any) ([]byte, error) {
	var body []byte
	var reqBody io.Reader
	if in != nil {
		var err error
		body, err = json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("sonoteller: couldn't marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(body)
	}
	logBody := string(body)
	if len(logBody) > 100 {
		logBody = logBody[:100] + "..."
	}
	c.log("sonoteller: do %s %s %s", method, path, logBody)

	// Check if path is absolute
	u := fmt.Sprintf("https://us-central1-sonochordal-415613.cloudfunctions.net/%s", path)
	if strings.HasPrefix(path, "http") {
		u = path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("sonoteller: couldn't create request: %w", err)
	}
	c.addHeaders(req)

	unlock := c.ratelimit.Lock(ctx)
	defer unlock()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sonoteller: couldn't %s %s: %w", method, u, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sonoteller: couldn't read response body: %w", err)
	}
	c.log("sonoteller: response %s %s %d %s", method, path, resp.StatusCode, string(respBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMessage := string(respBody)
		if len(errMessage) > 100 {
			errMessage = errMessage[:100] + "..."
		}
		//_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
		return nil, fmt.Errorf("sonoteller: %s %s returned (%s): %w", method, u, errMessage, errStatusCode(resp.StatusCode))
	}
	if out != nil {
		var appErr appError
		if err := json.Unmarshal(respBody, &appErr); err == nil && appErr.Message != "" {
			return nil, &appErr
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			// Write response body to file for debugging.
			_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
			return nil, fmt.Errorf("sonoteller: couldn't unmarshal response body (%T): %w", out, err)
		}
	}
	return respBody, nil
}

func (c *Client) addHeaders(req *http.Request) {
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
	req.Header.Set("cache-control", "max-age=0")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("origin", "https://sonoteller.ai")
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="123", "Not:A-Brand";v="8", "Chromium";v="123"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "cross-site")
	req.Header.Set("user-agent", `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36`)
}
