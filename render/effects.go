package render

import (
	"fmt"
	"image"
	"image/draw"
)

type BarndoorEffect struct {
	from         IndexedFrame
	target       IndexedFrame
	bandHeight   int
	ticksPerStep int
	step         int
	elapsed      int
	open         bool
	done         bool
	fromRGBA     *image.RGBA
	targetRGBA   *image.RGBA
}

func NewBarndoorOpen(from, target IndexedFrame, duration int) (*BarndoorEffect, error) {
	return newBarndoorEffect(from, target, duration, true)
}

func NewBarndoorClose(from, target IndexedFrame, duration int) (*BarndoorEffect, error) {
	return newBarndoorEffect(from, target, duration, false)
}

func newBarndoorEffect(from, target IndexedFrame, duration int, open bool) (*BarndoorEffect, error) {
	if from.Width <= 0 || from.Height <= 0 || from.Width != target.Width || from.Height != target.Height || len(from.Pixels) != from.Width*from.Height || len(target.Pixels) != target.Width*target.Height {
		return nil, fmt.Errorf("barndooropen frames do not have matching pixel dimensions")
	}
	if duration < 1 || duration > 1000 || (from.rgba == nil && target.rgba == nil && (len(from.Palette) == 0 || len(from.Palette) != len(target.Palette))) {
		return nil, fmt.Errorf("barndooropen duration or palette is invalid")
	}
	if from.rgba == nil && target.rgba == nil {
		for i := range from.Palette {
			fr, fg, fb, fa := from.Palette[i].RGBA()
			tr, tg, tb, ta := target.Palette[i].RGBA()
			if fr != tr || fg != tg || fb != tb || fa != ta {
				return nil, fmt.Errorf("barndooropen palettes differ at entry %d", i)
			}
		}
	}
	stepTicks := duration / 8
	if stepTicks < 1 {
		stepTicks = 1
	}
	from.Pixels = append([]byte(nil), from.Pixels...)
	target.Pixels = append([]byte(nil), target.Pixels...)
	effect := &BarndoorEffect{from: from, target: target, bandHeight: from.Height/16 + 1, ticksPerStep: stepTicks, step: 1, open: open}
	if from.rgba != nil || target.rgba != nil {
		var err error
		effect.fromRGBA, err = from.rgbaImage()
		if err != nil {
			return nil, err
		}
		effect.targetRGBA, err = target.rgbaImage()
		if err != nil {
			return nil, err
		}
	}
	return effect, nil
}

func (e *BarndoorEffect) CurrentFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	pixels := append([]byte(nil), e.from.Pixels...)
	var rgba *image.RGBA
	if e.fromRGBA != nil {
		rgba = image.NewRGBA(image.Rect(0, 0, e.from.Width, e.from.Height))
		draw.Draw(rgba, rgba.Bounds(), e.fromRGBA, image.Point{}, draw.Src)
	}
	center := e.from.Height / 2
	for band := 0; band < e.step; band++ {
		topStart, topEnd := band*e.bandHeight, (band+1)*e.bandHeight
		bottomStart, bottomEnd := e.from.Height-(band+1)*e.bandHeight, e.from.Height-band*e.bandHeight
		if e.open {
			topStart, topEnd = center-(band+1)*e.bandHeight, center-band*e.bandHeight
			bottomStart, bottomEnd = center+band*e.bandHeight, center+(band+1)*e.bandHeight
		}
		if topStart < 0 {
			topStart = 0
		}
		if topEnd > e.from.Height {
			topEnd = e.from.Height
		}
		if bottomStart < 0 {
			bottomStart = 0
		}
		if bottomEnd > e.from.Height {
			bottomEnd = e.from.Height
		}
		for y := topStart; y < topEnd; y++ {
			copy(pixels[y*e.from.Width:(y+1)*e.from.Width], e.target.Pixels[y*e.target.Width:(y+1)*e.target.Width])
			if rgba != nil {
				copy(rgba.Pix[y*rgba.Stride:y*rgba.Stride+e.from.Width*4], e.targetRGBA.Pix[y*e.targetRGBA.Stride:y*e.targetRGBA.Stride+e.from.Width*4])
			}
		}
		for y := bottomStart; y < bottomEnd; y++ {
			copy(pixels[y*e.from.Width:(y+1)*e.from.Width], e.target.Pixels[y*e.target.Width:(y+1)*e.target.Width])
			if rgba != nil {
				copy(rgba.Pix[y*rgba.Stride:y*rgba.Stride+e.from.Width*4], e.targetRGBA.Pix[y*e.targetRGBA.Stride:y*e.targetRGBA.Stride+e.from.Width*4])
			}
		}
	}
	return IndexedFrame{Width: e.from.Width, Height: e.from.Height, Pixels: pixels, Palette: e.target.Palette, rgba: rgba}
}

func (e *BarndoorEffect) TargetFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	return e.target
}

func (e *BarndoorEffect) Update() (IndexedFrame, bool, bool) {
	if e == nil {
		return IndexedFrame{}, false, true
	}
	if e.done {
		return e.TargetFrame(), false, true
	}
	e.elapsed++
	if e.elapsed < e.ticksPerStep {
		return e.CurrentFrame(), false, false
	}
	e.elapsed = 0
	if e.step == 8 {
		e.done = true
		return e.CurrentFrame(), false, true
	}
	e.step++
	return e.CurrentFrame(), true, false
}
