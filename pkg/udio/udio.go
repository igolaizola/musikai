package udio

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

const (
	defaultMinDuration   = 2*time.Minute + 5*time.Second
	defaultMaxDuration   = 3*time.Minute + 55*time.Second
	defaultMaxExtensions = 2
)

type UserResponse struct {
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
	var resp UserResponse
	if _, err := c.do(ctx, "GET", "users/current", nil, &resp); err != nil {
		return "", err
	}
	return resp.User.Email, nil
}

type generateRequest struct {
	Prompt         string `json:"prompt"`
	LyricInput     string `json:"lyricInput"`
	SamplerOptions struct {
		Seed                 int  `json:"seed"`
		BypassPromptOptimize bool `json:"bypass_prompt_optimization"`
	} `json:"samplerOptions"`
	CaptchaToken string `json:"captchaToken"`
}

type generateResponse struct {
	Message      string   `json:"message"`
	GenerationID string   `json:"generation_id"`
	TrackIDs     []string `json:"track_ids"`
}

type Song struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Style        string  `json:"style"`
	Audio        string  `json:"audio"`
	Image        string  `json:"image"`
	Video        string  `json:"video"`
	Duration     float32 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	//History      []Fragment `json:"history"`
}

func (c *Client) Generate(ctx context.Context, prompt, lyrics string) ([][]Song, error) {
	// Get captcha token
	captchaToken, err := c.nopechaClient.Token(ctx, "hcaptcha", hcaptchaSiteKey, "https://www.udio.com/")
	if err != nil {
		return nil, err
	}

	// Generate first fragments
	req := &generateRequest{
		Prompt:     prompt,
		LyricInput: lyrics,
		SamplerOptions: struct {
			Seed                 int  `json:"seed"`
			BypassPromptOptimize bool `json:"bypass_prompt_optimization"`
		}{
			Seed:                 -1,
			BypassPromptOptimize: false,
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
		return nil, errors.New("suno: empty clips")
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
	songs := [][]Song{}
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

			clips, err := c.extend(ctx, f)
			if err != nil {
				log.Printf("❌ %v\n", err)
				return
			}
			ss := []Song{}
			for _, clp := range clips {
				videoPath := ""
				if clp.VideoPath != nil {
					videoPath = *clp.VideoPath
				}
				ss = append(ss, Song{
					ID:       clp.ID,
					Title:    clp.Title,
					Style:    strings.Join(clp.Tags, ", "),
					Audio:    clp.SongPath,
					Image:    clp.ImagePath,
					Video:    videoPath,
					Duration: float32(clp.Duration),
					// TODO: determine if instrumental
					Instrumental: true,
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
		return nil, errors.New("suno: no songs generated")
	}
	return songs, nil
}

type clipsResponse struct {
	Clips []clip `json:"songs"`
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

func (c *Client) extend(_ context.Context, clp *clip) ([]*clip, error) {
	// TODO: implement
	return []*clip{clp}, nil
}

func (c *Client) waitClips(ctx context.Context, ids []string) ([]clip, error) {
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
		var oks []clip
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
