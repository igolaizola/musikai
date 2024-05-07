package nopecha

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

type tokenRequest struct {
	Key     string      `json:"key"`
	Type    string      `json:"type"`
	SiteKey string      `json:"sitekey"`
	URL     string      `json:"url"`
	Proxy   *tokenProxy `json:"proxy,omitempty"`
}

type tokenProxy struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   string `json:"port"`
}

type tokenResponse struct {
	Data    string `json:"data"`
	Error   int    `json:"error"`
	Message string `json:"message"`
}

func (c *Client) Token(ctx context.Context, typ, siteKey, u string) (string, error) {
	var t string
	var err error
	for i := 0; i < 3; i++ {
		t, err = c.token(ctx, typ, siteKey, u)
		var appErr *appError
		if errors.As(err, &appErr) {
			log.Println("❌ retrying...", err)
			continue
		}
		break
	}
	return t, err
}

func (c *Client) token(ctx context.Context, typ, siteKey, u string) (string, error) {
	req := &tokenRequest{
		Key:     c.key,
		Type:    typ,
		SiteKey: siteKey,
		URL:     u,
	}
	if c.proxy != nil {
		req.Proxy = &tokenProxy{
			Scheme: c.proxy.Scheme,
			Host:   c.proxy.Hostname(),
			Port:   c.proxy.Port(),
		}
	}
	var resp tokenResponse
	if _, err := c.do(ctx, "POST", "token", req, &resp); err != nil {
		return "", err
	}
	if resp.Data == "" {
		return "", errors.New("nopecha didn't return data")
	}
	path := fmt.Sprintf("token?key=%s&id=%s", c.key, resp.Data)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(15 * time.Second):
		}
		var resp tokenResponse
		_, err := c.do(ctx, "GET", path, nil, &resp)
		var appErr *appError
		switch {
		case errors.As(err, &appErr) && appErr.Code == 14:
			continue
		case err != nil:
			return "", err
		case resp.Data != "":
			return resp.Data, nil
		default:
			return "", fmt.Errorf("nopecha: %s (%d)", resp.Message, resp.Error)
		}
	}
}
