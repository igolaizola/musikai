package ngrok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// BinPath is the path to the ngrok binary
var BinPath = "ngrok"

type tunnelsResponse struct {
	Tunnels []struct {
		Name      string `json:"name"`
		ID        string `json:"id"`
		PublicURL string `json:"public_url"`
		Proto     string `json:"proto"`
		Config    struct {
			Addr string `json:"addr"`
		} `json:"config"`
	} `json:"tunnels"`
}

func Run(ctx context.Context, protocol, port string) (string, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(ctx)

	// Launch ngrok
	cmd := exec.CommandContext(ctx, BinPath, protocol, port)

	// Start the command
	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, fmt.Errorf("ngrok: couldn't start: %w", err)
	}

	// Get the public URL
	client := &http.Client{
		Timeout: 2 * time.Minute,
	}
	var err error
	var resp *http.Response
	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			cancel()
			return "", nil, fmt.Errorf("ngrok: context cancelled")
		case <-time.After(500 * time.Millisecond):
		}
		resp, err = client.Get("http://localhost:4040/api/tunnels")
		if err == nil {
			break
		}
	}
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("ngrok: couldn't start: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("ngrok: couldn't read response: %w", err)
	}
	var tr tunnelsResponse
	if err := json.Unmarshal(data, &tr); err != nil {
		cancel()
		return "", nil, fmt.Errorf("ngrok: couldn't unmarshal response (%s): %w", string(data), err)
	}
	var u string
	for _, t := range tr.Tunnels {
		p := strings.Split(t.Config.Addr, ":")[1]
		if p != port {
			continue
		}
		u = strings.Replace(t.PublicURL, "tcp://", "http://", 1)
		break
	}
	if u == "" {
		cancel()
		return "", nil, fmt.Errorf("ngrok: couldn't find tunnel")
	}
	return u, cancel, nil
}
