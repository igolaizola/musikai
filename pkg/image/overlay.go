package image

import (
	"image"
	"image/draw"
	"os"
)

// AddOverlay applies a PNG overlay over a base image.
func AddOverlay(overlay, input, output string) error {
	// Get encoder and decoder
	overlayDecode, err := getDecoder(overlay)
	if err != nil {
		return err
	}
	inputDecode, err := getDecoder(input)
	if err != nil {
		return err
	}
	encode, err := getEncoder(output)
	if err != nil {
		return err
	}

	// Open the base image file.
	inputFile, err := os.Open(input)
	if err != nil {
		return err
	}
	defer inputFile.Close()

	// Decode the base image.
	baseImage, err := inputDecode(inputFile)
	if err != nil {
		return err
	}

	// Open the overlay image file.
	overlayFile, err := os.Open(overlay)
	if err != nil {
		return err
	}
	defer overlayFile.Close()

	// Decode the overlay image.
	overlayImage, err := overlayDecode(overlayFile)
	if err != nil {
		return err
	}

	// Create an image the size of the base image to hold the final output.
	outputImage := image.NewRGBA(baseImage.Bounds())

	// Draw the base image onto the output image.
	draw.Draw(outputImage, baseImage.Bounds(), baseImage, image.Point{}, draw.Src)

	// Draw the overlay image onto the output image.
	overlayBounds := overlayImage.Bounds()
	overlayOffset := image.Pt((baseImage.Bounds().Dx()-overlayBounds.Dx())/2, (baseImage.Bounds().Dy()-overlayBounds.Dy())/2)
	draw.Draw(outputImage, overlayImage.Bounds().Add(overlayOffset), overlayImage, image.Point{}, draw.Over)

	// Create the output file.
	outputFile, err := os.Create(output)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	// Encode the output image to the output file.
	return encode(outputFile, outputImage)
}
