package ngrok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	go func() {
		cmd := exec.CommandContext(ctx, BinPath, protocol, port)
		data, err := cmd.CombinedOutput()
		if err != nil {
			msg := string(data)
			log.Println(fmt.Errorf("ngrok: %w: %s", err, msg))
		}
	}()
	client := &http.Client{
		Timeout: 2 * time.Minute,
	}
	resp, err := client.Get("http://localhost:4040")
	if err != nil {
		cancel()
		return "", nil, fmt.Errorf("ngrok: couldn't start: %w", err)
	}
	resp.Body.Close()
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
		u = strings.Replace("tcp://", "http://", t.PublicURL, 1)
		break
	}
	return u, cancel, nil
}
