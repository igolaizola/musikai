package udio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/music"
)

const (
	defaultMinDuration   = 2*time.Minute + 5*time.Second
	defaultMaxDuration   = 3*time.Minute + 55*time.Second
	defaultMaxExtensions = 6
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

type generateRequest struct {
	Prompt         string         `json:"prompt"`
	LyricInput     string         `json:"lyricInput"`
	SamplerOptions samplerOptions `json:"samplerOptions"`
	CaptchaToken   string         `json:"captchaToken"`
}

type samplerOptions struct {
	Seed                    int     `json:"seed"`
	CropStartTime           float64 `json:"crop_start_time,omitempty"`
	BypassPromptOptimize    bool    `json:"bypass_prompt_optimization"`
	AudioConditioningPath   string  `json:"audio_conditioning_path,omitempty"`
	AudioConditioningSongID string  `json:"audio_conditioning_song_id,omitempty"`
	AudioConditioningType   string  `json:"audio_conditioning_type,omitempty"`
}

type generateResponse struct {
	Message      string   `json:"message"`
	GenerationID string   `json:"generation_id"`
	TrackIDs     []string `json:"track_ids"`
}

func (c *Client) Generate(ctx context.Context, prompt string, manual, instrumental bool) ([][]music.Song, error) {
	if !instrumental {
		return nil, errors.New("udio: only instrumental songs are supported")
	}

	// Get captcha token
	captchaToken, err := c.nopechaClient.Token(ctx, "hcaptcha", hcaptchaSiteKey, "https://www.udio.com/")
	if err != nil {
		return nil, err
	}

	// Generate first fragments
	req := &generateRequest{
		Prompt:     prompt,
		LyricInput: "",
		SamplerOptions: samplerOptions{
			Seed:                 -1,
			BypassPromptOptimize: manual,
		},
		CaptchaToken: captchaToken,
	}
	var resp generateResponse
	if _, err := c.do(ctx, "POST", "generate-proxy", req, &resp); err != nil {
		return nil, err
	}
	if resp.Message != "Success" {
		return nil, fmt.Errorf("udio: generation failed: %s", resp.Message)
	}
	if len(resp.TrackIDs) == 0 {
		return nil, errors.New("udio: empty clips")
	}
	fragments, err := c.waitClips(ctx, resp.TrackIDs)
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
	songs := [][]music.Song{}
	var wg sync.WaitGroup
	var lck sync.Mutex
	for _, fragment := range fragments {
		f := fragment

		// Wait for semaphore
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("udio: %w", ctx.Err())
		case sem <- struct{}{}:
		}
		wg.Add(1)

		go func() {
			// Defer wait group and semaphore
			defer wg.Done()
			defer func() { <-sem }()

			clips, err := c.extend(ctx, f, manual)
			if err != nil {
				log.Printf("❌ %v\n", err)
				return
			}
			ss := []music.Song{}
			for _, clp := range clips {
				videoPath := ""
				if clp.VideoPath != nil {
					videoPath = *clp.VideoPath
				}
				ss = append(ss, music.Song{
					ID:           clp.ID,
					Title:        clp.Title,
					Style:        strings.Join(clp.Tags, ", "),
					Audio:        clp.SongPath,
					Image:        clp.ImagePath,
					Video:        videoPath,
					Duration:     float32(clp.Duration),
					Instrumental: instrumental,
				})
			}
			lck.Lock()
			defer lck.Unlock()
			songs = append(songs, ss)
		}()
	}

	// Wait for all fragments to be extended
	wg.Wait()
	if len(songs) == 0 {
		return nil, errors.New("udio: no songs generated")
	}
	return songs, nil
}

type clipsResponse struct {
	Clips []*clip `json:"songs"`
}

type clip struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	Artist       string     `json:"artist"`
	Title        string     `json:"title"`
	CreatedAt    time.Time  `json:"created_at"`
	ErrorID      *string    `json:"error_id"`
	ErrorType    *string    `json:"error_type"`
	GenerationID string     `json:"generation_id"`
	ImagePath    string     `json:"image_path"`
	Lyrics       string     `json:"lyrics"`
	Prompt       string     `json:"prompt"`
	Likes        int        `json:"likes"`
	Plays        int        `json:"plays"`
	PublishedAt  *time.Time `json:"published_at"`
	ReplacedTags map[string]struct {
		Tags []string `json:"tags"`
		Type string   `json:"type"`
	} `json:"replaced_tags"`
	SongPath    string   `json:"song_path"`
	Tags        []string `json:"tags"`
	Duration    float64  `json:"duration"`
	VideoPath   *string  `json:"video_path"`
	ErrorDetail *string  `json:"error_detail"`
	Finished    bool     `json:"finished"`
	Liked       bool     `json:"liked"`
	Disliked    bool     `json:"disliked"`
}

func (c *Client) extend(ctx context.Context, clp *clip, manual bool) ([]*clip, error) {
	// Initialize variables
	clips := []*clip{clp}
	var duration float32
	var extensions int

	for {
		// TODO: Choose the best clip

		// Choose random clip
		rnd := rand.Intn(len(clips))
		clp = clips[rnd]

		duration = float32(clp.Duration)

		// Check if the song is over the min duration
		if duration > c.maxDuration {
			break
		}

		// Check if the song is over the max extensions
		if extensions >= c.maxExtensions {
			break
		}

		// Generate next fragment
		extensions++

		// If the duration is over the min duration, set outro settings
		cropStartTime := 0.0
		if duration+30.0 > c.minDuration || extensions == c.maxExtensions {
			cropStartTime = 0.9
		}

		// Check auth
		if err := c.Auth(ctx); err != nil {
			return nil, err
		}

		// Get captcha token
		captchaToken, err := c.nopechaClient.Token(ctx, "hcaptcha", hcaptchaSiteKey, "https://www.udio.com/")
		if err != nil {
			return nil, err
		}

		// Generate extension
		req := &generateRequest{
			Prompt:     clp.Prompt,
			LyricInput: "",
			SamplerOptions: samplerOptions{
				Seed:                    -1,
				CropStartTime:           cropStartTime,
				BypassPromptOptimize:    manual,
				AudioConditioningPath:   clp.SongPath,
				AudioConditioningSongID: clp.ID,
				AudioConditioningType:   "continuation",
			},
			CaptchaToken: captchaToken,
		}
		var resp generateResponse
		if _, err := c.do(ctx, "POST", "generate-proxy", req, &resp); err != nil {
			return nil, err
		}
		if resp.Message != "Success" {
			return nil, fmt.Errorf("udio: generation failed: %s", resp.Message)
		}
		if len(resp.TrackIDs) == 0 {
			return nil, errors.New("udio: empty clips")
		}
		candidates, err := c.waitClips(ctx, resp.TrackIDs)
		if err != nil {
			return nil, err
		}
		clips = candidates
	}

	// If there are no extensions, return the original clip
	if extensions == 0 {
		return []*clip{clp}, nil
	}

	// Sort clips putting clp first
	sort.Slice(clips, func(i, j int) bool {
		return clips[i].ID == clp.ID
	})
	return clips, nil
}

func (c *Client) waitClips(ctx context.Context, ids []string) ([]*clip, error) {
	u := fmt.Sprintf("songs?songIds=%s", strings.Join(ids, ","))
	var last []byte
	for {
		var resp clipsResponse
		select {
		case <-ctx.Done():
			log.Println("udio: context done, last response:", string(last))
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
		if err := c.Auth(ctx); err != nil {
			return nil, err
		}
		if _, err := c.do(ctx, "GET", u, nil, &resp); err != nil {
			return nil, fmt.Errorf("udio: couldn't get clips: %w", err)
		}
		clips := resp.Clips
		var oks []*clip
		var errs []string
		for _, clip := range clips {
			if clip.ErrorID != nil {
				errs = append(errs, *clip.ErrorID)
			} else if clip.SongPath != "" {
				oks = append(oks, clip)
			}
		}
		if len(errs)+len(oks) < len(clips) {
			continue
		}
		for _, id := range errs {
			log.Printf("❌ udio: clip %s returned error status\n", id)
		}
		if len(oks) == 0 {
			return nil, fmt.Errorf("udio: all clips failed: %v", errs)
		}
		return oks, nil
	}
}
