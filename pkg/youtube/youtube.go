package youtube

import (
	"context"
	"fmt"
	"html"
	"log"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

type Client struct {
	service *youtube.Service
	debug   bool
}

func New(ctx context.Context, key string, debug bool) (*Client, error) {
	service, err := youtube.NewService(ctx, option.WithAPIKey(key))
	if err != nil {
		return nil, fmt.Errorf("youtube: couldn't create service: %w", err)
	}
	return &Client{
		service: service,
		debug:   debug,
	}, nil
}

type Video struct {
	Title string
	ID    string
}

func (c *Client) GetVideos(ctx context.Context, channelID string, after time.Time) ([]Video, error) {
	// Prepare a search call
	call := c.service.Search.List([]string{"snippet"}).
		ChannelId(channelID).
		MaxResults(50).
		Type("video").
		Context(ctx)
	if !after.IsZero() {
		call = call.PublishedAfter(after.Format(time.RFC3339))
	}

	var videos []Video
	var pageToken string
	var page int
	for {
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		if c.debug {
			log.Println("youtube: fetching videos", page+1, channelID, pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("youtube: couldn't fetch videos: %w", err)
		}
		if c.debug {
			b, _ := resp.MarshalJSON()
			log.Println("youtube:", string(b))
		}

		for _, item := range resp.Items {
			title := html.UnescapeString(item.Snippet.Title)
			videos = append(videos, Video{
				Title: title,
				ID:    item.Id.VideoId,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
		page++
	}
	return videos, nil
}
