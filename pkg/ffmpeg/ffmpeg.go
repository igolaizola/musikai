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

func (f *ffmpeg) FadeOut(ctx context.Context, input, output string, duration time.Duration) error {
	cmd := exec.CommandContext(ctx, f.bin, "-y", "-i", input, "-af", fmt.Sprintf("afade=t=out:st=0:d=%d", +int(duration.Seconds())), output)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return fmt.Errorf("ffmpeg: couldn't fade out: %w: %s", err, msg)
	}
	return nil
}

func (f *ffmpeg) Cut(ctx context.Context, input, output string, end time.Duration) error {
	cmd := exec.CommandContext(ctx, f.bin, "-y", "-i", input, "-to", toText(end), "-acodec", "copy", output)
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
