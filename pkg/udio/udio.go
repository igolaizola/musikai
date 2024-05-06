package udio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/music"
	"github.com/igolaizola/musikai/pkg/sound"
)

const (
	defaultMinDuration   = 2*time.Minute + 5*time.Second
	defaultMaxDuration   = 3*time.Minute + 55*time.Second
	defaultMaxExtensions = 6
)

type generateRequest struct {
	Prompt         string         `json:"prompt"`
	LyricInput     *string        `json:"lyricInput,omitempty"`
	SamplerOptions samplerOptions `json:"samplerOptions"`
	CaptchaToken   string         `json:"captchaToken"`
}

type samplerOptions struct {
	Seed                         int       `json:"seed"`
	AudioConditioningCropSeconds []float64 `json:"audio_conditioning_crop_seconds,omitempty"`
	CropStartTime                float64   `json:"crop_start_time,omitempty"` // 0.4 extend, 0.9 outro 0.0 intro
	BypassPromptOptimize         bool      `json:"bypass_prompt_optimization"`
	AudioConditioningPath        string    `json:"audio_conditioning_path,omitempty"`
	AudioConditioningSongID      string    `json:"audio_conditioning_song_id,omitempty"`
	AudioConditioningType        string    `json:"audio_conditioning_type,omitempty"` // continuation, precede
}

type generateResponse struct {
	Message      string   `json:"message"`
	GenerationID string   `json:"generation_id"`
	TrackIDs     []string `json:"track_ids"`
}

func (c *Client) Generate(ctx context.Context, prompt string, manual, instrumental bool) ([][]music.Song, error) {
	// Check auth
	if err := c.Auth(ctx); err != nil {
		return nil, err
	}

	// TODO: Get lyrics from input
	var lyrics *string
	if instrumental {
		s := ""
		lyrics = &s
	}

	// Generate first fragments
	req := &generateRequest{
		Prompt:     prompt,
		LyricInput: lyrics,
		SamplerOptions: samplerOptions{
			Seed:                 -1,
			BypassPromptOptimize: manual,
		},
	}
	resp, err := c.tryGenerate(ctx, req, 0)
	if err != nil {
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

			clips, err := c.extend(ctx, f, manual, lyrics)
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
					Lyrics:       clp.Lyrics,
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

func (c *Client) tryGenerate(ctx context.Context, req *generateRequest, attempt int) (*generateResponse, error) {
	if attempt > 3 {
		return nil, errors.New("udio: too many attempts")
	}

	captchaToken, err := c.resolveCaptcha(ctx)
	if err != nil {
		return nil, err
	}
	req.CaptchaToken = captchaToken

	var resp generateResponse
	_, err = c.do(ctx, "POST", "generate-proxy", req, &resp)
	var errStatus errStatusCode
	if errors.As(err, &errStatus) && int(errStatus) == 500 {
		log.Println("❌ udio: generation failed with status 500, retrying with new captcha token")
		return c.tryGenerate(ctx, req, attempt+1)
	}
	if err != nil {
		return nil, fmt.Errorf("udio: couldn't generate: %w", err)
	}
	return &resp, nil
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
	Duration    float32  `json:"duration"`
	VideoPath   *string  `json:"video_path"`
	ErrorDetail *string  `json:"error_detail"`
	Finished    bool     `json:"finished"`
	Liked       bool     `json:"liked"`
	Disliked    bool     `json:"disliked"`
}

func (c *Client) extend(ctx context.Context, clp *clip, manual bool, lyrics *string) ([]*clip, error) {
	// Initialize variables
	clips := []*clip{clp}
	var duration, prevDuration float32
	var extensions int
	var over bool

	for {
		// Check clip silences
		lookup := map[string]struct {
			firstSilencePosition time.Duration
			clip                 *clip
			ends                 bool
		}{}

		// Check if the song is over
		if over {
			break
		}

		for _, c := range clips {
			a, err := sound.NewAnalyzer(c.SongPath)
			if err != nil {
				return nil, fmt.Errorf("udio: couldn't create analyzer: %w", err)
			}
			silences, err := a.Silences(ctx)
			if err != nil {
				return nil, fmt.Errorf("udio: couldn't get silences: %w", err)
			}
			var firstSilencePosition, endSilenceDuration time.Duration
			if len(silences) > 0 {
				for _, s := range silences {
					if s.Start.Seconds() > float64(c.Duration*0.70) {
						firstSilencePosition = s.Start
						break
					}
				}
				last := silences[len(silences)-1]
				if last.Final {
					endSilenceDuration = last.Duration
				}
			}

			// Check if the clip ends
			var ends bool
			if c.Duration-prevDuration < 20.0 {
				ends = true
			}
			if endSilenceDuration > 0 {
				ends = true
			}
			if a.HasFadeOut() {
				ends = true
			}

			// Add to lookup
			lookup[c.ID] = struct {
				firstSilencePosition time.Duration
				clip                 *clip
				ends                 bool
			}{
				firstSilencePosition: firstSilencePosition,
				clip:                 c,
				ends:                 ends,
			}
		}

		var okClips []*clip
		for _, c := range clips {
			if lookup[c.ID].ends {
				continue
			}
			okClips = append(okClips, c)
		}
		if len(okClips) == 0 {
			okClips = clips
		}

		// Choose random clip
		rnd := rand.Intn(len(okClips))
		clp = okClips[rnd]

		duration = clp.Duration

		switch {
		// Check if the song is over the min duration
		case duration > c.maxDuration:
			over = true
		// Check if the song is over the max extensions
		case extensions >= c.maxExtensions:
			over = true
		// Check if the extensions is less than 20 seconds
		case extensions > 0 && clp.Duration-prevDuration < 20.0:
			over = true
		}

		// Check if has ended and we don't want to add an intro
		if over && !c.intro {
			break
		}

		// Generate next fragment
		extensions++

		prevDuration = duration
		var cropSeconds []float64
		firstSilence := lookup[clp.ID].firstSilencePosition
		if firstSilence > 0 {
			cropSeconds = []float64{
				0.0,
				firstSilence.Seconds() - 1.0,
			}
			prevDuration = float32(cropSeconds[1])
			log.Println("✂️ udio: cropping", cropSeconds, clp.Title)
		}

		cropStartTime := 0.0
		conditioning := "continuation"
		if c.intro && over {
			log.Println("▶️ udio: setting intro", clp.Title)
			conditioning = "precede"
		} else {
			// If the duration is over the min duration, set outro settings
			if prevDuration+30.0 > c.maxDuration || extensions == c.maxExtensions {
				cropStartTime = 0.9
				log.Println("🔚 udio: setting outro", clp.Title)
			}
		}

		// Check auth
		if err := c.Auth(ctx); err != nil {
			return nil, err
		}

		// Generate extension
		req := &generateRequest{
			Prompt:     clp.Prompt,
			LyricInput: lyrics,
			SamplerOptions: samplerOptions{
				Seed:                         -1,
				CropStartTime:                cropStartTime,
				AudioConditioningCropSeconds: cropSeconds,
				BypassPromptOptimize:         manual,
				AudioConditioningPath:        clp.SongPath,
				AudioConditioningSongID:      clp.ID,
				AudioConditioningType:        conditioning,
			},
		}
		resp, err := c.tryGenerate(ctx, req, 0)
		if err != nil {
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
		for _, c := range clips {
			log.Println("🎵 fragment duration:", c.Duration)
		}
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
		case <-time.After(15 * time.Second):
		}
		if _, err := c.do(ctx, "GET", u, nil, &resp); err != nil {
			return nil, fmt.Errorf("udio: couldn't get clips: %w", err)
		}
		clips := resp.Clips
		var oks []*clip
		var errs []string
		for _, clip := range clips {
			if clip.ErrorID != nil {
				js, _ := json.Marshal(clip)
				log.Println("❌ udio: clip error:", string(js))
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
