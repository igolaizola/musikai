package inkpic

import (
	"context"
	"fmt"
	"testing"
)

func TestText(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := New(ctx)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if err := c.AddText(ctx, "background.jpg", "./fonts/DIN-Bold.otf", "@IGOLAIZOLA'S TWEETS ARE THE BEST", "result.jpg"); err != nil {
		t.Fatalf("AddText() failed: %v", err)
	}
	for i := 0; i < 10; i++ {
		if err := c.AddText(ctx, "background.jpg", "./fonts/Inter-Bold.ttf", "@IGOLAIZOLA'S TWEETS ARE THE BEST", fmt.Sprintf("result%d.jpg", i)); err != nil {
			t.Fatalf("AddText() failed: %v", err)
		}
	}
}
