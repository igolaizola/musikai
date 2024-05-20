package phaselimiter

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/igolaizola/musikai/pkg/sound/ffmpeg"
)

type Config struct {
	SoundQuality2Cache   string
	Loudness             *float64
	Level                *float64
	SkipBassPreservation bool
	Docker               bool
}

type PhaseLimiter struct {
	soundQuality2Cache string
	loudness           float64
	level              float64
	bassPreservation   bool
	docker             bool
}

// BinPath is the path to the phaselimiter binary
var BinPath = "phase_limiter"

// New returns a new PhaseLimiter
func New(cfg *Config) *PhaseLimiter {
	soundQuality2Cache := "/etc/phaselimiter/resource/sound_quality2_cache"
	if cfg.SoundQuality2Cache != "" {
		soundQuality2Cache = cfg.SoundQuality2Cache
	}
	loudness := -9.0
	if cfg.Loudness != nil {
		loudness = *cfg.Loudness
	}
	level := 1.0
	if cfg.Level != nil {
		level = *cfg.Level
	}
	bassPreservation := true
	if cfg.SkipBassPreservation {
		bassPreservation = false
	}

	return &PhaseLimiter{
		soundQuality2Cache: soundQuality2Cache,
		loudness:           loudness,
		level:              level,
		bassPreservation:   bassPreservation,
		docker:             cfg.Docker,
	}
}

func (p *PhaseLimiter) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, BinPath, "--version")
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return "", fmt.Errorf("phaselimiter: couldn't get version: %w: %s", err, msg)
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "phase_limiter version") {
		return "", fmt.Errorf("phaselimiter: invalid version: %s", line)
	}
	version := strings.TrimPrefix(line, "phase_limiter version ")
	return version, nil
}

func (p *PhaseLimiter) Master(ctx context.Context, input string, output string) error {
	wav := fmt.Sprintf("%s.wav", output)
	var bin string
	var args []string
	if p.docker {
		bin = "docker"
		args = []string{
			"run",
			"--rm",
			"-v", fmt.Sprintf("%s:/out", filepath.Dir(output)),
			"-v", fmt.Sprintf("%s:/in", filepath.Dir(input)),
			"igolaizola/phaselimiter:v1.0.0",
			filepath.Join("/in", filepath.Base(input)),
			filepath.Join("/out", filepath.Base(wav)),
		}
	} else {
		bin = BinPath
		args = []string{
			"--input", input,
			"--output", wav,
			"--ffmpeg", ffmpeg.BinPath,
			"--mastering", "true",
			"--mastering_mode", "mastering5",
			"--sound_quality2_cache", p.soundQuality2Cache,
			"--mastering_matching_level", formatFloat(p.level),
			"--mastering_ms_matching_level", formatFloat(p.level),
			"--mastering5_mastering_level", formatFloat(p.level),
			"--erb_eval_func_weighting", formatBool(p.bassPreservation),
			"--reference", formatFloat(p.loudness),
		}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return fmt.Errorf("phaselimiter: couldn't master: %w: %s", err, msg)
	}

	// Use a temporary file if the input and output are the same
	tmp := output
	if input == output {
		tmp = fmt.Sprintf("%s.tmp%s", input, filepath.Ext(input))
	}
	// Encode the wav file to mp3
	if err := encode(ctx, wav, tmp); err != nil {
		return fmt.Errorf("phaselimiter: couldn't encode: %w", err)
	}
	// Move the temporary file to the output path
	if tmp != output {
		_ = os.Remove(output)
		if err := os.Rename(tmp, output); err != nil {
			return fmt.Errorf("ffmpeg: couldn't rename temporary file: %w", err)
		}
	}
	return nil
}

func formatFloat(x float64) string {
	return strconv.FormatFloat(x, 'f', 7, 64)
}
func formatBool(x bool) string {
	if x {
		return "true"
	}
	return "false"
}

func encode(ctx context.Context, input, output string) error {
	if ext := filepath.Ext(input); ext != ".wav" {
		return fmt.Errorf("ffmpeg: input file must be a wav file: %s", ext)
	}
	if ext := filepath.Ext(output); ext != ".mp3" {
		return fmt.Errorf("ffmpeg: output file must be a mp3 file: %s", ext)
	}
	cmd := exec.CommandContext(ctx, ffmpeg.BinPath, "-y", "-i", input, "-codec:a", "libmp3lame", "-b:a", "320k", "-ac", "2", output)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return fmt.Errorf("ffmpeg: couldn't encode: %w: %s", err, msg)
	}
	return nil
}
