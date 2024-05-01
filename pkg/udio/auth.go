package udio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/igolaizola/musikai/pkg/session"
)

type apiUsageResponse struct {
	Data struct {
		Tier               string    `json:"tier"`
		ConcurrentUsed     int       `json:"concurrent_used"`
		DailyUsed          int       `json:"daily_used"`
		MonthlyUsed        int       `json:"monthly_used"`
		Disabled           bool      `json:"disabled"`
		Discretionary      int       `json:"discretionary"`
		StartDay           time.Time `json:"start_day"`
		StartMonth         time.Time `json:"start_month"`
		LastUse            time.Time `json:"last_use"`
		ConcurrentLimit    int       `json:"concurrent_limit"`
		DailyThrottleLimit int       `json:"daily_throttle_limit"`
		DailyThrottled     bool      `json:"daily_throttled"`
		MonthlyLimit       int       `json:"monthly_limit"`
	} `json:"data"`
}

func (c *Client) CheckLimit(ctx context.Context) error {
	var resp apiUsageResponse
	if _, err := c.do(ctx, "GET", "users/current/api-usage", nil, &resp); err != nil {
		return fmt.Errorf("udio: couldn't get api usage: %w", err)
	}
	if resp.Data.Disabled {
		return errors.New("udio: api disabled")
	}
	if resp.Data.DailyThrottled {
		return errors.New("udio: daily throttled")
	}
	if resp.Data.DailyUsed >= resp.Data.DailyThrottleLimit {
		return errors.New("udio: daily limit reached")
	}
	if resp.Data.MonthlyUsed >= resp.Data.MonthlyLimit {
		return errors.New("udio: monthly limit reached")
	}
	return nil
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type authData struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	ExpiresAt    int64  `json:"expires_at"`
	RefreshToken string `json:"refresh_token"`
	User         struct {
		ID               string `json:"id"`
		Aud              string `json:"aud"`
		Role             string `json:"role"`
		Email            string `json:"email"`
		EmailConfirmedAt string `json:"email_confirmed_at"`
		Phone            string `json:"phone"`
		ConfirmedAt      string `json:"confirmed_at"`
		LastSignInAt     string `json:"last_sign_in_at"`
		CreatedAt        string `json:"created_at"`
		UpdatedAt        string `json:"updated_at"`
		IsAnonymous      bool   `json:"is_anonymous"`
	} `json:"user"`
}

func (c *Client) refresh(ctx context.Context) error {
	u, err := url.Parse("https://www.udio.com")
	if err != nil {
		return fmt.Errorf("udio: couldn't parse url: %w", err)
	}
	var authToken string
	for _, cookie := range c.client.Jar.Cookies(u) {
		if !strings.HasPrefix(cookie.Name, "sb-ssr-production-auth-token.") {
			continue
		}
		authToken += cookie.Value
	}
	if authToken == "" {
		return errors.New("udio: couldn't find auth token")
	}

	// Decode auth token
	authToken, err = url.QueryUnescape(authToken)
	if err != nil {
		return fmt.Errorf("udio: couldn't decode auth token (%s): %w", authToken, err)
	}
	var auth authData
	if err := json.Unmarshal([]byte(authToken), &auth); err != nil {
		return fmt.Errorf("udio: couldn't unmarshal auth token (%s): %w", authToken, err)
	}
	refreshToken := auth.RefreshToken

	// Refresh token
	req := &refreshRequest{
		RefreshToken: refreshToken,
	}
	var resp authData
	raw, err := c.do(ctx, "POST", "https://api.udio.com/auth/v1/token?grant_type=refresh_token", req, &resp)
	if err != nil {
		return fmt.Errorf("udio: couldn't refresh token: %w", err)
	}

	expiration := resp.ExpiresIn * 70 / 100
	c.expiration = time.Now().Add(time.Duration(expiration) * time.Second)

	encoded := url.QueryEscape(string(raw))

	// Split in chunks of 3180 characters
	var cookies []string
	idx := 0
	for i := 0; i < len(encoded); i += 3180 {
		end := i + 3180
		if end > len(encoded) {
			end = len(encoded)
		}
		c := fmt.Sprintf("sb-ssr-production-auth-token.%d=%s", idx, encoded[i:end])
		cookies = append(cookies, c)
		idx++
	}

	cookie := strings.Join(cookies, "; ")
	if err := session.SetCookies(c.client, "https://www.udio.com", cookie, nil); err != nil {
		return fmt.Errorf("udio: couldn't set cookie: %w", err)
	}
	if err := c.cookieStore.SetCookie(ctx, cookie); err != nil {
		return err
	}
	return nil
}

type userResponse struct {
	User struct {
		ID          string      `json:"id"`
		Factors     interface{} `json:"factors"`
		Aud         string      `json:"aud"`
		Iat         int64       `json:"iat"`
		Iss         string      `json:"iss"`
		Email       string      `json:"email"`
		Phone       string      `json:"phone"`
		AppMetadata struct {
			Provider  string   `json:"provider"`
			Providers []string `json:"providers"`
		} `json:"app_metadata"`
		UserMetadata struct {
			AvatarURL     string `json:"avatar_url"`
			Email         string `json:"email"`
			EmailVerified bool   `json:"email_verified"`
			FullName      string `json:"full_name"`
			Iss           string `json:"iss"`
			Name          string `json:"name"`
			PhoneVerified bool   `json:"phone_verified"`
			Picture       string `json:"picture"`
			ProviderID    string `json:"provider_id"`
			Sub           string `json:"sub"`
		} `json:"user_metadata"`
		Role string `json:"role"`
		AAL  string `json:"aal"`
		AMR  []struct {
			Method    string `json:"method"`
			Timestamp int64  `json:"timestamp"`
		} `json:"amr"`
		SessionID   string    `json:"session_id"`
		IsAnonymous bool      `json:"is_anonymous"`
		CreatedAt   time.Time `json:"created_at"`
	} `json:"user"`
}

func (c *Client) User(ctx context.Context) (string, error) {
	var resp userResponse
	if _, err := c.do(ctx, "GET", "users/current", nil, &resp); err != nil {
		return "", err
	}
	return resp.User.Email, nil
}
