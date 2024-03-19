package upscale

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Upscaler struct {
	cmd             func(context.Context, string, string) *exec.Cmd
	outputExtension string
	timeout         time.Duration
}

func New(upscalerType, bin string) (*Upscaler, error) {
	var upscaler Upscaler
	upscaler.timeout = time.Minute
	switch upscalerType {
	case "realesrgan":
		upscaler.outputExtension = "jpeg"
		upscaler.cmd = func(ctx context.Context, file, outDir string) *exec.Cmd {
			output := toExtension(filepath.Join(outDir, filepath.Base(file)), upscaler.outputExtension)
			return exec.CommandContext(ctx, bin, "-i", file, "-o", output, "-s", "4", "-n", "realesrgan-x4plus")
		}
	case "topaz":
		switch runtime.GOOS {
		case "windows":
			if bin == "" {
				bin = `C:\Program Files\Topaz Labs LLC\Topaz Photo AI\tpai.exe`
			}
			upscaler.cmd = func(ctx context.Context, file, outDir string) *exec.Cmd {
				return exec.CommandContext(ctx, bin, file, "--output", outDir, "--format", upscaler.outputExtension, "--quality", "100")
			}
		case "darwin":
			if bin == "" {
				bin = "/Applications/Topaz Photo AI.app/Contents/MacOS/Topaz Photo AI"
			}
			upscaler.cmd = func(ctx context.Context, file, outDir string) *exec.Cmd {
				return exec.CommandContext(ctx, bin, "--cli", file, "-o", outDir, "--format", upscaler.outputExtension, "--quality", "100")
			}
		default:
			return nil, fmt.Errorf("upscale: not supported OS: %s", runtime.GOOS)
		}
		upscaler.outputExtension = "jpeg"
	default:
		return nil, fmt.Errorf("upscale: unknown upscaler type: %s", upscalerType)
	}
	return &upscaler, nil
}

func (u *Upscaler) Upscale(ctx context.Context, file, outDir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, u.timeout)
	defer cancel()
	cmd := u.cmd(ctx, file, outDir)
	data, err := cmd.CombinedOutput()
	if err != nil {
		// If the error is too long, truncate it
		msg := string(data)
		msg = strings.ReplaceAll(msg, "https://", "")
		msg = strings.ReplaceAll(msg, ".com", "")
		if len(msg) > 200 {
			msg = msg[len(msg)-200:]
		}
		return "", fmt.Errorf("upscale: couldn't upscale the image (%s): %w: %s", file, err, msg)
	}
	return toExtension(filepath.Join(outDir, filepath.Base(file)), u.outputExtension), nil
}

func toExtension(file string, ext string) string {
	base := filepath.Base(file)
	new := fmt.Sprintf("%s.%s", base[:len(base)-len(filepath.Ext(base))], ext)
	return filepath.Join(filepath.Dir(file), new)
}
