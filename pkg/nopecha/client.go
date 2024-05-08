package nopecha

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
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/igolaizola/musikai/pkg/ratelimit"
)

type Client struct {
	client    *http.Client
	debug     bool
	ratelimit ratelimit.Lock
	key       string
	proxy     *url.URL
}

type Config struct {
	Wait   time.Duration
	Debug  bool
	Client *http.Client
	Key    string
	Proxy  string
}

func New(cfg *Config) (*Client, error) {
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

	var proxy *url.URL
	if cfg.Proxy != "" {
		var err error
		proxy, err = url.Parse(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("nopecha couldn't parse proxy URL: %w", err)
		}
	}

	return &Client{
		client:    client,
		ratelimit: ratelimit.New(wait),
		debug:     cfg.Debug,
		key:       cfg.Key,
		proxy:     proxy,
	}, nil
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
		if strings.Contains(err.Error(), "connection reset by peer") {
			continue
		}

		// Check if we should retry after waiting
		var retry bool
		var wait bool

		// Check status code
		var errStatus errStatusCode
		var appErr *appError
		var code int
		if errors.As(err, &errStatus) {
			code = int(errStatus)
		} else if errors.As(err, &appErr) {
			code = appErr.StatusCode
		}
		switch int(code) {
		case http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusTooManyRequests, 520, 500, 400:
			// Retry on these status codes
			retry = true
			wait = true
			err = nil
		default:
			return nil, err
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

const (
	incompleteJobCode  = 14
	maxRetriesCode     = 9
	invalidRequestCode = 10
)

type appError struct {
	Message    string `json:"message"`
	Code       int    `json:"error"`
	StatusCode int
}

func (e *appError) Error() string {
	return fmt.Sprintf("nopecha: app error %d (%d): %s", e.Code, e.StatusCode, e.Message)

}

func (c *Client) doAttempt(ctx context.Context, method, path string, in, out any) ([]byte, error) {
	var body []byte
	var reqBody io.Reader
	if f, ok := in.(url.Values); ok {
		reqBody = strings.NewReader(f.Encode())
	} else if in != nil {
		var err error
		body, err = json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("nopecha: couldn't marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(body)
	}
	logBody := string(body)
	c.log("nopecha: do %s %s %s", method, path, logBody)

	// Check if path is absolute
	u := fmt.Sprintf("https://api.nopecha.com/%s", path)
	if strings.HasPrefix(path, "http") {
		u = path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("nopecha: couldn't create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	unlock := c.ratelimit.Lock(ctx)
	defer unlock()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nopecha: couldn't %s %s: %w", method, u, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("nopecha: couldn't read response body: %w", err)
	}
	c.log("nopecha: response %s %s %d %s", method, path, resp.StatusCode, string(respBody))
	var appErr appError
	if err := json.Unmarshal(respBody, &appErr); err == nil && appErr.Code != 0 {
		appErr.StatusCode = resp.StatusCode
		return nil, &appErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMessage := string(respBody)
		if len(errMessage) > 100 {
			errMessage = errMessage[:100] + "..."
		}
		_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
		return nil, fmt.Errorf("nopecha %s %s returned (%s): %w", method, u, errMessage, errStatusCode(resp.StatusCode))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			// Write response body to file for debugging.
			_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
			return nil, fmt.Errorf("nopecha: couldn't unmarshal response body (%T): %w", out, err)
		}
	}
	return respBody, nil
}
