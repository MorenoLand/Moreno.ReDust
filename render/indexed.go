package render

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"redust/assets"
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

var nativePaletteCurve = func() [256]uint8 {
	var curve [256]uint8
	for i := range curve {
		curve[i] = uint8(math.Pow(float64(i)/255, 0.75) * 255)
	}
	return curve
}()

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

func (p *PaletteState) SetFixedEntry(index int, red, green, blue uint16) error {
	if index == 0 {
		red, green, blue = 0, 0, 0
	} else if index == 255 {
		red, green, blue = 0xffff, 0xffff, 0xffff
	}
	if err := p.SetEntry(index, nativePaletteChannel(red), nativePaletteChannel(green), nativePaletteChannel(blue)); err != nil {
		return err
	}
	p.entries[index].fixed = [3]uint16{red, green, blue}
	return nil
}

func nativePaletteChannel(value uint16) uint8 {
	index, fraction := int(value>>8), int(value&0xff)
	base := int(nativePaletteCurve[index])
	if index == 255 {
		return uint8(base)
	}
	return uint8(base + (int(nativePaletteCurve[index+1])-base)*fraction>>8)
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

func StageFrame(stage *assets.Stage, pixels []byte) (IndexedFrame, error) {
	if stage == nil {
		return IndexedFrame{}, fmt.Errorf("stage is unavailable")
	}
	width, height := int(stage.Width), int(stage.Height)
	if width <= 0 || height <= 0 || len(pixels) != width*height {
		return IndexedFrame{}, fmt.Errorf("stage frame data does not match %dx%d", width, height)
	}
	if len(stage.PaletteRaw) != 0x800 {
		return IndexedFrame{}, fmt.Errorf("stage palette has %d bytes, want 2048", len(stage.PaletteRaw))
	}
	var palette PaletteState
	for index := range palette.entries {
		offset := index * 8
		if err := palette.SetFixedEntry(index, binary.LittleEndian.Uint16(stage.PaletteRaw[offset+2:offset+4]), binary.LittleEndian.Uint16(stage.PaletteRaw[offset+4:offset+6]), binary.LittleEndian.Uint16(stage.PaletteRaw[offset+6:offset+8])); err != nil {
			return IndexedFrame{}, err
		}
	}
	return IndexedFrame{Width: width, Height: height, Pixels: append([]byte(nil), pixels...), Palette: palette.Colors()}, nil
}
