package image

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// averageBackgroundColor calculates the average color of the specified area of the image.
/*
func averageBackgroundColor(img image.Image, rect image.Rectangle) color.Color {
	var rTotal, gTotal, bTotal, count int64
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			rTotal += int64(r)
			gTotal += int64(g)
			bTotal += int64(b)
			count++
		}
	}
	if count == 0 {
		return color.RGBA{0, 0, 0, 255} // Default to black if no pixels were analyzed
	}
	return color.RGBA{
		R: uint8(rTotal / count >> 8),
		G: uint8(gTotal / count >> 8),
		B: uint8(bTotal / count >> 8),
		A: 255,
	}
}
*/

// calculateTextPixelsAverageColor calculates the average color of pixels that match with the text letters.
func calculateTextPixelsAverageColor(img image.Image, x, y int, label string, face font.Face) color.Color {
	// Create a mask image where the text will be drawn.
	mask := image.NewAlpha(img.Bounds())

	// Draw the text onto the mask.
	dr := &font.Drawer{
		Dst:  mask,
		Src:  image.NewUniform(color.Alpha{A: 255}),
		Face: face,
		Dot: fixed.Point26_6{
			X: fixed.Int26_6(x << 6),
			Y: fixed.Int26_6(y << 6),
		},
	}
	dr.DrawString(label)

	// Now calculate the average color of the pixels under the text in the original image.
	var rTotal, gTotal, bTotal, count uint32
	for i := 0; i < mask.Bounds().Dx(); i++ {
		for j := 0; j < mask.Bounds().Dy(); j++ {
			// Check if the pixel is part of the text.
			if mask.AlphaAt(i, j).A > 0 {
				r, g, b, _ := img.At(i, j).RGBA()
				rTotal += r
				gTotal += g
				bTotal += b
				count++
			}
		}
	}

	if count == 0 {
		return color.RGBA{0, 0, 0, 255} // Default to black if no text pixels are found.
	}

	// Calculate average color.
	avgColor := color.RGBA{
		R: uint8(rTotal / count >> 8),
		G: uint8(gTotal / count >> 8),
		B: uint8(bTotal / count >> 8),
		A: 255,
	}

	return avgColor
}

/*
// chooseContrastingColor decides whether to use white or black text based on the average background color.
func chooseContrastingColor0(bgColor color.Color) color.Color {
	r, g, b, _ := bgColor.RGBA()
	// Simple algorithm to determine brightness
	brightness := (r*299 + g*587 + b*114) / 1000
	if brightness > 0xffff/2 {
		return color.RGBA{0, 0, 0, 255} // Dark text for bright backgrounds
	}
	return color.RGBA{255, 255, 255, 255} // Light text for dark backgrounds
}
*/

func chooseContrastingColor(bgColor color.Color) color.Color {
	r, g, b, _ := bgColor.RGBA()
	// Convert RGB from 16-bit to float for luminance calculation
	rFloat := float64(r) / 65535
	gFloat := float64(g) / 65535
	bFloat := float64(b) / 65535

	// Apply gamma correction for sRGB
	rLinear := linearize(rFloat)
	gLinear := linearize(gFloat)
	bLinear := linearize(bFloat)

	// Calculate the relative luminance according to ITU-R BT.709
	luminance := 0.2126*rLinear + 0.7152*gLinear + 0.0722*bLinear

	// Standard threshold for determining light/dark color
	if luminance > 0.179 {
		return color.RGBA{0, 0, 0, 255} // Dark text if background is light
	} else {
		return color.RGBA{255, 255, 255, 255} // Light text if background is dark
	}
}

// linearize converts a color channel from sRGB to linear space
func linearize(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	} else {
		return pow((c+0.055)/1.055, 2.4)
	}
}

// pow is a helper function since math.Pow requires float64 and we're working with float64 already
func pow(x, y float64) float64 {
	return math.Pow(x, y)
}

// loadFont loads a TrueType font and returns a font.Face with the specified size.
func loadFont(path string, size float64) (font.Face, error) {
	fontBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fontType, err := opentype.Parse(fontBytes)
	if err != nil {
		return nil, err
	}
	face, err := opentype.NewFace(fontType, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	return face, nil
}

// drawStringWithShadowAndContrast draws a string onto an image with a shadow for legibility and chooses a contrasting color based on the background.
/*
func drawStringWithShadowAndContrast(img draw.Image, label string, face font.Face, position Position) error {
	// Image dimensions
	imgWidth := img.Bounds().Dx()
	imgHeight := img.Bounds().Dy()

	// Measure the text
	bounds, _ := font.BoundString(face, label)
	textWidth := (bounds.Max.X - bounds.Min.X).Ceil()
	textHeight := (bounds.Max.Y - bounds.Min.Y).Ceil()

	// Calculate margins based on percentage
	xMargin := int(float64(imgWidth) * 5 / 100)
	yMargin := int(float64(imgHeight) * 5 / 100)

	// Initialize x and y position
	var x, y int

	// Calculate x and y based on Position
	switch position {
	case TopLeft:
		x = xMargin
		y = yMargin + textHeight
	case TopRight:
		x = imgWidth - textWidth - xMargin
		y = yMargin + textHeight
	case BottomLeft:
		x = xMargin
		y = imgHeight - yMargin
	case BottomRight:
		x = imgWidth - textWidth - xMargin
		y = imgHeight - yMargin
	case TopCenter:
		x = (imgWidth - textWidth) / 2
		y = yMargin + textHeight
	case BottomCenter:
		x = (imgWidth - textWidth) / 2
		y = imgHeight - yMargin
	case Center:
		x = (imgWidth - textWidth) / 2
		y = (imgHeight + textHeight) / 2
	}

	// Adjust the y position to align the text by its baseline
	if position == TopLeft || position == TopRight || position == TopCenter {
		// Calculate the ascent of the font to adjust the y position correctly for top-aligned text
		ascent := face.Metrics().Ascent.Ceil()
		y += ascent
	}

	// Calculate average background color and choose contrasting color
	// rect := image.Rect(x, y-textHeight, x+advance.Ceil(), y)
	// bgColor := averageBackgroundColor(img, rect)
	bgColor := calculateTextPixelsAverageColor(img, x, y, label, face)
	textColor := chooseContrastingColor(bgColor)
	shadowColor := color.RGBA{0, 0, 0, 255} // Semi-transparent black for the shadow

	// Draw the shadow
	shadowOffset := fixed.Int26_6(2 * 64) // Shadow offset in fixed-point format
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(shadowColor),
		Face: face,
		Dot: fixed.Point26_6{
			X: fixed.Int26_6(x+int(shadowOffset/64)) * 64,
			Y: fixed.Int26_6((y+int(shadowOffset/64))-textHeight) * 64,
		},
	}
	d.DrawString(label)

	// Draw the main text
	d.Src = image.NewUniform(textColor)
	d.Dot = fixed.Point26_6{
		X: fixed.Int26_6(x) * 64,
		Y: fixed.Int26_6(y-textHeight) * 64,
	}
	d.DrawString(label)

	return nil
}*/

// Adjust this constant to change the desired text width as a percentage of the image width
const textWidthPercentage = 50 // Example: 50%

// drawStringWithDynamicFontSize draws a string onto an image with a dynamically adjusted font size so that the text occupies a given percentage of the image size.
func drawStringWithDynamicFontSize(img draw.Image, label string, getFace func(float64) (font.Face, error), position Position, initialFontSize float64) error {
	imgWidth := img.Bounds().Dx()
	imgHeight := img.Bounds().Dy()

	lines := strings.Split(label, "\n")
	desiredTextWidth := int(float64(imgWidth) * (textWidthPercentage / 100.0))

	// Variables to keep track of the best matching font size and its measurements
	var face font.Face
	var lineTextHeight int

	// Start with the initial font size
	fontSize := initialFontSize

	// Loop to adjust font size
	for {
		// Create a new font face with the current font size
		// Note: The creation of a new font face depends on your font loading method, which might involve a font library or custom implementation.
		// Example: newFace := truetype.NewFace(yourFont, &truetype.Options{Size: fontSize})
		newFace, err := getFace(fontSize) // You need to implement this function
		if err != nil {
			return err
		}

		// Measure "Ay" with the current font size to get the line height
		singleLineHeight, _ := font.BoundString(newFace, "Ay")
		lineHeight := (singleLineHeight.Max.Y - singleLineHeight.Min.Y).Ceil()

		// Calculate the total text block height
		totalTextHeight := lineHeight * len(lines)

		// Assume the first line's width as the max width for simplicity, or you can loop through all lines to find the max
		bounds, _ := font.BoundString(newFace, lines[0])
		currentTextWidth := (bounds.Max.X - bounds.Min.X).Ceil()

		if currentTextWidth <= desiredTextWidth && totalTextHeight <= imgHeight {
			face = newFace
			fontSize += 1 // Try a larger font size
			lineTextHeight = lineHeight
		} else {
			break // The text fits within the desired percentage, or the font size is too large
		}
	}

	if face == nil {
		return errors.New("unable to find a suitable font size")
	}

	// Calculate margins based on percentage
	xMargin := int(float64(imgWidth) * 5 / 100)
	yMargin := int(float64(imgHeight) * 5 / 100)

	// Initialize x and y position
	var x, y int

	// Adjust the initial y position based on the total height of the text block and the selected position
	totalTextHeight := lineTextHeight * len(lines)
	switch position {
	case TopLeft, TopRight, TopCenter:
		y = yMargin
	case BottomLeft, BottomRight, BottomCenter:
		y = imgHeight - yMargin - totalTextHeight
	case Center:
		y = (imgHeight - totalTextHeight) / 2
	}

	// Loop through each line and draw it
	for _, line := range lines {
		fmt.Println("Drawing line:", line)

		// Measure the text width for the current line
		bounds, _ := font.BoundString(face, line)
		textWidth := (bounds.Max.X - bounds.Min.X).Ceil()

		// Calculate x position based on the Position
		switch position {
		case TopLeft, BottomLeft:
			x = xMargin
		case TopRight, BottomRight:
			x = imgWidth - textWidth - xMargin
		case TopCenter, BottomCenter, Center:
			x = (imgWidth - textWidth) / 2
		}

		// Calculate average background color and choose contrasting color
		bgColor := calculateTextPixelsAverageColor(img, x, y, line, face)
		textColor := chooseContrastingColor(bgColor)
		shadowColor := color.RGBA{0, 0, 0, 255} // Semi-transparent black for the shadow

		// Draw the shadow
		shadowOffset := fixed.Int26_6(2 * 64) // Shadow offset in fixed-point format
		d := &font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(shadowColor),
			Face: face,
			Dot: fixed.Point26_6{
				X: fixed.Int26_6(x+int(shadowOffset/64)) * 64,
				Y: fixed.Int26_6((y + int(shadowOffset/64))) * 64,
			},
		}
		d.DrawString(line)

		// Draw the main text
		d.Src = image.NewUniform(textColor)
		d.Dot = fixed.Point26_6{
			X: fixed.Int26_6(x) * 64,
			Y: fixed.Int26_6(y) * 64,
		}
		d.DrawString(line)

		// Move y position for the next line
		y += lineTextHeight + (lineTextHeight / 4)
	}
	return nil
}

type Position int

const (
	TopLeft Position = iota
	TopRight
	BottomLeft
	BottomRight
	TopCenter
	BottomCenter
	Center
)

// AddText opens an image, adds text to it with shadow and contrast adjustment, and saves the result.
func AddText(text string, position Position, fnt, input, output string) error {
	// Get encoder and decoder
	decode, err := getDecoder(input)
	if err != nil {
		return err
	}
	encode, err := getEncoder(output)
	if err != nil {
		return err
	}

	file, err := os.Open(input)
	if err != nil {
		return err
	}
	defer file.Close()

	img, err := decode(file)
	if err != nil {
		return err
	}

	// Convert image to RGBA to ensure we can draw on it
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{0, 0}, draw.Src)

	/*
		// Calculate the font size as a percentage of the shorter dimension
		// Determine the shorter dimension of the image
		imgWidth := img.Bounds().Dx()
		imgHeight := img.Bounds().Dy()
		shorterDim := imgWidth
		if imgHeight < imgWidth {
			shorterDim = imgHeight
		}
		fontSize := float64(shorterDim) * 6 / 100.0

		face, err := loadFont(fnt, fontSize)
		if err != nil {
			return err
		}
		defer face.Close()

		//  Draw the text with shadow and contrast
		if err := drawStringWithShadowAndContrast(rgba, text, face, position); err != nil {
			return err
		}
	*/

	// Draw the text with dynamic font size
	if err := drawStringWithDynamicFontSize(rgba, text, func(size float64) (font.Face, error) {
		return loadFont(fnt, size)
	}, position, 12); err != nil {
		return err
	}

	// Save the result
	outputFile, err := os.Create(output)
	if err != nil {
		return err
	}
	defer outputFile.Close()

	return encode(outputFile, rgba)
}
