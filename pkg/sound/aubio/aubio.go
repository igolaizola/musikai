package aubio

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// BinPath is the path to the aubio binary
var BinPath = "aubio"

func Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, BinPath, "--version")
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

func BPM(ctx context.Context, input string) ([]float64, error) {
	cmd := exec.CommandContext(ctx, BinPath, "beat", input)
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

func Tempo(ctx context.Context, input string) (float64, error) {
	cmd := exec.CommandContext(ctx, BinPath, "tempo", input)
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

func Fragments(ctx context.Context, silence bool, input string, duration time.Duration, thresholdDB int, thresholdTime time.Duration) ([][2]time.Duration, error) {
	if thresholdDB == 0 {
		thresholdDB = -70
	}
	cmd := exec.CommandContext(ctx, BinPath, "quiet", "-i", input, "-s", fmt.Sprintf("%d", thresholdDB))
	data, err := cmd.Output()
	if err != nil {
		msg := string(data)
		return nil, fmt.Errorf("aubio: couldn't get silences: %w: %s", err, msg)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("aubio: no silences found")
	}
	if silence != isSilence(lines[0]) {
		lines = lines[1:]
	}

	var fragments [][2]time.Duration
	for i := 0; i < len(lines); i += 2 {
		t0, err := toTimestamp(lines[i])
		if err != nil {
			return nil, fmt.Errorf("aubio: couldn't parse entry: %w", err)
		}
		var t1 time.Duration
		if i+1 >= len(lines) {
			t1 = duration
		} else {
			t1, err = toTimestamp(lines[i+1])
			if err != nil {
				return nil, fmt.Errorf("aubio: couldn't parse entry: %w", err)
			}
		}
		if t1-t0 > thresholdTime {
			fragments = append(fragments, [2]time.Duration{t0, t1})
		}
	}
	return fragments, nil
}

func isSilence(line string) bool {
	return strings.HasPrefix(line, "QUIET: ")
}

func toTimestamp(line string) (time.Duration, error) {
	line = strings.TrimSpace(line)
	split := strings.Split(line, ": ")
	if len(split) != 2 {
		return 0, fmt.Errorf("invalid line: %s", line)
	}
	// Check entry type
	if split[0] != "QUIET" && split[0] != "NOISY" {
		return 0, fmt.Errorf("invalid type: %s", split[0])
	}

	// Parse timestamp.
	f, err := strconv.ParseFloat(split[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration: %w", err)
	}
	ts := time.Duration(f * float64(time.Second))
	return ts, nil
}
