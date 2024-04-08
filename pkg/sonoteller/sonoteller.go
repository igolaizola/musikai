package sonoteller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sort"
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
	Music  struct {
		BPM         float64        `json:"bpm"`
		Genres      map[string]int `json:"genres"`
		Instruments []string       `json:"instruments"`
		Key         string         `json:"key"`
		MetaArtist  string         `json:"meta_artist"`
		MetaTitle   string         `json:"meta_title"`
		Moods       map[string]int `json:"moods"`
		Styles      map[string]int `json:"styles"`
		VocalFamily string         `json:"vocal_family"`
	} `json:"music"`
	Title string `json:"title"`
}

type analyzeLyricsResponse struct {
	Explicit string           `json:"explicit"`
	Keywords []string         `json:"keywords"`
	Language string           `json:"language"`
	Moods    []map[string]int `json:"moods"`
	Summary  string           `json:"summary"`
	Themes   []map[string]int `json:"themes"`
}

type WeigthValue struct {
	Weigth int
	Value  string
}

type Analysis struct {
	Title  string
	Lyrics *LyricsAnalysis
	Music  MusicAnalysis
}

type LyricsAnalysis struct {
	Explicit bool
	Keywords []string
	Language string
	Moods    []WeigthValue
	Summary  string
	Themes   []WeigthValue
}

type MusicAnalysis struct {
	BPM         float64
	Genres      []WeigthValue
	Instruments []string
	Key         string
	MetaArtist  string
	MetaTitle   string
	Moods       []WeigthValue
	Styles      []WeigthValue
	VocalFamily string
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
		Music: MusicAnalysis{
			BPM:         resp.Music.BPM,
			Genres:      toWeightValues(resp.Music.Genres),
			Instruments: resp.Music.Instruments,
			Key:         resp.Music.Key,
			MetaArtist:  resp.Music.MetaArtist,
			MetaTitle:   resp.Music.MetaTitle,
			Moods:       toWeightValues(resp.Music.Moods),
			Styles:      toWeightValues(resp.Music.Styles),
			VocalFamily: resp.Music.VocalFamily,
		},
	}

	var lyricsErr string
	if err := json.Unmarshal(resp.Lyrics, &lyricsErr); err != nil {
		var lyricsResp analyzeLyricsResponse
		if err := json.Unmarshal(resp.Lyrics, &lyricsResp); err != nil {
			return nil, fmt.Errorf("sonoteller: couldn't unmarshal lyrics (%s): %w", string(resp.Lyrics), err)
		}
		analysis.Lyrics = &LyricsAnalysis{
			Explicit: lyricsResp.Explicit == "true",
			Keywords: lyricsResp.Keywords,
			Language: lyricsResp.Language,
			Moods:    toWeightValues(lyricsResp.Moods[0]),
			Summary:  lyricsResp.Summary,
			Themes:   toWeightValues(lyricsResp.Themes[0]),
		}
	} else if !strings.Contains(strings.ToLower(lyricsErr), "instrumental") {
		log.Println("sonoteller: error in lyrics analysis:", lyricsErr)
	}
	return &analysis, nil
}

func toWeightValues(m map[string]int) []WeigthValue {
	var wv []WeigthValue
	for k, v := range m {
		wv = append(wv, WeigthValue{Weigth: v, Value: k})
	}
	// Sort descending
	sort.Slice(wv, func(i, j int) bool {
		return wv[i].Weigth > wv[j].Weigth
	})
	return wv
}

func randomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
