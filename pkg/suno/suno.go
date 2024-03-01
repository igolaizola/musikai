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
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/ratelimit"
	"github.com/igolaizola/musikai/pkg/session"
	"github.com/igolaizola/musikai/pkg/sound"
)

// TODO: obtain this version from redirect response of https://clerk.suno.ai/npm/@clerk/clerk-js@4/dist/clerk.browser.js
const clerkVersion = "4.70.0"

type Client struct {
	client          *http.Client
	debug           bool
	ratelimit       ratelimit.Lock
	session         string
	token           string
	tokenExpiration time.Time
	cookieStore     CookieStore
	parallel        bool
	lck             sync.Mutex
}

type Config struct {
	Wait        time.Duration
	Debug       bool
	Client      *http.Client
	CookieStore CookieStore
	Parallel    bool
}

type cookieStore struct {
	path string
}

func (c *cookieStore) GetCookie(ctx context.Context) (string, error) {
	b, err := os.ReadFile(c.path)
	if err != nil {
		return "", fmt.Errorf("suno: couldn't read cookie: %w", err)
	}
	return string(b), nil
}

func (c *cookieStore) SetCookie(ctx context.Context, cookie string) error {
	if err := os.WriteFile(c.path, []byte(cookie), 0644); err != nil {
		return fmt.Errorf("suno: couldn't write cookie: %w", err)
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
	return &Client{
		client:      client,
		ratelimit:   ratelimit.New(wait),
		debug:       cfg.Debug,
		cookieStore: cfg.CookieStore,
		parallel:    cfg.Parallel,
	}
}

func (c *Client) Start(ctx context.Context) error {
	// Create log folder if it doesn't exist
	if _, err := os.Stat("logs"); os.IsNotExist(err) {
		if err := os.Mkdir("logs", 0755); err != nil {
			return fmt.Errorf("suno: couldn't create logs folder: %w", err)
		}
	}

	// Get cookie
	cookie, err := c.cookieStore.GetCookie(ctx)
	if err != nil {
		return err
	}
	if cookie == "" {
		return fmt.Errorf("suno: cookie is empty")
	}
	if err := session.SetCookies(c.client, "https://clerk.suno.ai", cookie, nil); err != nil {
		return fmt.Errorf("suno: couldn't set cookie: %w", err)
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

	token, expiration, err := c.sessionToken(ctx, "api")
	if err != nil {
		return err
	}
	c.token = token
	// Set token expiration to 90% of the actual expiration
	c.tokenExpiration = time.Now().Add(expiration.Sub(time.Now().UTC()) * 90 / 100).UTC()
	return nil
}

func (c *Client) Stop(ctx context.Context) error {
	cookie, err := session.GetCookies(c.client, "https://clerk.suno.ai")
	if err != nil {
		return fmt.Errorf("suno: couldn't get cookie: %w", err)
	}
	if err := c.cookieStore.SetCookie(ctx, cookie); err != nil {
		return err
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

type generateRequest struct {
	Prompt               string   `json:"prompt"`
	Tags                 string   `json:"tags,omitempty"`
	MV                   string   `json:"mv"`
	Title                string   `json:"title,omitempty"`
	ContinueClipID       *string  `json:"continue_clip_id"`
	ContinueAt           *float32 `json:"continue_at"`
	GPTDescriptionPrompt string   `json:"gpt_description_prompt,omitempty"`
	MakeInstrumental     bool     `json:"make_instrumental,omitempty"`
}

type generateResponse struct {
	ID                string    `json:"id"`
	Clips             []clip    `json:"clips"`
	Metadata          metadata  `json:"metadata"`
	MajorModelVersion string    `json:"major_model_version"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	BatchSize         int       `json:"batch_size"`
}

type clip struct {
	ID                string    `json:"id"`
	VideoURL          string    `json:"video_url"`
	AudioURL          string    `json:"audio_url"`
	ImageURL          string    `json:"image_url"`
	MajorModelVersion string    `json:"major_model_version"`
	ModelName         string    `json:"model_name"`
	Metadata          metadata  `json:"metadata"`
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

type metadata struct {
	Tags                 string  `json:"tags"`
	Prompt               string  `json:"prompt"`
	GPTDescriptionPrompt string  `json:"gpt_description_prompt"`
	AudioPromptID        *string `json:"audio_prompt_id"`
	History              []struct {
		ID         string  `json:"id"`
		ContinueAt float32 `json:"continue_at"`
	} `json:"history"`
	ConcatHistory []struct {
		ID         string  `json:"id"`
		ContinueAt float32 `json:"continue_at"`
	} `json:"concat_history"`
	Type          string  `json:"type"`
	Duration      float32 `json:"duration"`
	RefundCredits bool    `json:"refund_credits"`
	Stream        bool    `json:"stream"`
	ErrorType     *string `json:"error_type"`
	ErrorMessage  *string `json:"error_message"`
}

type Fragment struct {
	ID         string  `json:"id"`
	ContinueAt float32 `json:"continue_at"`
}

type concatRequest struct {
	ClipID string `json:"clip_id"`
}

type Song struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Audio        string     `json:"audio"`
	Image        string     `json:"image"`
	Video        string     `json:"video"`
	Duration     float32    `json:"duration"`
	Instrumental bool       `json:"instrumental"`
	History      []Fragment `json:"history"`
}

func (c *Client) Generate(ctx context.Context, prompt, style, title string, instrumental bool) ([]Song, error) {
	if err := c.Auth(ctx); err != nil {
		return nil, err
	}

	// Generate first fragments
	req := &generateRequest{
		GPTDescriptionPrompt: prompt,
		MV:                   "chirp-v3-alpha",
		Tags:                 style,
		Title:                title,
		MakeInstrumental:     instrumental,
	}
	var resp generateResponse
	if _, err := c.do(ctx, "POST", "generate/v2/", req, &resp); err != nil {
		return nil, fmt.Errorf("suno: couldn't generate song: %w", err)
	}
	if len(resp.Clips) == 0 {
		return nil, errors.New("suno: empty clips")
	}
	if resp.Metadata.ErrorType != nil {
		return nil, fmt.Errorf("suno: song generation error: (%v) %s", *resp.Metadata.ErrorType, *resp.Metadata.ErrorMessage)
	}
	var ids []string
	for _, clip := range resp.Clips {
		ids = append(ids, clip.ID)
	}
	fragments, err := c.waitClips(ctx, ids)
	if err != nil {
		return nil, err
	}

	// Create a semaphore to limit concurrency
	concurrency := 1
	if c.parallel {
		concurrency = len(fragments)
	}
	sem := make(chan struct{}, concurrency)

	// Extend fragments
	var songs []Song
	var wg sync.WaitGroup
	var lck sync.Mutex
	for _, fragment := range fragments {
		f := &fragment

		// Wait for semaphore
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("suno: %w", ctx.Err())
		case sem <- struct{}{}:
		}
		wg.Add(1)

		go func() {
			// Defer wait group and semaphore
			defer wg.Done()
			defer func() { <-sem }()

			clp, err := c.extend(ctx, f)
			if err != nil {
				log.Printf("❌ %v\n", err)
				return
			}
			var history []Fragment
			for _, h := range clp.Metadata.ConcatHistory {
				history = append(history, Fragment{
					ID:         h.ID,
					ContinueAt: h.ContinueAt,
				})
			}
			lck.Lock()
			defer lck.Unlock()
			songs = append(songs, Song{
				ID:           clp.ID,
				Title:        clp.Title,
				Audio:        clp.AudioURL,
				Image:        clp.ImageURL,
				Video:        clp.VideoURL,
				Duration:     clp.Metadata.Duration,
				Instrumental: instrumental,
				History:      history,
			})
		}()
	}

	// Wait for all fragments to be extended
	wg.Wait()
	if len(songs) == 0 {
		return nil, errors.New("suno: no songs generated")
	}
	return songs, nil
}

const (
	minDuration   = 2.0*60.0 + 5.0
	maxDuration   = 3.0*60.0 + 55.0
	maxExtensions = 1
)

func (c *Client) extend(ctx context.Context, clp *clip) (*clip, error) {
	// Initialize variables
	clips := []clip{*clp}
	originalTags := clp.Metadata.Tags
	var duration float32
	var extensions int

	for {
		// Choose the best clip
		var best string
		lookup := map[string]struct {
			firstSilence time.Duration
			endSilence   time.Duration
			clip         *clip
		}{}

		for _, c := range clips {
			a, err := sound.NewAnalyzer(c.AudioURL)
			if err != nil {
				return nil, fmt.Errorf("suno: couldn't create analyzer: %w", err)
			}
			// Add to lookup
			_, firstPos := a.FirstSilence()
			endSilence, _ := a.EndSilence()
			lookup[c.ID] = struct {
				firstSilence time.Duration
				endSilence   time.Duration
				clip         *clip
			}{
				firstSilence: firstPos,
				endSilence:   endSilence,
				clip:         &c,
			}

			// Check if the clip ends
			if c.Metadata.Duration < 59.0 {
				best = c.ID
				break
			}
			if a.HasFadeOut() {
				best = c.ID
				break
			}
			if endSilence > 500*time.Millisecond {
				best = c.ID
				break
			}
		}

		ends := true
		var firstSilence time.Duration
		if best == "" {
			ends = false
			// Choose random clip
			rnd := rand.Intn(len(clips))
			clp = &clips[rnd]
		} else {
			clp = lookup[best].clip
		}

		prevDuration := duration
		duration += clp.Metadata.Duration

		continueAt := clp.Metadata.Duration
		firstSilence = lookup[clp.ID].firstSilence
		if firstSilence > 0 {
			continueAt = float32(firstSilence.Seconds() - 1.0)
		}
		if firstSilence > 0 && firstSilence.Seconds() < float64(clp.Metadata.Duration*0.5) && extensions == 0 {
			err := fmt.Errorf("suno: first silence too short: (%s, %s)", firstSilence, clp.AudioURL)
			c.err("%v", err)
			return nil, err
		}

		// If the duration is over the min duration, log
		if duration > minDuration && extensions > 0 && !ends {
			var urls []string
			for _, c := range clips {
				urls = append(urls, c.AudioURL)
			}
			c.err("suno: didn't end: (%s)", strings.Join(urls, ", "))
		}

		// TODO: check if this is too much
		if extensions >= maxExtensions {
			break
		}

		if duration > minDuration {
			break
		}

		// If we are extending the song, recalculate duration
		duration = prevDuration + continueAt

		// Generate next fragment
		extensions++

		// If the duration is over the max duration, add prompt to make it end
		var prompt string
		tags := originalTags
		if duration+30.0 > minDuration {
			prompt = "[refrain]"
			tags = "end"
		}
		req := &generateRequest{
			Prompt:         prompt,
			Tags:           tags,
			MV:             "chirp-v3-alpha",
			Title:          clp.Title,
			ContinueClipID: &clp.ID,
			ContinueAt:     &continueAt,
			// TODO: check if we need to set this on extensions
			// MakeInstrumental: instrumental,
		}
		var resp generateResponse
		if _, err := c.do(ctx, "POST", "generate/v2/", req, &resp); err != nil {
			return nil, fmt.Errorf("suno: couldn't generate song: %w", err)
		}
		if len(resp.Clips) == 0 {
			return nil, errors.New("suno: empty clips")
		}
		if resp.Metadata.ErrorType != nil {
			return nil, fmt.Errorf("suno: song generation error: (%v) %s", *resp.Metadata.ErrorType, *resp.Metadata.ErrorMessage)
		}
		var ids []string
		for _, c := range resp.Clips {
			ids = append(ids, c.ID)
		}
		candidates, err := c.waitClips(ctx, ids)
		if err != nil {
			return nil, err
		}
		clips = candidates
	}

	// If there are no extensions, return the original clip
	if extensions == 0 {
		return clp, nil
	}

	// Concatenate clips
	req := &concatRequest{
		ClipID: clp.ID,
	}
	var resp clip
	if _, err := c.do(ctx, "POST", "generate/concat/v2/", req, &resp); err != nil {
		return nil, fmt.Errorf("suno: couldn't concat song: %w", err)
	}
	if resp.Metadata.ErrorType != nil {
		return nil, fmt.Errorf("suno: song concat error: (%v) %s", *resp.Metadata.ErrorType, *resp.Metadata.ErrorMessage)
	}
	clips, err := c.waitClips(ctx, []string{resp.ID})
	if err != nil {
		return nil, err
	}
	return &clips[0], nil
}

func bestClip(clips []clip, duration float32, info func(clip) (bool, time.Duration, error)) (*clip, bool, time.Duration, error) {
	// Check if the clip fades out
	var infos []clipInfo
	for _, c := range clips {
		fadeOut, firstSilence, err := info(c)
		if err != nil {
			return nil, false, 0, fmt.Errorf("suno: couldn't check song fade out: %w", err)
		}
		d := duration + c.Metadata.Duration
		infos = append(infos, clipInfo{
			fadesOut:     fadeOut,
			firstSilence: firstSilence,
			timeOK:       d >= minDuration,
			clip:         &c,
		})
	}

	// Choose the best clip
	sort.Slice(infos, func(i, j int) bool {
		switch {
		// If both fade out, choose the one with the longest duration
		case infos[i].fadesOut == infos[j].fadesOut:
		// If both over the min duration, choose the one that doesn't fade out
		case infos[i].timeOK == infos[j].timeOK && infos[i].timeOK:
			return !infos[i].fadesOut
		// If both under the min duration, choose the one that doesn't fade out
		case infos[i].timeOK == infos[j].timeOK && !infos[i].timeOK:
			return infos[i].fadesOut
		}
		return clips[i].Metadata.Duration > clips[j].Metadata.Duration
	})
	best := infos[0]
	return best.clip, best.fadesOut, best.firstSilence, nil
}

type clipInfo struct {
	fadesOut     bool
	timeOK       bool
	firstSilence time.Duration
	clip         *clip
}

func (c *Client) waitClips(ctx context.Context, ids []string) ([]clip, error) {
	u := fmt.Sprintf("feed/?ids=%s", strings.Join(ids, ","))
	var last []byte
	var clips []clip
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
		if _, err := c.do(ctx, "GET", u, nil, &clips); err != nil {
			return nil, fmt.Errorf("suno: couldn't get clips: %w", err)
		}
		var pending bool
		for _, clip := range clips {
			if clip.Status != "complete" {
				pending = true
				break
			}
		}
		if !pending {
			break
		}
	}
	return clips, nil
}

func (c *Client) log(format string, args ...interface{}) {
	if c.debug {
		format += "\n"
		log.Printf(format, args...)
	}
}

func (c *Client) err(format string, args ...interface{}) {
	text := fmt.Sprintf(format, args...)
	log.Println("❌", text)
	c.lck.Lock()
	defer c.lck.Unlock()
	f, err := os.OpenFile("errors.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(fmt.Errorf("suno: couldn't open errors file: %w", err))
	}
	defer f.Close()
	if _, err := f.WriteString(fmt.Sprintf("%s\n", text)); err != nil {
		panic(fmt.Errorf("suno: couldn't write to errors file: %w", err))
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
		if err := json.Unmarshal(respBody, out); err != nil {
			// Write response body to file for debugging.
			_ = os.WriteFile(fmt.Sprintf("logs/debug_%s.json", time.Now().Format("20060102_150405")), respBody, 0644)
			return nil, fmt.Errorf("suno: couldn't unmarshal response body (%T): %w", out, err)
		}
	}
	return respBody, nil
}

func (c *Client) addHeaders(req *http.Request, path string) {
	// Custom headers for different paths
	var token string
	var contentType string
	switch {
	case strings.HasPrefix(path, "https://clerk.suno.ai"):
		contentType = "application/x-www-form-urlencoded"
	case strings.HasPrefix(path, "feed"):
		token = c.token
	default:
		token = c.token
		contentType = "text/plain;charset=UTF-8"
	}
	// Set headers
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-language", "en-US,en;q=0.9")
	if token != "" {
		req.Header.Set("authorization", fmt.Sprintf("Bearer %s", token))
	}
	if contentType != "" {
		req.Header.Set("content-type", contentType)
	}
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
