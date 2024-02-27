package suno

import "testing"

func TestBestClip(t *testing.T) {
	tests := []struct {
		clips       []clip
		wantID      string
		wantFadeOut bool
	}{
		{
			clips: []clip{
				{
					ID: "1",
					Metadata: metadata{
						Duration: 35.0,
					},
				},
				{
					ID:       "2",
					AudioURL: "f",
					Metadata: metadata{
						Duration: 30.0,
					},
				},
			},
			wantID:      "2",
			wantFadeOut: true,
		},
		{
			clips: []clip{
				{
					ID:       "1",
					AudioURL: "f",
					Metadata: metadata{
						Duration: 30.0,
					},
				},
				{
					ID:       "2",
					AudioURL: "f",
					Metadata: metadata{
						Duration: 35.0,
					},
				},
			},
			wantID:      "2",
			wantFadeOut: true,
		},
		{
			clips: []clip{
				{
					ID: "1",
					Metadata: metadata{
						Duration: 30.0,
					},
				},
				{
					ID: "2",
					Metadata: metadata{
						Duration: 35.0,
					},
				},
			},
			wantID:      "2",
			wantFadeOut: false,
		},
	}

	fadeOut := func(c clip) (bool, error) {
		return c.AudioURL == "f", nil
	}
	for _, tt := range tests {
		t.Run(tt.clips[0].ID, func(t *testing.T) {
			got, gotFadeOut, err := bestClip(tt.clips, 2.00, fadeOut)
			if err != nil {
				t.Fatalf("bestClip() err = %v; want nil", err)
			}
			if got.ID != tt.wantID {
				t.Fatalf("bestClip() = %v; want %v", got.ID, tt.wantID)
			}
			if gotFadeOut != tt.wantFadeOut {
				t.Fatalf("bestClip() = %v; want %v", gotFadeOut, tt.wantFadeOut)
			}
		})
	}
}
