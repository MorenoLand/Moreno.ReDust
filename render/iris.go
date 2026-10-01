package render

import (
	"fmt"
	"image"
	"image/draw"
)

type IrisOpenEffect struct {
	from, target         IndexedFrame
	fromRGBA, targetRGBA *image.RGBA
	step, elapsed        int
	ticksPerFrame        int
	done                 bool
}

func NewIrisOpenEffect(from, target IndexedFrame, duration int) (*IrisOpenEffect, error) {
	if from.Width < 1 || from.Height < 1 || from.Width != target.Width || from.Height != target.Height || len(from.Pixels) != from.Width*from.Height || len(target.Pixels) != target.Width*target.Height {
		return nil, fmt.Errorf("irisopen frames do not have matching pixel dimensions")
	}
	if from.rgba != nil && (from.rgba.Bounds().Dx() != from.Width || from.rgba.Bounds().Dy() != from.Height) || target.rgba != nil && (target.rgba.Bounds().Dx() != target.Width || target.rgba.Bounds().Dy() != target.Height) {
		return nil, fmt.Errorf("irisopen true-color frame dimensions do not match")
	}
	if duration < 1 {
		duration = 1
	} else if duration > 1000 {
		duration = 1000
	}
	ticksPerFrame := duration / 16
	if ticksPerFrame < 1 {
		ticksPerFrame = 1
	}
	hasRGBA := from.rgba != nil || target.rgba != nil
	palettesMatch := len(from.Palette) == 256 && len(from.Palette) == len(target.Palette)
	if palettesMatch {
		for index := range from.Palette {
			fr, fg, fb, fa := from.Palette[index].RGBA()
			tr, tg, tb, ta := target.Palette[index].RGBA()
			if fr != tr || fg != tg || fb != tb || fa != ta {
				palettesMatch = false
				break
			}
		}
	}
	hasRGBA = hasRGBA || !palettesMatch
	effect := &IrisOpenEffect{from: from, target: target, step: 1, ticksPerFrame: ticksPerFrame}
	effect.from.Pixels = append([]byte(nil), from.Pixels...)
	effect.target.Pixels = append([]byte(nil), target.Pixels...)
	if hasRGBA {
		fromRGBA, err := from.rgbaImage()
		if err != nil {
			return nil, err
		}
		targetRGBA, err := target.rgbaImage()
		if err != nil {
			return nil, err
		}
		effect.fromRGBA, effect.targetRGBA = clonePuppetRGBA(fromRGBA), clonePuppetRGBA(targetRGBA)
		effect.from.rgba, effect.target.rgba = effect.fromRGBA, effect.targetRGBA
	}
	return effect, nil
}

func (e *IrisOpenEffect) CurrentFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	if e.done || e.step >= 16 {
		return e.TargetFrame()
	}
	pixels := append([]byte(nil), e.from.Pixels...)
	rect := e.rect()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		start, end := y*e.from.Width+rect.Min.X, y*e.from.Width+rect.Max.X
		copy(pixels[start:end], e.target.Pixels[start:end])
	}
	var rgba *image.RGBA
	if e.fromRGBA != nil {
		rgba = image.NewRGBA(image.Rect(0, 0, e.from.Width, e.from.Height))
		draw.Draw(rgba, rgba.Bounds(), e.fromRGBA, e.fromRGBA.Bounds().Min, draw.Src)
		draw.Draw(rgba, rect, e.targetRGBA, e.targetRGBA.Bounds().Min.Add(rect.Min), draw.Src)
	}
	return IndexedFrame{Width: e.from.Width, Height: e.from.Height, Pixels: pixels, Palette: e.target.Palette, rgba: rgba}
}

func (e *IrisOpenEffect) TargetFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	frame := e.target
	frame.Pixels = append([]byte(nil), e.target.Pixels...)
	if e.targetRGBA != nil {
		frame.rgba = clonePuppetRGBA(e.targetRGBA)
	}
	return frame
}

func (e *IrisOpenEffect) Update() (IndexedFrame, bool, bool) {
	if e == nil {
		return IndexedFrame{}, false, true
	}
	if e.done {
		return e.TargetFrame(), false, true
	}
	e.elapsed++
	if e.elapsed < e.ticksPerFrame {
		return e.CurrentFrame(), false, false
	}
	e.elapsed = 0
	if e.step == 16 {
		e.done = true
		return e.TargetFrame(), false, true
	}
	e.step++
	return e.CurrentFrame(), true, false
}

func (e *IrisOpenEffect) rect() image.Rectangle {
	center := image.Pt(e.from.Width/2, e.from.Height/2)
	step := image.Pt(center.X/16+1, center.Y/16+1)
	half := image.Pt(e.step*step.X, e.step*step.Y)
	return image.Rect(center.X-half.X, center.Y-half.Y, center.X+half.X, center.Y+half.Y).Intersect(image.Rect(0, 0, e.from.Width, e.from.Height))
}
