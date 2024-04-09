package youtube

import (
	"context"
	"testing"
	"time"
)

func TestGetVideos(t *testing.T) {
	t.Skip("only for manual testing")
	ctx := context.Background()
	key := ""
	channelID := ""

	c, err := New(ctx, key, true)
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if _, err := c.GetVideos(ctx, channelID, time.Now().AddDate(0, 0, -15)); err != nil {
		t.Fatalf("GetVideos() = %v", err)
	}
}
