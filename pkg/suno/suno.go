package suno

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/igolaizola/musikai/pkg/ratelimit"
	"github.com/igolaizola/musikai/pkg/session"
)

// TODO: obtain this version from redirect response of https://clerk.suno.ai/npm/@clerk/clerk-js@4/dist/clerk.browser.js
const ClerkVersion = "4.70.0"

type Client struct {
	client          *http.Client
	debug           bool
	ratelimit       ratelimit.Lock
	session         string
	token           string
	tokenExpiration time.Time
	cookieStore     CookieStore
}

type Config struct {
	Wait        time.Duration
	Debug       bool
	Client      *http.Client
	CookieStore CookieStore
}

type cookieStore struct {
	dir string
}

func (c *cookieStore) GetCookie(ctx context.Context, domain string) (string, error) {
	path := filepath.Join(c.dir, fmt.Sprintf("%s.cookie", domain))
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("suno: couldn't read cookie: %w", err)
	}
	return string(b), nil
}

func (c *cookieStore) SetCookie(ctx context.Context, domain, cookie string) error {
	path := filepath.Join(c.dir, fmt.Sprintf("%s.cookie", domain))
	if err := os.WriteFile(path, []byte(cookie), 0644); err != nil {
		return fmt.Errorf("suno: couldn't write cookie: %w", err)
	}
	return nil
}

func NewCookieStore(dir string) CookieStore {
	return &cookieStore{
		dir: dir,
	}
}

type CookieStore interface {
	GetCookie(context.Context, string) (string, error)
	SetCookie(context.Context, string, string) error
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
	return &Client{
		client:      client,
		ratelimit:   ratelimit.New(wait),
		debug:       cfg.Debug,
		cookieStore: cfg.CookieStore,
	}
}

var domains = []string{"app.suno.ai", "clerk.suno.ai"}

func (c *Client) Start(ctx context.Context) error {
	// Get cookie
	for _, domain := range domains {
		cookie, err := c.cookieStore.GetCookie(ctx, domain)
		if err != nil {
			return err
		}
		if cookie == "" {
			return fmt.Errorf("suno: cookie is empty")
		}
		if err := session.SetCookies(c.client, domain, cookie, nil); err != nil {
			return fmt.Errorf("suno: couldn't set cookie: %w", err)
		}
	}

	// Authenticate
	if err := c.Auth(ctx); err != nil {
		return err
	}

	return nil
}

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

	token, expiration, err := c.sessionToken(ctx)
	if err != nil {
		return err
	}
	c.token = token
	// Set token expiration to 90% of the actual expiration
	c.tokenExpiration = time.Now().Add(expiration.Sub(time.Now().UTC()) * 90 / 100).UTC()
	return nil
}

func (c *Client) Stop(ctx context.Context) error {
	for _, domain := range domains {
		cookie, err := session.GetCookies(c.client, domain)
		if err != nil {
			return fmt.Errorf("suno: couldn't get cookie: %w", err)
		}
		if err := c.cookieStore.SetCookie(ctx, domain, cookie); err != nil {
			return err
		}
	}
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

func (c *Client) sessionID(ctx context.Context) (string, error) {
	b, err := c.do(ctx, "GET", "https://app.suno.ai", nil, nil)
	if err != nil {
		return "", fmt.Errorf("suno: couldn't get session: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
	if err != nil {
		return "", fmt.Errorf("suno: couldn't create document from reader: %w", err)
	}

	// Define regex pattern to extract initialState object
	pattern := `"initialState":(\{.*?\}),"children"`
	re := regexp.MustCompile(pattern)

	// Search json
	var js string
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		t := s.Text()
		t = strings.ReplaceAll(t, `\"`, `"`)

		// Obtain a slice of strings holding the text of the leftmost match
		matches := re.FindStringSubmatch(t)
		if matches == nil || len(matches) < 2 {
			return
		}

		// First is the entire match, second is the captured group
		js = matches[1]
	})
	if js == "" {
		return "", fmt.Errorf("suno: couldn't find initial state")
	}

	var state InitialState
	if err := json.Unmarshal([]byte(js), &state); err != nil {
		return "", fmt.Errorf("suno: couldn't unmarshal json (%s): %w", js, err)
	}
	if state.SessionId == "" {
		return "", fmt.Errorf("suno: couldn't find session id")
	}
	return state.SessionId, nil
}

type clerkTokenResponse struct {
	JWT    string `json:"jwt"`
	Object string `json:"object"`
}

func (c *Client) sessionToken(ctx context.Context) (string, time.Time, error) {
	u := fmt.Sprintf("https://clerk.suno.ai/v1/client/sessions/%s/tokens?_clerk_js_version=%s", c.session, ClerkVersion)
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

type GenerateV2Request struct {
	GPTDescriptionPrompt string `json:"gpt_description_prompt"`
	MV                   string `json:"mv"`
	Prompt               string `json:"prompt"`
	MakeInstrumental     bool   `json:"make_instrumental"`
}

type GenerateV2Response struct {
	ID                string    `json:"id"`
	Clips             []Clip    `json:"clips"`
	Metadata          Metadata  `json:"metadata"`
	MajorModelVersion string    `json:"major_model_version"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	BatchSize         int       `json:"batch_size"`
}

type Clip struct {
	ID                string    `json:"id"`
	VideoURL          string    `json:"video_url"`
	AudioURL          string    `json:"audio_url"`
	ImageURL          *string   `json:"image_url"`
	MajorModelVersion string    `json:"major_model_version"`
	ModelName         string    `json:"model_name"`
	Metadata          Metadata  `json:"metadata"`
	IsLiked           bool      `json:"is_liked"`
	UserID            string    `json:"user_id"`
	IsTrashed         bool      `json:"is_trashed"`
	Reaction          any       `json:"reaction"`
	CreatedAt         time.Time `json:"created_at"`
	Status            string    `json:"status"`
	Title             string    `json:"title"`
	PlayCount         int       `json:"play_count"`
	UpvoteCount       int       `json:"upvote_count"`
	IsPublic          bool      `json:"is_public"`
}

type Metadata struct {
	Tags                 any     `json:"tags"`
	Prompt               string  `json:"prompt"`
	GPTDescriptionPrompt string  `json:"gpt_description_prompt"`
	AudioPromptID        *string `json:"audio_prompt_id"`
	History              any     `json:"history"`
	ConcatHistory        any     `json:"concat_history"`
	Type                 string  `json:"type"`
	Duration             *int    `json:"duration"`
	RefundCredits        *int    `json:"refund_credits"`
	Stream               bool    `json:"stream"`
	ErrorType            *string `json:"error_type"`
	ErrorMessage         *string `json:"error_message"`
}

type Song struct {
	ID       string
	Title    string
	AudioURL string
	ImageURL string
}

func (c *Client) GenerateV2(ctx context.Context, prompt string) ([]Song, error) {
	if err := c.Auth(ctx); err != nil {
		return nil, err
	}
	req := &GenerateV2Request{
		GPTDescriptionPrompt: prompt,
		MV:                   "chirp-v2-xxl-alpha",
	}
	var resp GenerateV2Response
	if _, err := c.do(ctx, "POST", "generate/v2/", req, &resp); err != nil {
		return nil, fmt.Errorf("suno: couldn't generate song: %w", err)
	}
	if len(resp.Clips) == 0 {
		return nil, errors.New("suno: empty clips")
	}
	if resp.Metadata.ErrorType != nil {
		return nil, fmt.Errorf("suno: couldn't generate song: (%v) %s", *resp.Metadata.ErrorType, *resp.Metadata.ErrorMessage)
	}

	var ids []string
	for _, clip := range resp.Clips {
		ids = append(ids, clip.ID)
	}
	u := fmt.Sprintf("feed/?ids=%s", strings.Join(ids, ","))

	var last []byte
	for {
		select {
		case <-ctx.Done():
			log.Println("suno: context done, last response:", string(last))
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
		if err := c.Auth(ctx); err != nil {
			return nil, err
		}
		var clips []Clip
		if _, err := c.do(ctx, "GET", u, nil, &clips); err != nil {
			return nil, fmt.Errorf("suno: couldn't get clips: %w", err)
		}
		var pending bool
		for _, clip := range clips {
			if clip.AudioURL == "" {
				pending = true
				break
			}
		}
		if !pending {
			break
		}
	}
	var songs []Song
	for _, clip := range resp.Clips {
		songs = append(songs, Song{
			ID:       clip.ID,
			Title:    clip.Title,
			AudioURL: clip.AudioURL,
			ImageURL: *clip.ImageURL,
		})
	}
	return songs, nil
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

		// Check status code
		var errStatus errStatusCode
		if errors.As(err, &errStatus) {
			switch int(errStatus) {
			case http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusTooManyRequests:
				// Retry on these status codes
				retry = true
			default:
				return nil, err
			}
		}

		// Check API error
		var errAPI errAPI
		if errors.As(err, &errAPI) {
			if errAPI.code == invalidJWTCode {
				// If the JWT is invalid we should re-authenticate
				if err := c.Auth(ctx); err != nil {
					return nil, err
				}
			}
			// Retry on any API error
			retry = true
		}

		if !retry {
			return nil, err
		}

		// Wait before retrying
		idx := attempts - 1
		if idx >= len(backoff) {
			idx = len(backoff) - 1
		}
		wait := backoff[idx]
		c.log("server seems to be down, waiting %s before retrying\n", wait)
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

type errorResponse struct {
	Errors []struct {
		Message    string `json:"message"`
		Extensions struct {
			Code string `json:"code"`
		} `json:"extensions"`
	} `json:"errors"`
}

type form struct {
	writer *multipart.Writer
	data   *bytes.Buffer
}

type errStatusCode int

func (e errStatusCode) Error() string {
	return fmt.Sprintf("%d", e)
}

// Known error codes
const (
	invalidJWTCode = "invalid-jwt"
)

type errAPI struct {
	code string
}

func (e errAPI) Error() string {
	return e.code
}

func (c *Client) doAttempt(ctx context.Context, method, path string, in, out any) ([]byte, error) {
	var body []byte
	var reqBody io.Reader
	if in != nil {
		var err error
		body, err = json.Marshal(in)
		if err != nil {
			return nil, fmt.Errorf("suno: couldn't marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(body)
	}
	logBody := string(body)
	if len(logBody) > 100 {
		logBody = logBody[:100] + "..."
	}
	c.log("suno: do %s %s %s", method, path, logBody)

	// Check if path is absolute
	u := fmt.Sprintf("https://studio-api.suno.ai/api/%s", path)
	if strings.HasPrefix(path, "http") {
		u = path
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return nil, fmt.Errorf("suno: couldn't create request: %w", err)
	}
	c.addHeaders(req, path)

	unlock := c.ratelimit.Lock(ctx)
	defer unlock()

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("suno: couldn't %s %s: %w", method, u, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("suno: couldn't read response body: %w", err)
	}
	c.log("suno: response %s %s %d %s", method, path, resp.StatusCode, string(respBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMessage := string(respBody)
		if len(errMessage) > 100 {
			errMessage = errMessage[:100] + "..."
		}
		_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
		return nil, fmt.Errorf("suno: %s %s returned (%s): %w", method, u, errMessage, errStatusCode(resp.StatusCode))
	}
	if out != nil {
		var errResp errorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && len(errResp.Errors) > 0 {
			var msgs []string
			for _, e := range errResp.Errors {
				msgs = append(msgs, fmt.Sprintf("%s (%s)", e.Message, e.Extensions.Code))
			}
			_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
			return nil, fmt.Errorf("suno: %s: %w", strings.Join(msgs, ", "), errAPI{code: errResp.Errors[0].Extensions.Code})
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			// Write response body to file for debugging.
			_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
			return nil, fmt.Errorf("suno: couldn't unmarshal response body (%T): %w", out, err)
		}
	}
	return respBody, nil
}

func (c *Client) addHeaders(req *http.Request, path string) {
	switch {
	case strings.HasPrefix(path, "https://app.suno.ai"):
		req.Header.Set("accept", "accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		req.Header.Set("accept-language", "en-US,en;q=0.9")
		req.Header.Set("sec-ch-ua", `"Not A(Brand";v="99", "Google Chrome";v="121", "Chromium";v="121"`)
		req.Header.Set("sec-ch-ua-mobile", "?0")
		req.Header.Set("sec-ch-ua-platform", `"Windows"`)
		req.Header.Set("sec-fetch-dest", "document")
		req.Header.Set("sec-fetch-mode", "navigate")
		req.Header.Set("sec-fetch-site", "none")
		req.Header.Set("sec-fetch-user", "?1")
		req.Header.Set("upgrade-insecure-requests", "1")
		req.Header.Set("user-agent", `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36`)
	case strings.HasPrefix(path, "https://clerk.suno.ai"):
		req.Header.Set("accept", "*/*")
		req.Header.Set("accept-language", "en-US,en;q=0.9")
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
		req.Header.Set("origin", "https://app.suno.ai")
		req.Header.Set("referer", "https://app.suno.ai/")
		req.Header.Set("sec-ch-ua", `"Not A(Brand";v="99", "Google Chrome";v="121", "Chromium";v="121"`)
		req.Header.Set("sec-ch-ua-mobile", "?0")
		req.Header.Set("sec-ch-ua-platform", `"Windows"`)
		req.Header.Set("sec-fetch-dest", "empty")
		req.Header.Set("sec-fetch-mode", "cors")
		req.Header.Set("sec-fetch-site", "same-site")
		req.Header.Set("user-agent", `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36`)
	case strings.HasPrefix(path, "feed"):
		req.Header.Set("accept", "*/*")
		req.Header.Set("accept-language", "en-US,en;q=0.9")
		req.Header.Set("authorization", fmt.Sprintf("Bearer %s", c.token))
		req.Header.Set("origin", "https://app.suno.ai")
		req.Header.Set("referer", "https://app.suno.ai/")
		req.Header.Set("sec-ch-ua", `"Not A(Brand";v="99", "Google Chrome";v="121", "Chromium";v="121"`)
		req.Header.Set("sec-ch-ua-mobile", "?0")
		req.Header.Set("sec-ch-ua-platform", `"Windows"`)
		req.Header.Set("sec-fetch-dest", "empty")
		req.Header.Set("sec-fetch-mode", "cors")
		req.Header.Set("sec-fetch-site", "same-site")
		req.Header.Set("user-agent", `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36`)
	default:
		req.Header.Set("accept", "*/*")
		req.Header.Set("accept-language", "en-US,en;q=0.9")
		req.Header.Set("authorization", fmt.Sprintf("Bearer %s", c.token))
		req.Header.Set("content-type", "text/plain;charset=UTF-8")
		req.Header.Set("origin", "https://app.suno.ai")
		req.Header.Set("referer", "https://app.suno.ai/")
		req.Header.Set("sec-ch-ua", `"Not A(Brand";v="99", "Google Chrome";v="121", "Chromium";v="121"`)
		req.Header.Set("sec-ch-ua-mobile", "?0")
		req.Header.Set("sec-ch-ua-platform", `"Windows"`)
		req.Header.Set("sec-fetch-dest", "empty")
		req.Header.Set("sec-fetch-mode", "cors")
		req.Header.Set("sec-fetch-site", "same-site")
		req.Header.Set("user-agent", `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36`)
	}
}
