package render

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

	"github.com/hajimehoshi/ebiten/v2"

	"redust/assets"
)

type IndexedFrame struct {
	Width   int
	Height  int
	Pixels  []byte
	Palette color.Palette
	rgba    *image.RGBA
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
	if f.rgba != nil {
		if f.rgba.Bounds().Dx() != f.Width || f.rgba.Bounds().Dy() != f.Height {
			return nil, fmt.Errorf("true-color frame does not match %dx%d", f.Width, f.Height)
		}
		return ebiten.NewImageFromImage(f.rgba), nil
	}
	frame, err := f.Image()
	if err != nil {
		return nil, err
	}
	return ebiten.NewImageFromImage(frame), nil
}

func (f IndexedFrame) rgbaImage() (*image.RGBA, error) {
	if f.rgba != nil {
		return f.rgba, nil
	}
	frame, err := f.Image()
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(frame.Bounds())
	draw.Draw(rgba, rgba.Bounds(), frame, frame.Bounds().Min, draw.Src)
	return rgba, nil
}

func CompositeUnderlay(background, overlay IndexedFrame) (IndexedFrame, error) {
	if background.Width != overlay.Width || background.Height <= 0 || background.Height >= overlay.Height {
		return IndexedFrame{}, fmt.Errorf("underlay %dx%d does not fit overlay %dx%d", background.Width, background.Height, overlay.Width, overlay.Height)
	}
	backgroundRGBA, err := background.rgbaImage()
	if err != nil {
		return IndexedFrame{}, err
	}
	overlayRGBA, err := overlay.rgbaImage()
	if err != nil {
		return IndexedFrame{}, err
	}
	composite := image.NewRGBA(image.Rect(0, 0, overlay.Width, overlay.Height))
	draw.Draw(composite, image.Rect(0, 0, background.Width, background.Height), backgroundRGBA, image.Point{}, draw.Src)
	draw.Draw(composite, image.Rect(0, background.Height, overlay.Width, overlay.Height), overlayRGBA, image.Point{Y: background.Height}, draw.Src)
	overlay.rgba = composite
	return overlay, nil
}

func CompositePanel(frame, panel IndexedFrame, panelTop int) (IndexedFrame, error) {
	if frame.Width <= 0 || frame.Width != panel.Width || frame.Height != panel.Height || panelTop < 0 || panelTop >= frame.Height {
		return IndexedFrame{}, fmt.Errorf("panel frames or top boundary are invalid")
	}
	frameRGBA, err := frame.rgbaImage()
	if err != nil {
		return IndexedFrame{}, err
	}
	panelRGBA, err := panel.rgbaImage()
	if err != nil {
		return IndexedFrame{}, err
	}
	composite := image.NewRGBA(image.Rect(0, 0, frame.Width, frame.Height))
	draw.Draw(composite, composite.Bounds(), frameRGBA, image.Point{}, draw.Src)
	draw.Draw(composite, image.Rect(0, panelTop, frame.Width, frame.Height), panelRGBA, image.Pt(0, panelTop), draw.Src)
	frame.rgba = composite
	return frame, nil
}

func WritePNG(path string, frame IndexedFrame) error {
	image, err := frame.rgbaImage()
	if err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, image); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func StageFrame(stage *assets.Stage, pixels []byte) (IndexedFrame, error) {
	if stage == nil {
		return IndexedFrame{}, fmt.Errorf("stage is unavailable")
	}
	return indexedFrame(int(stage.Width), int(stage.Height), pixels, stage.PaletteRaw)
}

func indexedFrame(width, height int, pixels, paletteRaw []byte) (IndexedFrame, error) {
	if width <= 0 || height <= 0 || len(pixels) != width*height {
		return IndexedFrame{}, fmt.Errorf("indexed frame data does not match %dx%d", width, height)
	}
	palette, err := PaletteStateFromRaw(paletteRaw)
	if err != nil {
		return IndexedFrame{}, err
	}
	return IndexedFrame{Width: width, Height: height, Pixels: append([]byte(nil), pixels...), Palette: palette.Colors()}, nil
}

func PaletteStateFromRaw(paletteRaw []byte) (PaletteState, error) {
	if len(paletteRaw) != 0x800 {
		return PaletteState{}, fmt.Errorf("indexed palette has %d bytes, want 2048", len(paletteRaw))
	}
	var palette PaletteState
	for index := range palette.entries {
		offset := index * 8
		if err := palette.SetFixedEntry(index, binary.LittleEndian.Uint16(paletteRaw[offset+2:offset+4]), binary.LittleEndian.Uint16(paletteRaw[offset+4:offset+6]), binary.LittleEndian.Uint16(paletteRaw[offset+6:offset+8])); err != nil {
			return PaletteState{}, err
		}
	}
	return palette, nil
}

func interpolatePalette(from, to PaletteState, step, duration int) PaletteState {
	if duration < 1 {
		duration = 1
	}
	if step < 0 {
		step = 0
	} else if step > duration {
		step = duration
	}
	var palette PaletteState
	for index := range palette.entries {
		var components [3]uint16
		for channel := range components {
			start, end := int64(from.entries[index].fixed[channel]), int64(to.entries[index].fixed[channel])
			components[channel] = uint16(start + (end-start)*int64(step)/int64(duration))
		}
		_ = palette.SetFixedEntry(index, components[0], components[1], components[2])
	}
	return palette
}
