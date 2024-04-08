package sonoteller

import (
	"context"
	"testing"
)

func TestAnalyze(t *testing.T) {
	tests := []string{
		"https://www.youtube.com/watch?v=W3q8Od5qJio",
		"https://www.youtube.com/watch?v=TCGvZCbcE0Q",
	}

	c := New(&Config{Debug: true})
	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			if _, err := c.Analyze(context.Background(), tt); err != nil {
				t.Errorf("Analyze() error = %v", err)
			}
		})
	}
}
