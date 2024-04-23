package spotify

import (
	"context"
	"fmt"
	"net/url"

	"github.com/zmb3/spotify/v2"
)

func (c *Client) SearchTrack(ctx context.Context, query string) ([]string, error) {
	if err := c.Auth(ctx); err != nil {
		return nil, err
	}
	var resp spotify.SearchResult
	u := "search?q=" + url.QueryEscape(query) + "&type=track"
	if _, err := c.do(ctx, "GET", u, nil, &resp); err != nil {
		return nil, fmt.Errorf("spotify: couldn't search track: %w", err)
	}
	var tracks []string
	for _, t := range resp.Tracks.Tracks {
		tracks = append(tracks, t.Name)
	}
	return tracks, nil
}

type Track struct {
	Name   string
	ID     string
	Number int
}

func (c *Client) AlbumTracks(ctx context.Context, id string) ([]Track, error) {
	if id == "" {
		return nil, fmt.Errorf("spotify: empty album id")
	}
	if err := c.Auth(ctx); err != nil {
		return nil, err
	}
	u := "albums/" + id + "/tracks"
	var resp spotify.SimpleTrackPage
	if _, err := c.do(ctx, "GET", u, nil, &resp); err != nil {
		return nil, fmt.Errorf("spotify: couldn't get album tracks: %w", err)
	}
	var tracks []Track
	for _, t := range resp.Tracks {
		tracks = append(tracks, Track{
			Name:   t.Name,
			ID:     t.ID.String(),
			Number: t.TrackNumber,
		})
	}
	return tracks, nil
}

// Analysis contains various high-level acoustic attributes
// for a particular track.
type Analysis struct {
	// Acousticness is a confidence measure from 0.0 to 1.0 of whether
	// the track is acoustic.  A value of 1.0 represents high confidence
	// that the track is acoustic.
	Acousticness float32 `json:"acousticness"`
	// Danceability describes how suitable a track is for dancing based
	// on a combination of musical elements including tempo, rhythm stability,
	// beat strength, and overall regularity.  A value of 0.0 is least danceable
	// and 1.0 is most danceable.
	Danceability float32 `json:"danceability"`
	// The length of the track in milliseconds.
	Duration int `json:"duration_ms"`
	// Energy is a measure from 0.0 to 1.0 and represents a perceptual measure
	// of intensity and activity.  Typically, energetic tracks feel fast, loud,
	// and noisy.
	Energy float32 `json:"energy"`
	// Predicts whether a track contains no vocals.  "Ooh" and "aah" sounds are
	// treated as instrumental in this context.  Rap or spoken words are clearly
	// "vocal".  The closer the Instrumentalness value is to 1.0, the greater
	// likelihood the track contains no vocal content.  Values above 0.5 are
	// intended to represent instrumental tracks, but confidence is higher as the
	// value approaches 1.0.
	Instrumentalness float32 `json:"instrumentalness"`
	// The key the track is in.  Integers map to pitches using standard Pitch Class notation
	// (https://en.wikipedia.org/wiki/Pitch_class).
	Key int `json:"key"`
	// Detects the presence of an audience in the recording.  Higher liveness
	// values represent an increased probability that the track was performed live.
	// A value above 0.8 provides strong likelihood that the track is live.
	Liveness float32 `json:"liveness"`
	// The overall loudness of a track in decibels (dB).  Loudness values are
	// averaged across the entire track and are useful for comparing the relative
	// loudness of tracks.  Typical values range between -60 and 0 dB.
	Loudness float32 `json:"loudness"`
	// Mode indicates the modality (major or minor) of a track.
	Mode int `json:"mode"`
	// Detects the presence of spoken words in a track.  The more exclusively
	// speech-like the recording, the closer to 1.0 the speechiness will be.
	// Values above 0.66 describe tracks that are probably made entirely of
	// spoken words.  Values between 0.33 and 0.66 describe tracks that may
	// contain both music and speech, including such cases as rap music.
	// Values below 0.33 most likely represent music and other non-speech-like tracks.
	Speechiness float32 `json:"speechiness"`
	// The overall estimated tempo of the track in beats per minute (BPM).
	Tempo float32 `json:"tempo"`
	// An estimated overall time signature of a track.  The time signature (meter)
	// is a notational convention to specify how many beats are in each bar (or measure).
	TimeSignature int `json:"time_signature"`
	// A measure from 0.0 to 1.0 describing the musical positiveness conveyed
	// by a track.  Tracks with high valence sound more positive (e.g. happy,
	// cheerful, euphoric), while tracks with low valence sound more negative
	// (e.g. sad, depressed, angry).
	Valence float32 `json:"valence"`
}

func (c *Client) AudioFeatures(ctx context.Context, id string) (*Analysis, error) {
	if id == "" {
		return nil, fmt.Errorf("spotify: empty track id")
	}
	if err := c.Auth(ctx); err != nil {
		return nil, err
	}
	u := "audio-features/" + id
	var resp spotify.AudioFeatures
	if _, err := c.do(ctx, "GET", u, nil, &resp); err != nil {
		return nil, fmt.Errorf("spotify: couldn't get track analysis: %w", err)
	}
	return &Analysis{
		Acousticness:     resp.Acousticness,
		Danceability:     resp.Danceability,
		Duration:         resp.Duration,
		Energy:           resp.Energy,
		Instrumentalness: resp.Instrumentalness,
		Key:              resp.Key,
		Liveness:         resp.Liveness,
		Loudness:         resp.Loudness,
		Mode:             resp.Mode,
		Speechiness:      resp.Speechiness,
		Tempo:            resp.Tempo,
		TimeSignature:    resp.TimeSignature,
		Valence:          resp.Valence,
	}, nil
}
