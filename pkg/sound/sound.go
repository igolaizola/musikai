package sound

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	mp3 "github.com/hajimehoshi/go-mp3"
)

// Calculate RMS of a slice of samples.
func calculateRMS(samples []float64) float64 {
	var squareSum float64
	for _, sample := range samples {
		squareSum += sample * sample
	}
	meanSquare := squareSum / float64(len(samples))
	return math.Sqrt(meanSquare)
}

func FadeOut(f string) (bool, error) {
	fmt.Println(f)
	var reader io.ReadCloser
	if strings.HasPrefix(f, "http") {
		// Download MP3 file
		client := &http.Client{
			Timeout: 2 * time.Minute,
		}
		resp, err := client.Get(f)
		if err != nil {
			return false, fmt.Errorf("sound: couldn't download song: %w", err)
		}
		reader = resp.Body
	} else {
		// Open local file
		file, err := os.Open(f)
		if err != nil {
			return false, fmt.Errorf("sound: couldn't open file: %w", err)
		}
		reader = file
	}
	defer reader.Close()

	// Decode MP3 to PCM
	decoder, err := mp3.NewDecoder(reader)
	if err != nil {
		panic(err)
	}

	var samples []float64
	buf := make([]byte, 2) // 2 bytes per sample for 16-bit audio
	for {
		_, err := decoder.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, fmt.Errorf("sound: couldn't read sample: %w", err)
		}
		// Convert bytes to 16-bit integer sample, assuming little endian
		sample := int16(buf[0]) | int16(buf[1])<<8
		// Normalize sample to float64 range -1.0 to 1.0
		samples = append(samples, float64(sample)/32768.0)
	}

	// Analyze the last N samples for a fade-out
	return detectFadeOut(samples), nil
}

// detectFadeOut checks for a fade out in the last portion of the samples.
// It divides this portion into windows, calculates the RMS for each,
// and checks for a consistent decrease.
func detectFadeOut(samples []float64) bool {
	const (
		endFraction = 0.10 // Analyze the last 10% of the samples
		numWindows  = 10   // Divide this segment into 10 windows
	)

	// Calculate the start index for the end portion
	startIndex := len(samples) - int(float64(len(samples))*endFraction)
	if startIndex < 0 {
		startIndex = 0
	}

	// Length of each window
	windowLength := (len(samples) - startIndex) / numWindows
	if windowLength == 0 {
		return false // Not enough samples to analyze
	}

	// Calculate RMS values for each window
	rmsValues := make([]float64, numWindows)
	for i := 0; i < numWindows; i++ {
		windowStart := startIndex + i*windowLength
		windowEnd := windowStart + windowLength
		if i == numWindows-1 {
			// Ensure the last window includes any remaining samples
			windowEnd = len(samples)
		}
		windowSamples := samples[windowStart:windowEnd]
		rmsValues[i] = calculateRMS(windowSamples)
	}

	// Check for a consistent decrease in RMS values
	var count int
	for i := 1; i < len(rmsValues); i++ {
		if rmsValues[i] > rmsValues[i-1] {
			// If any window is louder than the previous, it's not a consistent fade out
			count++
		}
	}
	return count <= 1
}
