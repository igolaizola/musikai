package ffmpeg

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type ffmpeg struct {
	bin string
}

func New(bin string) *ffmpeg {
	return &ffmpeg{bin: bin}
}

func (f *ffmpeg) FadeOut(input, output string, duration time.Duration) error {
	cmd := exec.Command(f.bin, "-i", input, "-af", "afade=t=out:st=0:d="+toText(duration), output)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return fmt.Errorf("ffmpeg: couldn't fade out: %w: %s", err, msg)
	}
	return nil
}

func (f *ffmpeg) Cut(ctx context.Context, input, output string, start, duration time.Duration) error {
	cmd := exec.CommandContext(ctx, f.bin, "-i", input, "-ss", toText(start), "-t", toText(duration), output)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return fmt.Errorf("ffmpeg: couldn't cut: %w: %s", err, msg)
	}
	return nil
}

func toText(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
