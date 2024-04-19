package udio

import (
	"context"
	"time"
)

const (
	defaultMinDuration   = 2*time.Minute + 5*time.Second
	defaultMaxDuration   = 3*time.Minute + 55*time.Second
	defaultMaxExtensions = 2
)

type UserResponse struct {
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
}

func (c *Client) User(ctx context.Context) (string, error) {
	var resp UserResponse
	if _, err := c.do(ctx, "GET", "users/current", nil, &resp); err != nil {
		return "", err
	}
	return resp.Email, nil
}
