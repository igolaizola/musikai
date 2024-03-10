package image

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"path/filepath"

	"golang.org/x/image/webp"
)

type Decode func(io.Reader) (image.Image, error)

func getDecoder(file string) (Decode, error) {
	inputExt := filepath.Ext(file)
	var decode Decode
	switch inputExt {
	case ".png":
		decode = png.Decode
	case ".jpg", ".jpeg":
		decode = jpeg.Decode
	case ".webp":
		decode = webp.Decode
	default:
		return nil, fmt.Errorf("image: unsupported extension: %s", inputExt)
	}
	return decode, nil
}

type Encode func(io.Writer, image.Image) error

func getEncoder(file string) (Encode, error) {
	outputExt := filepath.Ext(file)
	var encode Encode
	switch outputExt {
	case ".png":
		encode = png.Encode
	case ".jpg", ".jpeg":
		encode = func(w io.Writer, m image.Image) error {
			return jpeg.Encode(w, m, nil)
		}
	case ".webp":
		encode = png.Encode
	default:
		return nil, fmt.Errorf("image: unsupported extension: %s", outputExt)
	}
	return encode, nil
}
