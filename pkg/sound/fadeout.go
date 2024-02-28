package sound

import (
	"log"
	"math"
	"time"
)

func (a *Analyzer) HasFadeOut() bool {
	rmsWindow := 100 * time.Millisecond
	fadeOutWindow := 1 * time.Second
	analysisWindow := int(fadeOutWindow.Seconds() / rmsWindow.Seconds())
	rms := a.RMS(50 * time.Millisecond)

	rms = rms[len(rms)-analysisWindow:]

	// Check for a consistent decrease in RMS values
	var count int
	for i := 1; i < len(rms); i++ {
		// Calculate the increment
		inc := rms[i] - rms[i-1]
		if inc < 0 && inc*-1.0 > 0.001 {
			// If any window is louder than the previous, it's not a consistent fade out
			count++
		}
	}
	return count <= 1
}

// DetectFadeOut uses linear regression on the last N RMS values to detect a fade-out.
func (a *Analyzer) HasFadeOutLR() bool {
	rmsWindow := 500 * time.Millisecond
	fadeOutWindow := 3 * time.Second
	analysisWindow := int(fadeOutWindow.Seconds() / rmsWindow.Seconds())
	rms := a.RMS(50 * time.Millisecond)

	// Generate x values (assuming each RMS corresponds to a 50ms window)
	x := make([]float64, analysisWindow)
	for i := 0; i < analysisWindow; i++ {
		x[i] = float64(i)
	}

	// Extract the last N RMS values for y
	y := rms[len(rms)-analysisWindow:]

	// Calculate linear regression
	slope, _ := linearRegression(x, y)

	// A negative slope indicates a fade-out, but we may set a threshold for "significance"
	// Adjust this threshold based on your specific needs
	threashold := -0.02
	if slope < threashold {
		return true
	}
	log.Println(slope)
	return false
}

func linearRegression(x, y []float64) (slope, intercept float64) {
	if len(x) != len(y) {
		return math.NaN(), math.NaN() // Error handling
	}

	var sumX, sumY, sumXY, sumXX float64
	for i := range x {
		sumX += x[i]
		sumY += y[i]
		sumXY += x[i] * y[i]
		sumXX += x[i] * x[i]
	}

	n := float64(len(x))
	slope = (n*sumXY - sumX*sumY) / (n*sumXX - sumX*sumX)
	intercept = (sumY - slope*sumX) / n

	return slope, intercept
}
