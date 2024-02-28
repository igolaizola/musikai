package analyze

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

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
	d, p := a.FirstSilence()
	fmt.Printf("First silence: duration: %s, position %s\n", d, p)

	d, p = a.EndSilence()
	fmt.Printf("End silence: duration: %s, position %s\n", d, p)

	name := filepath.Base(cfg.Input)
	name = strings.TrimSuffix(name, filepath.Ext(name))

	out := filepath.Join(cfg.Output, name)

	if err := a.PlotRMS(out + "-rms.png"); err != nil {
		return err
	}
	if err := a.PlotWave(out + "-wave.png"); err != nil {
		return err
	}

	return nil

}
