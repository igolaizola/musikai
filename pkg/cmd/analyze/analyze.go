package analyze

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igolaizola/musikai/pkg/sound"
	"github.com/igolaizola/musikai/pkg/sound/aubio"
	"github.com/igolaizola/musikai/pkg/sound/ffmpeg"
)

type Config struct {
	Debug  bool
	Input  string
	Output string
}

func Run(ctx context.Context, cfg *Config) error {
	tempo, err := aubio.Tempo(ctx, cfg.Input)
	if err != nil {
		return err
	}
	fmt.Println("Tempo:", tempo)

	a, err := sound.NewAnalyzer(cfg.Input)
	if err != nil {
		return err
	}

	silences, err := a.Silences(ctx)
	if err != nil {
		return err
	}
	for _, s := range silences {
		fmt.Printf("Silence: (%s, %s) duration %s, final %v\n", s.Start, s.End, s.Duration, s.Final)
	}

	noises, err := a.Noises(ctx)
	if err != nil {
		return err
	}
	for _, s := range noises {
		fmt.Printf("Noise: (%s, %s) duration %s, final %v\n", s.Start, s.End, s.Duration, s.Final)
	}

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

	input := cfg.Input

	lastSilence := silences[len(silences)-1]

	if lastSilence.Final {
		p := lastSilence.Start
		log.Println("cutting song...")
		out := strings.Replace(cfg.Input, ".mp3", "-cut.mp3", 1)
		p = p + 1*time.Second
		if err := ffmpeg.Cut(ctx, cfg.Input, out, p); err != nil {
			return fmt.Errorf("couldn't cut song: %w", err)
		}
	} else {
		out := strings.Replace(input, ".mp3", "-fadeout.mp3", 1)
		log.Println("fading out song...")
		pos := a.Duration() - 5*time.Second
		if err := ffmpeg.FadeOut(ctx, input, out, a.Duration(), pos); err != nil {
			return fmt.Errorf("couldn't fade out song: %w", err)
		}
	}
	return nil
}
