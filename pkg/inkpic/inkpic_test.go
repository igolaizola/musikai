package inkpic

import (
	"context"
	"fmt"
	"testing"
)

func TestText(t *testing.T) {
	t.Skip("Only for manual testing")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := New(ctx)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	for i := 0; i < 10; i++ {
		if err := c.AddText(ctx, "../../cfg/bg.jpg", "GENERATE MUSIC WITH MUSIKAI", "../../cfg/_fonts/DIN-Bold.otf", "9vw", fmt.Sprintf("result%d.jpg", i)); err != nil {
			t.Fatalf("AddText() failed: %v", err)
		}
	}
}
