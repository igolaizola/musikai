package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/igolaizola/musikai/pkg/sound"
	"github.com/igolaizola/musikai/pkg/sound/aubio"
	"github.com/igolaizola/musikai/pkg/sound/ffmpeg"
	"github.com/igolaizola/musikai/pkg/sound/phaselimiter"
)

type Config struct {
	Debug      bool
	Input      string
	Output     string
	SkipMaster bool
}

type flags struct {
	Silences []int `json:"silences,omitempty"`
	NoEnd    bool  `json:"no_end,omitempty"`
	Short    bool  `json:"short,omitempty"`
	BPM2     bool  `json:"bpm_2,omitempty"`
	BPM4     bool  `json:"bpm_4,omitempty"`
	BPMN     bool  `json:"bpm_n,omitempty"`
}

func Run(ctx context.Context, cfg *Config) error {
	name := filepath.Base(cfg.Input)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	output := cfg.Output
	if output == "" && !strings.HasPrefix(cfg.Input, "http") {
		output = filepath.Dir(cfg.Input)
	}
	out := filepath.Join(output, name)

	input := cfg.Input
	processed := out + "-processed.mp3"

	// Master the song
	if !cfg.SkipMaster {
		ph := phaselimiter.New(&phaselimiter.Config{})
		if err := ph.Master(ctx, cfg.Input, processed); err != nil {
			return err
		}
		input = processed
	}

	// Analyze the song
	analyzer, err := sound.NewAnalyzer(input)
	if err != nil {
		return err
	}

	// If master was skipped, copy the original file to the processed file
	if cfg.SkipMaster {
		if err := copyFile(analyzer.Source(), processed); err != nil {
			return fmt.Errorf("process: couldn't copy original file: %w", err)
		}
	}

	fmt.Println("Duration:", analyzer.Duration(), analyzer.Duration().Seconds())

	silences, err := analyzer.Silences(ctx)
	if err != nil {
		return err
	}
	for _, s := range silences {
		fmt.Printf("Silence: (%s, %s) duration %s, final %v\n", s.Start, s.End, s.Duration, s.Final)
	}

	fadeOut := 6 * time.Second
	noEnd := true
	// Remove last silence
	if len(silences) > 0 {
		last := silences[len(silences)-1]
		if last.Final || last.End > analyzer.Duration()-10*time.Second {
			// Cut the last silence
			if err := ffmpeg.Cut(ctx, processed, processed, last.Start); err != nil {
				return fmt.Errorf("process: couldn't cut last silence: %w", err)
			}
		}
		fadeOut = 1 * time.Second
		noEnd = false
	}

	// Apply fade out
	if err := ffmpeg.FadeOut(ctx, processed, processed, analyzer.Duration(), fadeOut); err != nil {
		return fmt.Errorf("process: couldn't fade out song: %w", err)
	}

	analyzer, err = sound.NewAnalyzer(processed)
	if err != nil {
		return err
	}

	silences, err = analyzer.Silences(ctx)
	if err != nil {
		return err
	}
	for _, s := range silences {
		fmt.Printf("Silence: (%s, %s) duration %s, final %v\n", s.Start, s.End, s.Duration, s.Final)
	}

	// process the wave image
	waveBytes, err := analyzer.PlotWave("wave")
	if err != nil {
		return fmt.Errorf("process: couldn't plot wave: %w", err)
	}
	if err := os.WriteFile(out+"-wave.jpg", waveBytes, 0644); err != nil {
		return fmt.Errorf("process: couldn't write wave image: %w", err)
	}

	// Get the tempo
	tempo, err := aubio.Tempo(ctx, processed)
	if err != nil {
		return fmt.Errorf("process: couldn't get tempo: %w", err)
	}
	fmt.Println("Tempo:", tempo)

	// Detect flags
	f := flags{
		NoEnd: noEnd,
	}
	for _, s := range silences {
		if s.Final {
			break
		}
		p := (s.Start.Seconds() + s.Duration.Seconds()/2.0) / analyzer.Duration().Seconds()
		p100 := int(p * 100.0)
		f.Silences = append(f.Silences, p100)
	}

	// Short song
	if analyzer.Duration() < 2*time.Minute {
		f.Short = true
	}

	// BPM changes
	beats, err := aubio.BPM(ctx, processed)
	if err != nil {
		return fmt.Errorf("process: couldn't get bpm: %w", err)
	}

	f.BPM2 = analyzer.BPMChange(beats, []float64{analyzer.Duration().Seconds() / 2.0})

	q := analyzer.Duration().Seconds() / 4.0
	f.BPM4 = analyzer.BPMChange(beats, []float64{1 * q, 2 * q, 3 * q})

	noises, err := analyzer.Noises(ctx)
	if err != nil {
		return fmt.Errorf("process: couldn't get noises: %w", err)
	}
	f.BPMN = analyzer.FragmentBPMChange(beats, noises)

	flagsBytes, _ := json.MarshalIndent(f, "", "  ")

	fmt.Println(string(flagsBytes))

	return nil
}

func copyFile(src, dst string) error {
	// Open the source file for reading
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get the file information of the source file to obtain its permissions
	srcFileInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	// Open the destination file for writing. If it does not exist, create it with
	// the same permissions as the source file. If it exists, truncate it.
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcFileInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy the content from the source file to the destination file
	_, err = io.Copy(dstFile, srcFile)
	return err
}
