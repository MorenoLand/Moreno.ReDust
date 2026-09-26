package render

import (
	"fmt"
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
)

type IndexedFrame struct {
	Width   int
	Height  int
	Pixels  []byte
	Palette color.Palette
}

func (f IndexedFrame) Image() (*image.Paletted, error) {
	if f.Width <= 0 || f.Height <= 0 {
		return nil, fmt.Errorf("invalid indexed frame dimensions %dx%d", f.Width, f.Height)
	}
	if len(f.Palette) != 256 {
		return nil, fmt.Errorf("indexed frame palette has %d entries, want 256", len(f.Palette))
	}
	if len(f.Pixels) != f.Width*f.Height {
		return nil, fmt.Errorf("indexed frame has %d pixels, want %d", len(f.Pixels), f.Width*f.Height)
	}
	frame := image.NewPaletted(image.Rect(0, 0, f.Width, f.Height), f.Palette)
	copy(frame.Pix, f.Pixels)
	return frame, nil
}

func (f IndexedFrame) EbitenImage() (*ebiten.Image, error) {
	frame, err := f.Image()
	if err != nil {
		return nil, err
	}
	return ebiten.NewImageFromImage(frame), nil
}
