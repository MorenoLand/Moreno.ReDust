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

type paletteEntry struct {
	rgb   [3]byte
	bgr   [4]byte
	fixed [3]uint16
	flags byte
}

type PaletteState struct {
	entries [256]paletteEntry
}

func (p *PaletteState) SetEntry(index int, red, green, blue uint8) error {
	if p == nil {
		return fmt.Errorf("palette state is nil")
	}
	if index < 0 || index >= len(p.entries) {
		return fmt.Errorf("palette index %d is out of range", index)
	}
	entry := &p.entries[index]
	entry.rgb = [3]byte{red, green, blue}
	entry.bgr = [4]byte{blue, green, red, 0}
	entry.fixed = [3]uint16{uint16(red) << 8, uint16(green) << 8, uint16(blue) << 8}
	entry.flags = 0
	if index != 0 && index != len(p.entries)-1 {
		entry.flags = 5
	}
	return nil
}

func (p PaletteState) Colors() color.Palette {
	colors := make(color.Palette, len(p.entries))
	for index, entry := range p.entries {
		colors[index] = color.RGBA{R: entry.rgb[0], G: entry.rgb[1], B: entry.rgb[2], A: 255}
	}
	return colors
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
