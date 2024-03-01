package aubio

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type App struct {
	bin string
}

func New(bin string) *App {
	return &App{bin: bin}
}

func (a *App) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, a.bin, "--version")
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return "", fmt.Errorf("aubio: couldn't get version: %w: %s", err, msg)
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "aubio version") {
		return "", fmt.Errorf("aubio: invalid version: %s", line)
	}
	version := strings.TrimPrefix(line, "aubio version ")
	return version, nil
}

func (a *App) BPM(ctx context.Context, input string) ([]float64, error) {
	cmd := exec.CommandContext(ctx, a.bin, "beat", input)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return nil, fmt.Errorf("aubio: couldn't get bpm: %w: %s", err, msg)
	}
	var bpm []float64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		b, err := strconv.ParseFloat(line, 64)
		if err != nil {
			continue
		}
		bpm = append(bpm, b)
	}
	if len(bpm) == 0 {
		return nil, fmt.Errorf("aubio: no bpm found")
	}
	return bpm, nil
}

func (a *App) Tempo(ctx context.Context, input string) (float64, error) {
	cmd := exec.CommandContext(ctx, a.bin, "tempo", input)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return 0, fmt.Errorf("aubio: couldn't get tempo: %w: %s", err, msg)
	}
	tempo := -1.0
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasSuffix(line, " bpm") {
			continue
		}
		line = strings.TrimSuffix(line, " bpm")
		t, err := strconv.ParseFloat(line, 64)
		if err != nil {
			continue
		}
		tempo = t
		break
	}
	if tempo < 0 {
		return 0, fmt.Errorf("aubio: no tempo found")
	}
	return tempo, nil
}
