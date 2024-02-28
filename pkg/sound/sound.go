package sound

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"math"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	mp3 "github.com/hajimehoshi/go-mp3"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

type Analyzer struct {
	stereo [2][]float64
	mono   []float64
	rate   int
}

func NewAnalyzer(u string) (*Analyzer, error) {
	decoder, err := toDecoder(u)
	if err != nil {
		return nil, fmt.Errorf("sound: couldn't create decoder: %w", err)
	}

	var stereo [2][]float64 // Assume stereo audio
	buf := make([]byte, 2)  // 2 bytes per sample for 16-bit audio
	var i int
	for {
		_, err := decoder.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("sound: couldn't read sample: %w", err)
		}
		// Convert bytes to 16-bit integer sample, assuming little endian
		sample := int16(buf[0]) | int16(buf[1])<<8
		// Normalize sample to float64 range -1.0 to 1.0
		normalized := float64(sample) / 32768.0
		stereo[i%2] = append(stereo[i%2], normalized)
		i++
	}

	// Convert to mono
	var mono []float64
	for i, left := range stereo[0] {
		right := stereo[1][i]
		mono = append(mono, (left+right)/2.0)
	}

	return &Analyzer{
		stereo: stereo,
		mono:   mono,
		rate:   decoder.SampleRate(),
	}, nil
}

func (a *Analyzer) Resample(windowSize time.Duration) []float64 {
	samples := a.mono
	windowLength := int(float64(a.rate) * windowSize.Seconds())

	var resampled []float64
	for i := 0; i < len(samples); i += windowLength {
		end := i + windowLength
		if end > len(samples) {
			end = len(samples)
		}
		window := samples[i:end]
		var min, max float64
		for _, v := range window {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		resampled = append(resampled, min)
		resampled = append(resampled, max)
	}
	return resampled
}

func (a *Analyzer) RMS(windowSize time.Duration) []float64 {
	samples := a.mono
	windowLength := int(float64(a.rate) * windowSize.Seconds())

	var rms []float64
	for i := 0; i < len(samples); i += windowLength {
		end := i + windowLength
		if end > len(samples) {
			end = len(samples)
		}
		window := samples[i:end]
		rms = append(rms, calculateRMS(window))
	}
	return rms
}

func calculateRMS(samples []float64) float64 {
	var squareSum float64
	for _, sample := range samples {
		squareSum += sample * sample
	}
	meanSquare := squareSum / float64(len(samples))
	return math.Sqrt(meanSquare)
}

const silenceThreshold = 0.002

func (a *Analyzer) EndSilence() (time.Duration, time.Duration) {
	rms := a.RMS(50 * time.Millisecond)
	// Reverse the slice
	slices.Reverse(rms)

	var duration time.Duration
	for i, v := range rms {
		if v < silenceThreshold {
			continue
		}
		duration = time.Duration(i) * 50 * time.Millisecond
		break
	}
	full := time.Duration(len(rms)) * 50 * time.Millisecond
	position := full - duration
	return duration, position
}

func (a *Analyzer) PlotRMS(output string) error {
	rms := a.RMS(50 * time.Millisecond)
	return createPlot("rms", output, rms, 0, 1, 0.01)
}

func (a *Analyzer) PlotWave(output string) error {
	resampled := a.Resample(50 * time.Millisecond)
	return createPlot("samples", output, resampled, -1, 1, 0.00)
}

func createPlot(name, output string, data []float64, min, max float64, line float64) error {
	// Create a new plot
	p := plot.New()

	// Set Y-axis limits
	p.Y.Min = min
	p.Y.Max = max

	p.Title.Text = name
	p.X.Label.Text = "time"
	p.Y.Label.Text = "data"

	// Make a line plotter and set its style.
	pts := make(plotter.XYs, len(data))
	for i, d := range data {
		pts[i].X = float64(i)
		pts[i].Y = float64(d)
	}
	l, err := plotter.NewLine(makePoints(data))
	if err != nil {
		return fmt.Errorf("sound: couldn't create line plotter: %w", err)
	}
	l.LineStyle.Width = vg.Points(1)

	// Add the line plotter to the plot
	p.Add(l)

	// Create a red line at y = N
	if line > 0 {
		hLine := plotter.NewFunction(func(x float64) float64 { return line })
		hLine.Color = color.RGBA{R: 255, A: 255} // Red color and fully opaque
		// Add the red line plotter to the plot
		p.Add(hLine)
	}

	// Save the plot to a PNG file.
	if err := p.Save(4*vg.Inch, 4*vg.Inch, output); err != nil {
		return fmt.Errorf("sound: couldn't save plot: %w", err)
	}
	return nil
}

// makePoints converts a slice of float32 to plotter.XYs
func makePoints(samples []float64) plotter.XYs {
	pts := make(plotter.XYs, len(samples))
	for i, v := range samples {
		pts[i].X = float64(i)
		pts[i].Y = float64(v)
	}
	return pts
}

func toDecoder(u string) (*mp3.Decoder, error) {
	var reader io.ReadCloser
	if strings.HasPrefix(u, "http") {
		// Download MP3 file
		client := &http.Client{
			Timeout: 2 * time.Minute,
		}
		resp, err := client.Get(u)
		if err != nil {
			return nil, fmt.Errorf("sound: couldn't download song: %w", err)
		}
		reader = resp.Body
	} else {
		// Open local file
		file, err := os.Open(u)
		if err != nil {
			return nil, fmt.Errorf("sound: couldn't open file: %w", err)
		}
		reader = file
	}
	b, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("sound: couldn't read song: %w", err)
	}
	defer reader.Close()

	// Decode MP3 to PCM
	decoder, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("sound: couldn't decode mp3: %w", err)
	}
	return decoder, nil
}
