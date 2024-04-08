package sonoteller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
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
	Title  string
	Lyrics *LyricsAnalysis
	Music  MusicAnalysis
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

func (c *Client) Analyze(ctx context.Context, u string) (*Analysis, error) {
	c.newIP()
	req := &analyzeRequest{
		URL:         u,
		User:        "web",
		Token:       token,
		Fingerprint: randomString(7),
	}
	var resp analyzeResponse
	if _, err := c.do(ctx, "POST", "sonoteller_web_yt_api_multi_function", req, &resp); err != nil {
		return nil, fmt.Errorf("sonoteller: couldn't analyze: %w", err)
	}

	analysis := Analysis{
		Title: resp.Title,
		Music: resp.Music,
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
