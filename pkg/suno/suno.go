package suno

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/sound"
)

const (
	defaultMinDuration   = 2*time.Minute + 5*time.Second
	defaultMaxDuration   = 3*time.Minute + 55*time.Second
	defaultMaxExtensions = 1
)

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
	Style        string     `json:"style"`
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
				Style:        clp.Metadata.Tags,
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
		if duration > c.minDuration && extensions > 0 && !ends {
			var urls []string
			for _, c := range clips {
				urls = append(urls, c.AudioURL)
			}
			c.err("suno: didn't end: (%s)", strings.Join(urls, ", "))
		}

		if duration > c.minDuration {
			break
		}

		// If we are extending the song, recalculate duration
		duration = prevDuration + continueAt

		// Generate next fragment
		extensions++

		// If the duration is over the max duration, add prompt to make it end
		var prompt string
		tags := originalTags
		if duration+30.0 > c.minDuration {
			prompt = c.endPrompt
			if c.endStyle != "" {
				if c.endStyleAppend {
					tags += c.endStyle
				} else {
					tags = c.endStyle
				}
			}
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
