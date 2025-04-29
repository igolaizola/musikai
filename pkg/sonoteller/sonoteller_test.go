package sonoteller

import (
	"context"
	"testing"
)

func TestAnalyze(t *testing.T) {
	t.Skip("Only for manual testing")

	tests := []string{
		"W3q8Od5qJio",
		"TCGvZCbcE0Q",
	}

	c, err := New(&Config{Debug: true, Proxy: "http://your-proxy-url-here:port"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			if _, err := c.Analyze(context.Background(), tt); err != nil {
				t.Errorf("Analyze() error = %v", err)
			}
		})
	}
}
