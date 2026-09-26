package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/png"
)

//go:embed icon.png
var embeddedIcon []byte

func AppIcon() (image.Image, error) {
	icon, err := png.Decode(bytes.NewReader(embeddedIcon))
	if err != nil {
		return nil, fmt.Errorf("decode embedded icon: %w", err)
	}
	return icon, nil
}
