package sonoteller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
)

type analyzeRequest struct {
	URL         string `json:"url"`
	User        string `json:"user"`
	Token       string `json:"token"`
	Fingerprint string `json:"fp"`
}

type analyzeResponse struct {
	Golden struct {
		Chords []map[string]string `json:"chords"`
		Start  string              `json:"start"`
	} `json:"golden"`
	Lyrics json.RawMessage `json:"lyrics"`
	Music  MusicAnalysis   `json:"music"`
	Title  string          `json:"title"`
}

type Analysis struct {
	Title  string          `json:"title"`
	Golden float32         `json:"golden"`
	Lyrics *LyricsAnalysis `json:"lyrics,omitempty"`
	Music  MusicAnalysis   `json:"music"`
}

type MusicAnalysis struct {
	BPM         float64        `json:"bpm"`
	Genres      map[string]int `json:"genres"`
	Instruments []string       `json:"instruments"`
	Key         string         `json:"key"`
	MetaArtist  string         `json:"meta_artist"`
	MetaTitle   string         `json:"meta_title"`
	Moods       map[string]int `json:"moods"`
	Styles      map[string]int `json:"styles"`
	VocalFamily string         `json:"vocal_family"`
}

type LyricsAnalysis struct {
	Explicit string           `json:"explicit"`
	Keywords []string         `json:"keywords"`
	Language string           `json:"language"`
	Moods    []map[string]int `json:"moods"`
	Summary  string           `json:"summary"`
	Themes   []map[string]int `json:"themes"`
}

func (c *Client) Analyze(ctx context.Context, id string) (*Analysis, error) {
	c.client = c.getClient()
	req := &analyzeRequest{
		URL:         fmt.Sprintf("https://www.youtube.com/watch?v=%s", id),
		User:        "web",
		Token:       token,
		Fingerprint: randomString(7),
	}
	var resp analyzeResponse
	b, err := c.do(ctx, "POST", "sonoteller_web_yt_api_multi_function", req, &resp)
	if err != nil {
		return nil, fmt.Errorf("sonoteller: couldn't analyze: %w", err)
	}

	// Parse string to float
	golden, err := strconv.ParseFloat(resp.Golden.Start, 32)
	if err != nil {
		return nil, fmt.Errorf("sonoteller: couldn't parse golden start (%s): %w", string(b), err)
	}

	analysis := Analysis{
		Title:  resp.Title,
		Golden: float32(golden),
		Music:  resp.Music,
	}

	var lyricsErr string
	if err := json.Unmarshal(resp.Lyrics, &lyricsErr); err != nil {
		var lyrics LyricsAnalysis
		if err := json.Unmarshal(resp.Lyrics, &lyrics); err != nil {
			return nil, fmt.Errorf("sonoteller: couldn't unmarshal lyrics (%s): %w", string(resp.Lyrics), err)
		}
		analysis.Lyrics = &lyrics
	} else if !strings.Contains(strings.ToLower(lyricsErr), "instrumental") {
		log.Println("sonoteller: error in lyrics analysis:", lyricsErr)
	}
	return &analysis, nil
}

func randomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
