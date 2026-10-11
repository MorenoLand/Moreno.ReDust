package render

import (
	"fmt"
	"image"
	"image/draw"
)

type BarndoorEffect struct {
	from         IndexedFrame
	target       IndexedFrame
	bandWidth    int
	ticksPerStep int
	step         int
	elapsed      int
	open         bool
	done         bool
	fromRGBA     *image.RGBA
	targetRGBA   *image.RGBA
}

type FadeEffect struct {
	from, target         IndexedFrame
	fromRGBA, targetRGBA *image.RGBA
	duration, step       int
	done                 bool
}

func NewFadeEffect(from, target IndexedFrame, duration int) (*FadeEffect, error) {
	if from.Width < 1 || from.Height < 1 || from.Width != target.Width || from.Height != target.Height || len(from.Pixels) != from.Width*from.Height || len(target.Pixels) != target.Width*target.Height || duration < 1 || duration > 1000 {
		return nil, fmt.Errorf("fade frames or duration are invalid")
	}
	fromRGBA, err := from.rgbaImage()
	if err != nil {
		return nil, err
	}
	targetRGBA, err := target.rgbaImage()
	if err != nil {
		return nil, err
	}
	from.Pixels, target.Pixels = append([]byte(nil), from.Pixels...), append([]byte(nil), target.Pixels...)
	return &FadeEffect{from: from, target: target, fromRGBA: clonePuppetRGBA(fromRGBA), targetRGBA: clonePuppetRGBA(targetRGBA), duration: duration}, nil
}

func (e *FadeEffect) CurrentFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	frame := image.NewRGBA(image.Rect(0, 0, e.from.Width, e.from.Height))
	for index, start := range e.fromRGBA.Pix {
		end := e.targetRGBA.Pix[index]
		frame.Pix[index] = byte((int(start)*(e.duration-e.step) + int(end)*e.step) / e.duration)
	}
	return IndexedFrame{Width: e.from.Width, Height: e.from.Height, Pixels: append([]byte(nil), e.from.Pixels...), Palette: e.target.Palette, rgba: frame}
}

func (e *FadeEffect) TargetFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	return e.target
}

func (e *FadeEffect) Update() (IndexedFrame, bool, bool) {
	if e == nil || e.done {
		return e.TargetFrame(), false, true
	}
	e.step++
	if e.step >= e.duration {
		e.done = true
		return e.TargetFrame(), true, true
	}
	return e.CurrentFrame(), true, false
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
	effect := &BarndoorEffect{from: from, target: target, bandWidth: from.Width/16 + 1, ticksPerStep: stepTicks, step: 1, open: open}
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

// CurrentFrame returns the screen during the wait after the bands so far.
// The native bands are vertical strips. FUN_004072C0 and FUN_00407390 take the
// band size from word 3 minus word 1 of the stage rectangle, and word 1 is the
// x that the BitBlt reads, so each band is bandWidth columns of full height.
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
	copyColumns := func(start, end int) {
		if start < 0 {
			start = 0
		}
		if end > e.from.Width {
			end = e.from.Width
		}
		if start >= end {
			return
		}
		for y := 0; y < e.from.Height; y++ {
			copy(pixels[y*e.from.Width+start:y*e.from.Width+end], e.target.Pixels[y*e.target.Width+start:y*e.target.Width+end])
			if rgba != nil {
				copy(rgba.Pix[y*rgba.Stride+start*4:y*rgba.Stride+end*4], e.targetRGBA.Pix[y*e.targetRGBA.Stride+start*4:y*e.targetRGBA.Stride+end*4])
			}
		}
	}
	center := e.from.Width / 2
	for band := 0; band < e.step; band++ {
		leftStart, leftEnd := band*e.bandWidth, (band+1)*e.bandWidth
		rightStart, rightEnd := e.from.Width-(band+1)*e.bandWidth, e.from.Width-band*e.bandWidth
		if e.open {
			leftStart, leftEnd = center-(band+1)*e.bandWidth, center-band*e.bandWidth
			rightStart, rightEnd = center+band*e.bandWidth, center+(band+1)*e.bandWidth
		}
		copyColumns(leftStart, leftEnd)
		copyColumns(rightStart, rightEnd)
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
