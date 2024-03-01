package analyze

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igolaizola/musikai/pkg/ffmpeg"
	"github.com/igolaizola/musikai/pkg/sound"
)

type Config struct {
	Debug  bool
	Input  string
	Output string
}

func Run(ctx context.Context, cfg *Config) error {
	a, err := sound.NewAnalyzer(cfg.Input)
	if err != nil {
		return err
	}
	d, p := a.EndSilence()
	fmt.Printf("End silence: duration: %s, position %s\n", d, p)

	d, p = a.FirstSilence()
	fmt.Printf("First silence: duration: %s, position %s\n", d, p)

	name := filepath.Base(cfg.Input)
	name = strings.TrimSuffix(name, filepath.Ext(name))

	out := filepath.Join(cfg.Output, name)

	b, err := a.PlotRMS()
	if err != nil {
		return err
	}
	if err := os.WriteFile(out+"-rms.jpg", b, 0644); err != nil {
		return err
	}

	b, err = a.PlotWave()
	if err != nil {
		return err
	}
	if err := os.WriteFile(out+"-wave.jpg", b, 0644); err != nil {
		return err
	}

	ffm := ffmpeg.New("ffmpeg")
	input := cfg.Input
	if d > 0 {
		log.Println("cutting song...")
		out := strings.Replace(cfg.Input, ".mp3", "-cut.mp3", 1)
		p = p + 1*time.Second
		if err := ffm.Cut(ctx, cfg.Input, out, p); err != nil {
			return fmt.Errorf("couldn't cut song: %w", err)
		}
		a, err = sound.NewAnalyzer(out)
		if err != nil {
			return fmt.Errorf("couldn't create analyzer: %w", err)
		}
		input = out
	}
	if true /*!a.HasFadeOut() */ {
		out := strings.Replace(input, ".mp3", "-fadeout.mp3", 1)
		log.Println("fading out song...")
		pos := a.Duration() - 2*time.Second
		if err := ffm.FadeOut(ctx, input, out, pos); err != nil {
			return fmt.Errorf("couldn't fade out song: %w", err)
		}
	}
	return nil

}
