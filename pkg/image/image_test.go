package image

import (
	"fmt"
	"testing"
)

func TestAddText(t *testing.T) {
	t.Skip("skipping test")
	tests := []struct {
		name     string
		position Position
	}{
		{"TopLeft", TopLeft},
		{"TopRight", TopRight},
		{"BottomLeft", BottomLeft},
		{"BottomRight", BottomRight},
		{"TopCenter", TopCenter},
		{"BottomCenter", BottomCenter},
		{"Center", Center},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := fmt.Sprintf("output-%s.png", tt.name)
			if err := AddText("Dreams Collection: Vol 1", tt.position, "Hind-Bold.ttf", "cover.png", output); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAddOverlay(t *testing.T) {
	t.Skip("skipping test")
	tests := []struct {
		name string
	}{
		{"overlay1.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := fmt.Sprintf("output-%s.png", tt.name)
			if err := AddOverlay(tt.name, "cover.png", output); err != nil {
				t.Fatal(err)
			}
		})
	}
}
