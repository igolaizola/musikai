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
