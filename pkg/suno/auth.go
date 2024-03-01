package suno

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (c *Client) Auth(ctx context.Context) error {
	if c.session == "" {
		id, err := c.sessionID(ctx)
		if err != nil {
			return err
		}
		c.session = id
	}
	if c.token != "" && time.Now().Before(c.tokenExpiration) {
		return nil
	}

	token, expiration, err := c.sessionToken(ctx, "api")
	if err != nil {
		return err
	}
	c.token = token
	// Set token expiration to 90% of the actual expiration
	c.tokenExpiration = time.Now().Add(expiration.Sub(time.Now().UTC()) * 90 / 100).UTC()
	return nil
}

type InitialState struct {
	Actor         string `json:"actor"`
	SessionClaims struct {
		Azp string `json:"azp"`
		Exp int64  `json:"exp"`
		Iat int64  `json:"iat"`
		Iss string `json:"iss"`
		Nbf int64  `json:"nbf"`
		Sid string `json:"sid"`
		Sub string `json:"sub"`
	} `json:"sessionClaims"`
	SessionId      string `json:"sessionId"`
	Session        string `json:"session"`
	UserId         string `json:"userId"`
	User           string `json:"user"`
	OrgId          string `json:"orgId"`
	OrgRole        string `json:"orgRole"`
	OrgSlug        string `json:"orgSlug"`
	OrgPermissions string `json:"orgPermissions"`
	Organization   string `json:"organization"`
}

type clerkClientResponse struct {
	Response clientResponse `json:"response"`
	Client   any            `json:"client"`
}

type clientResponse struct {
	Object              string          `json:"object"`
	ID                  string          `json:"id"`
	Sessions            []clientSession `json:"sessions"`
	LastActiveSessionID string          `json:"last_active_session_id"`
	CreatedAt           int64           `json:"created_at"`
	UpdatedAt           int64           `json:"updated_at"`
}

type clientSession struct {
	Object       string `json:"object"`
	ID           string `json:"id"`
	Status       string `json:"status"`
	ExpireAt     int64  `json:"expire_at"`
	AbandonAt    int64  `json:"abandon_at"`
	LastActiveAt int64  `json:"last_active_at"`
	// TODO: not needed for now
	// LastActiveOrganizationID *string         `json:"last_active_organization_id"`
	// Actor                    *Actor          `json:"actor"`
	// User                     User            `json:"user"`
	// PublicUserData           PublicUserData  `json:"public_user_data"`
	CreatedAt       int64 `json:"created_at"`
	UpdatedAt       int64 `json:"updated_at"`
	LastActiveToken struct {
		Object string `json:"object"`
		JWT    string `json:"jwt"`
	} `json:"last_active_token"`
}

// TODO: obtain this version from redirect response of https://clerk.suno.ai/npm/@clerk/clerk-js@4/dist/clerk.browser.js
const clerkVersion = "4.70.0"

func (c *Client) sessionID(ctx context.Context) (string, error) {
	var resp clerkClientResponse
	u := fmt.Sprintf("https://clerk.suno.ai/v1/client?_clerk_js_version=%s", clerkVersion)
	if _, err := c.do(ctx, "GET", u, nil, &resp); err != nil {
		return "", fmt.Errorf("suno: couldn't get client: %w", err)
	}
	if resp.Response.LastActiveSessionID == "" {
		return "", errors.New("suno: empty session id")
	}
	return resp.Response.LastActiveSessionID, nil
}

type clerkTokenResponse struct {
	JWT    string `json:"jwt"`
	Object string `json:"object"`
}

func (c *Client) sessionToken(ctx context.Context, path string) (string, time.Time, error) {
	if path != "" {
		path = fmt.Sprintf("/%s", path)
	}
	u := fmt.Sprintf("https://clerk.suno.ai/v1/client/sessions/%s/tokens%s?_clerk_js_version=%s", c.session, path, clerkVersion)
	var resp clerkTokenResponse
	if _, err := c.do(ctx, "POST", u, nil, &resp); err != nil {
		return "", time.Time{}, fmt.Errorf("suno: couldn't get clerk token: %w", err)
	}
	if resp.JWT == "" {
		return "", time.Time{}, errors.New("suno: empty clerk token")
	}
	claims, err := toClaims(resp.JWT)
	if err != nil {
		return "", time.Time{}, err
	}
	exp := time.Unix(claims.Exp, 0)
	return resp.JWT, exp, nil
}

type claims struct {
	Azp string `json:"azp"`
	Exp int64  `json:"exp"`
	Iat int64  `json:"iat"`
	Iss string `json:"iss"`
	Nbf int64  `json:"nbf"`
	Sid string `json:"sid"`
	Sub string `json:"sub"`
}

func toClaims(token string) (*claims, error) {
	// Split the JWT into its three parts
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("suno: invalid access token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("suno: couldn't decode access token: %w", err)
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, fmt.Errorf("suno: couldn't unmarshal access token: %w", err)
	}
	return &c, nil
}
