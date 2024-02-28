package sound

import (
	"testing"
)

func TestFadeOut(t *testing.T) {
	tests := []struct {
		file string
		want bool
	}{
		{"data/not-finish.mp3", false},
		{"data/almost-finish.mp3", false},
		{"data/finish.mp3", true},
		{"data/finish-2.mp3", true},
		{"data/finish-3.mp3", true},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			a, err := NewAnalyzer(tt.file)
			if err != nil {
				t.Fatalf("NewAnalyzer(%q) err = %v; want nil", tt.file, err)
			}
			got := a.HasFadeOut()
			if got != tt.want {
				t.Fatalf("FadeOut(%q) = %v; want %v", tt.file, got, tt.want)
			}
		})
	}

}
