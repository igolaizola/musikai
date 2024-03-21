package sound

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	mp3 "github.com/hajimehoshi/go-mp3"
	"github.com/igolaizola/musikai/pkg/sound/aubio"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

type Analyzer struct {
	stereo   [2][]float64
	mono     []float64
	rate     int
	duration time.Duration
	source   string
}

func NewAnalyzer(u string) (*Analyzer, error) {
	decoder, src, err := toDecoder(u)
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

	duration := time.Duration(float64(len(mono)) / float64(decoder.SampleRate()) * float64(time.Second))
	return &Analyzer{
		source:   src,
		stereo:   stereo,
		mono:     mono,
		rate:     decoder.SampleRate(),
		duration: duration,
	}, nil
}

func (a *Analyzer) Source() string {
	return a.source
}

func (a *Analyzer) Duration() time.Duration {
	return a.duration
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

type Fragment struct {
	Start    time.Duration
	End      time.Duration
	Duration time.Duration
	Final    bool
}

func (a *Analyzer) Silences(ctx context.Context) ([]Fragment, error) {
	return a.fragments(ctx, true, 1*time.Second)
}

func (a *Analyzer) Noises(ctx context.Context) ([]Fragment, error) {
	return a.fragments(ctx, false, 10*time.Second)
}

func (a *Analyzer) fragments(ctx context.Context, silence bool, timeThreshold time.Duration) ([]Fragment, error) {
	ss, err := aubio.Fragments(ctx, silence, a.source, a.duration, -70, timeThreshold)
	if err != nil {
		return nil, fmt.Errorf("sound: couldn't get silences: %w", err)
	}
	var fragments []Fragment
	for _, s := range ss {
		fragments = append(fragments, Fragment{
			Start:    s[0],
			End:      s[1],
			Duration: s[1] - s[0],
			Final:    s[1] == a.duration,
		})
	}
	return fragments, nil
}

const bpmThreshold = 10.0

func (a *Analyzer) BPMChange(beats []float64, splits []float64) bool {
	bpms := a.BPMs(beats, splits)
	if len(bpms) < 2 {
		return false
	}
	var sum float64
	for _, bpm := range bpms {
		sum += bpm
	}
	avg := sum / float64(len(bpms))
	for _, bpm := range bpms {
		if math.Abs(bpm-avg) > bpmThreshold {
			return true
		}
	}
	return false
}

func (a *Analyzer) FragmentBPMChange(beats []float64, fragments []Fragment) bool {
	bpms := a.FragmentBPMs(beats, fragments)
	if len(bpms) < 2 {
		return false
	}
	var sum float64
	for _, bpm := range bpms {
		sum += bpm
	}
	avg := sum / float64(len(bpms))
	for _, bpm := range bpms {
		if math.Abs(bpm-avg) > bpmThreshold {
			return true
		}
	}
	return false
}

func (a *Analyzer) FragmentBPMs(beats []float64, fragments []Fragment) []float64 {
	bpms := make([]float64, len(fragments))
	for _, pos := range beats {
		for i, f := range fragments {
			if pos >= f.Start.Seconds() && pos < f.End.Seconds() {
				bpms[i]++
			}
		}
	}
	for i, v := range bpms {
		bpms[i] = v * 60.0 / fragments[i].Duration.Seconds()
	}
	return bpms
}

func (a *Analyzer) BPMs(beats []float64, splits []float64) []float64 {
	i := 0
	bpms := make([]float64, len(splits)+1)
	for _, pos := range beats {
		if i < len(splits) && pos >= splits[i] {
			i++
		}
		bpms[i]++
	}
	splits = append([]float64{0}, append(splits, a.duration.Seconds())...)
	for i, v := range bpms {
		bpms[i] = v * 60.0 / (splits[i+1] - splits[i])
	}
	return bpms
}

func (a *Analyzer) PlotRMS() ([]byte, error) {
	window := 50 * time.Millisecond
	rms := a.RMS(window)
	return createPlot("rms", rms, 0, 1, window.Seconds(), 0.01)
}

func (a *Analyzer) PlotWave(name string) ([]byte, error) {
	window := 50 * time.Millisecond
	resampled := a.Resample(window)
	return createPlot(name, resampled, -1, 1, window.Seconds(), 0.00)
}

func createPlot(name string, data []float64, min, max float64, window float64, line float64) ([]byte, error) {
	// Create a new plot
	p := plot.New()

	// Set Y-axis limits
	p.Y.Min = min
	p.Y.Max = max

	d := time.Duration(float64(len(data))*window*0.5) * time.Second
	p.Title.Text = fmt.Sprintf("%s %s", name, d)
	p.X.Label.Text = "time"
	p.Y.Label.Text = "data"

	// Make a line plotter and set its style.
	pts := make(plotter.XYs, len(data))
	for i, d := range data {
		pts[i].X = float64(i) * window
		pts[i].Y = float64(d)
	}
	l, err := plotter.NewLine(makePoints(data))
	if err != nil {
		return nil, fmt.Errorf("sound: couldn't create line plotter: %w", err)
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

	// Save the plot
	c, err := p.WriterTo(4*vg.Inch, 4*vg.Inch, "jpeg")
	if err != nil {
		return nil, fmt.Errorf("sound: couldn't create plot: %w", err)
	}
	var buf bytes.Buffer
	if _, err := c.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("sound: couldn't write plot: %w", err)
	}
	return buf.Bytes(), nil
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

func toDecoder(u string) (*mp3.Decoder, string, error) {
	src := u
	var b []byte
	if strings.HasPrefix(u, "http") {
		// Download MP3 file
		client := &http.Client{
			Timeout: 2 * time.Minute,
		}
		resp, err := client.Get(u)
		if err != nil {
			return nil, "", fmt.Errorf("sound: couldn't download song: %w", err)
		}
		defer resp.Body.Close()
		b, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("sound: couldn't read song: %w", err)
		}
		// Write to temporary file
		src = filepath.Join(os.TempDir(), filepath.Base(u))
		if err := os.WriteFile(src, b, 0644); err != nil {
			return nil, "", fmt.Errorf("sound: couldn't write song: %w", err)
		}
	} else {
		// Open local file
		file, err := os.Open(u)
		if err != nil {
			return nil, "", fmt.Errorf("sound: couldn't open file: %w", err)
		}
		defer file.Close()
		b, err = io.ReadAll(file)
		if err != nil {
			return nil, "", fmt.Errorf("sound: couldn't read song: %w", err)
		}
	}

	// Decode MP3 to PCM
	decoder, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		return nil, "", fmt.Errorf("sound: couldn't decode mp3: %w", err)
	}
	return decoder, src, nil
}
