package render

import (
	"fmt"
	"image"
	"image/draw"
)

type WipeEffect struct {
	from, target                                      IndexedFrame
	fromRGBA, targetRGBA                              *image.RGBA
	bandWidth, ticksPerStep, step, elapsed, direction int
	done                                              bool
}

func NewWipeEffect(from, target IndexedFrame, duration, direction int) (*WipeEffect, error) {
	if from.Width < 1 || from.Height < 1 || from.Width != target.Width || from.Height != target.Height || len(from.Pixels) != from.Width*from.Height || len(target.Pixels) != target.Width*target.Height || direction < 0 || direction > 1 {
		return nil, fmt.Errorf("wipe frames or direction are invalid")
	}
	if duration < 1 {
		duration = 1
	}
	if duration > 1000 {
		duration = 1000
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
	return &WipeEffect{from: from, target: target, fromRGBA: clonePuppetRGBA(fromRGBA), targetRGBA: clonePuppetRGBA(targetRGBA), bandWidth: from.Width/16 + 1, ticksPerStep: max(duration/15, 1), step: 1, direction: direction}, nil
}

func (e *WipeEffect) CurrentFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	if e.done {
		return e.TargetFrame()
	}
	pixels := append([]byte(nil), e.from.Pixels...)
	rgba := clonePuppetRGBA(e.fromRGBA)
	width := min(e.from.Width, e.step*e.bandWidth)
	left, right := 0, width
	if e.direction == 1 {
		left, right = e.from.Width-width, e.from.Width
	}
	for y := 0; y < e.from.Height; y++ {
		copy(pixels[y*e.from.Width+left:y*e.from.Width+right], e.target.Pixels[y*e.target.Width+left:y*e.target.Width+right])
	}
	rect := image.Rect(left, 0, right, e.from.Height)
	draw.Draw(rgba, rect, e.targetRGBA, rect.Min, draw.Src)
	return IndexedFrame{Width: e.from.Width, Height: e.from.Height, Pixels: pixels, Palette: e.target.Palette, rgba: rgba}
}

func (e *WipeEffect) TargetFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	frame := e.target
	frame.rgba = e.targetRGBA
	return frame
}

func (e *WipeEffect) Update() (IndexedFrame, bool, bool) {
	if e == nil || e.done {
		return e.TargetFrame(), false, true
	}
	e.elapsed++
	if e.elapsed < e.ticksPerStep {
		return e.CurrentFrame(), false, false
	}
	e.elapsed = 0
	if e.step == 15 {
		e.done = true
		return e.TargetFrame(), true, true
	}
	e.step++
	return e.CurrentFrame(), true, false
}
