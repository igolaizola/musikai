package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// BinPath is the path to the ffmpeg binary
var BinPath = "ffmpeg"

func FadeOut(ctx context.Context, input, output string, totalDuration, fadeOutDuration time.Duration) error {
	// Use a temporary file if the input and output are the same
	tmp := output
	if input == output {
		tmp = fmt.Sprintf("%s.tmp%s", input, filepath.Ext(input))
	}

	fd := fadeOutDuration.Seconds()
	st := totalDuration.Seconds() - fadeOutDuration.Seconds()
	cmd := exec.CommandContext(ctx, BinPath, "-y", "-i", input, "-b:a", "320k", "-af", fmt.Sprintf("afade=t=out:st=%f:d=%f", st, fd), tmp)
	data, err := cmd.CombinedOutput()
	if err != nil {
		if tmp != output {
			_ = os.Remove(tmp)
		}
		msg := string(data)
		return fmt.Errorf("ffmpeg: couldn't fade out: %w: %s", err, msg)
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

func Cut(ctx context.Context, input, output string, end time.Duration) error {
	// Use a temporary file if the input and output are the same
	tmp := output
	if input == output {
		tmp = fmt.Sprintf("%s.tmp%s", input, filepath.Ext(input))
	}

	cmd := exec.CommandContext(ctx, BinPath, "-y", "-i", input, "-b:a", "320k", "-to", toText(end), "-acodec", "copy", tmp)
	data, err := cmd.CombinedOutput()
	if err != nil {
		if tmp != output {
			_ = os.Remove(tmp)
		}
		msg := string(data)
		return fmt.Errorf("ffmpeg: couldn't cut: %w: %s", err, msg)
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

func Convert(ctx context.Context, input, output string) error {
	cmd := exec.CommandContext(ctx, BinPath, "-y", "-i", input, "-b:a", "320k", output)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return fmt.Errorf("ffmpeg: couldn't convert %s to %s: %w: %s", input, output, err, msg)
	}

	return nil
}

func StaticVideo(ctx context.Context, image, music, output string) error {
	// See https://superuser.com/questions/1041816/combine-one-image-one-audio-file-to-make-one-video-using-ffmpeg/1041820#1041820
	cmd := exec.CommandContext(ctx, BinPath, "-y", "-r", "1", "-loop", "1", "-i", image, "-i", music, "-acodec", "copy", "-r", "1", "-shortest", "-vf", "scale=1080:1080", output)
	data, err := cmd.CombinedOutput()
	if err != nil {
		msg := string(data)
		return fmt.Errorf("ffmpeg: couldn't create static video: %w: %s", err, msg)
	}
	return nil
}

func toText(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
