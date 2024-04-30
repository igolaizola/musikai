package nopecha

import (
	"context"
	"errors"
	"fmt"
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
		if _, err := c.do(ctx, "GET", path, nil, &resp); err != nil {
			return "", fmt.Errorf("nopecha: couldn't get token: %w", err)
		}
		if resp.Data != "" {
			return resp.Data, nil
		}
		if resp.Error == 14 {
			continue
		}
		return "", fmt.Errorf("nopecha: %s (%d)", resp.Message, resp.Error)
	}
}
