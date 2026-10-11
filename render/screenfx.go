package render

import (
	"fmt"
	"image"
)

// ScreenEffectKind names a strip transition that FUN_00407160 runs for a
// 24000-band keyword. The geometry below is the native's own: each step blits
// one strip (or, for irisclose and venetian, several strips) from the new frame
// into the screen, and the caller waits between steps.
//
// Rectangles use the word order that FUN_0042D120's BitBlt call reads: word 0
// is the top, word 1 the left, word 2 the bottom and word 3 the right. With the
// 512 by 384 playfield, the stage view is 512 wide and 384 high. This reading
// comes from the decompile and the playfield size; it is not checked against
// a native frame.
type ScreenEffectKind uint8

const (
	// ScreenWipeUp is wipeup, FUN_00407AF0: fifteen full-width bands from the
	// bottom edge upward, then the full stage.
	ScreenWipeUp ScreenEffectKind = iota + 1
	// ScreenWipeDown is wipedown, FUN_00407A30: fifteen full-width bands from
	// the top edge downward, then the full stage.
	ScreenWipeDown
	// ScreenScrollUp is scrollup, FUN_00407750: the top band of the new frame
	// fills the bottom of the screen, growing upward; then the full stage.
	ScreenScrollUp
	// ScreenScrollDown is scrolldown, FUN_00407690: the bottom band of the new
	// frame fills the top of the screen; then the full stage.
	ScreenScrollDown
	// ScreenScrollLeft is scrolleft, FUN_004078D0: the left band of the new
	// frame fills the right edge of the screen; then the full stage.
	ScreenScrollLeft
	// ScreenScrollRight is scrollright, FUN_00407810: the right band of the new
	// frame fills the left edge of the screen; then the full stage.
	ScreenScrollRight
	// ScreenVenetian is venetian, FUN_00407990: forty-nine offsets of two-row
	// slats, spaced forty-nine rows apart. Together the offsets cover every
	// row, so no full-stage draw follows.
	ScreenVenetian
	// ScreenIrisClose is irisclose, FUN_00407460: four edge strips (top,
	// bottom, left and right) move inward in six steps. The strips cover the
	// whole frame by the last step, so no full-stage draw follows.
	ScreenIrisClose
)

// screenStrip copies the src rectangle of the new frame to the dst rectangle
// of the screen. Both have the same size, and both are in frame coordinates.
type screenStrip struct {
	dst, src image.Rectangle
}

// StripEffect is a timed strip transition. It implements the same Update,
// CurrentFrame and TargetFrame contract as WipeEffect and IrisOpenEffect.
type StripEffect struct {
	target       IndexedFrame
	targetRGBA   *image.RGBA
	steps        [][]screenStrip
	finalDraw    bool
	ticksPerStep int
	applied      int
	elapsed      int
	pixels       []byte
	rgba         *image.RGBA
	done         bool
}

// NewScreenEffect starts a strip transition from the frame on the screen to
// target. The first strip is blitted before the call returns, as the native
// handler blits before its first wait. ticksPerStep is the caller's wait per
// step from the verified schedule, at least one tick.
func NewScreenEffect(kind ScreenEffectKind, from, target IndexedFrame, ticksPerStep int) (*StripEffect, error) {
	if from.Width < 1 || from.Height < 1 || from.Width != target.Width || from.Height != target.Height || len(from.Pixels) != from.Width*from.Height || len(target.Pixels) != target.Width*target.Height {
		return nil, fmt.Errorf("screen effect frames do not match")
	}
	steps, finalDraw, err := screenEffectSteps(kind, from.Width, from.Height)
	if err != nil {
		return nil, err
	}
	fromRGBA, err := from.rgbaImage()
	if err != nil {
		return nil, err
	}
	targetRGBA, err := target.rgbaImage()
	if err != nil {
		return nil, err
	}
	target.Pixels = append([]byte(nil), target.Pixels...)
	effect := &StripEffect{
		target:       target,
		targetRGBA:   clonePuppetRGBA(targetRGBA),
		steps:        steps,
		finalDraw:    finalDraw,
		ticksPerStep: max(ticksPerStep, 1),
		pixels:       append([]byte(nil), from.Pixels...),
		rgba:         clonePuppetRGBA(fromRGBA),
	}
	effect.applyNext()
	return effect, nil
}

// screenEffectSteps returns the strips of each step for a w by h stage view,
// and whether the handler draws the full stage after its last wait.
func screenEffectSteps(kind ScreenEffectKind, w, h int) ([][]screenStrip, bool, error) {
	same := func(r image.Rectangle) screenStrip { return screenStrip{dst: r, src: r} }
	build := func(count int, strip func(k int) []screenStrip) [][]screenStrip {
		steps := make([][]screenStrip, count)
		for k := range steps {
			steps[k] = strip(k)
		}
		return steps
	}
	switch kind {
	case ScreenWipeUp:
		band := h/16 + 1
		return build(15, func(k int) []screenStrip {
			return []screenStrip{same(image.Rect(0, h-(k+1)*band, w, h-k*band))}
		}), true, nil
	case ScreenWipeDown:
		band := h/16 + 1
		return build(15, func(k int) []screenStrip {
			return []screenStrip{same(image.Rect(0, k*band, w, (k+1)*band))}
		}), true, nil
	case ScreenScrollUp, ScreenScrollDown:
		band := h/16 + 1
		return build(15, func(k int) []screenStrip {
			n := (k + 1) * band
			if kind == ScreenScrollDown {
				return []screenStrip{{dst: image.Rect(0, 0, w, n), src: image.Rect(0, h-n, w, h)}}
			}
			return []screenStrip{{dst: image.Rect(0, h-n, w, h), src: image.Rect(0, 0, w, n)}}
		}), true, nil
	case ScreenScrollLeft, ScreenScrollRight:
		band := w/16 + 1
		return build(15, func(k int) []screenStrip {
			n := (k + 1) * band
			if kind == ScreenScrollRight {
				return []screenStrip{{dst: image.Rect(0, 0, n, h), src: image.Rect(w-n, 0, w, h)}}
			}
			return []screenStrip{{dst: image.Rect(w-n, 0, w, h), src: image.Rect(0, 0, n, h)}}
		}), true, nil
	case ScreenVenetian:
		band := h>>3 + 1
		return build(band, func(s int) []screenStrip {
			var strips []screenStrip
			for j := 0; j == 0 || j*band < h; j++ {
				y := s + j*band
				strips = append(strips, same(image.Rect(0, y, w, y+2)))
			}
			return strips
		}), false, nil
	case ScreenIrisClose:
		bandY, bandX := h/12+1, w/12+1
		return build(6, func(k int) []screenStrip {
			return []screenStrip{
				same(image.Rect(0, k*bandY, w, (k+1)*bandY)),
				same(image.Rect(0, h-(k+1)*bandY, w, h-k*bandY)),
				same(image.Rect(k*bandX, 0, (k+1)*bandX, h)),
				same(image.Rect(w-(k+1)*bandX, 0, w-k*bandX, h)),
			}
		}), false, nil
	}
	return nil, false, fmt.Errorf("screen effect kind %d is unknown", kind)
}

// applyNext blits the next step's strips into the screen state.
func (e *StripEffect) applyNext() {
	bounds := image.Rect(0, 0, e.target.Width, e.target.Height)
	for _, strip := range e.steps[e.applied] {
		dst, src := strip.dst.Intersect(bounds), strip.src.Intersect(bounds)
		if dst.Empty() || src.Empty() || dst.Dx() != src.Dx() || dst.Dy() != src.Dy() {
			continue
		}
		width := dst.Dx()
		for row := 0; row < dst.Dy(); row++ {
			dstY, srcY := dst.Min.Y+row, src.Min.Y+row
			copy(e.pixels[dstY*e.target.Width+dst.Min.X:dstY*e.target.Width+dst.Min.X+width], e.target.Pixels[srcY*e.target.Width+src.Min.X:srcY*e.target.Width+src.Min.X+width])
			copy(e.rgba.Pix[dstY*e.rgba.Stride+dst.Min.X*4:dstY*e.rgba.Stride+(dst.Min.X+width)*4], e.targetRGBA.Pix[srcY*e.targetRGBA.Stride+src.Min.X*4:srcY*e.targetRGBA.Stride+(src.Min.X+width)*4])
		}
	}
	e.applied++
}

// CurrentFrame returns the screen as it is after the strips applied so far.
func (e *StripEffect) CurrentFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	return IndexedFrame{Width: e.target.Width, Height: e.target.Height, Pixels: append([]byte(nil), e.pixels...), Palette: e.target.Palette, rgba: clonePuppetRGBA(e.rgba)}
}

// TargetFrame returns the new frame that the effect ends on.
func (e *StripEffect) TargetFrame() IndexedFrame {
	if e == nil {
		return IndexedFrame{}
	}
	frame := e.target
	frame.Pixels = append([]byte(nil), e.target.Pixels...)
	frame.rgba = clonePuppetRGBA(e.targetRGBA)
	return frame
}

// Update advances one tick. It reports whether the displayed frame changed and
// whether the effect has finished. A strip is blitted once its wait has run,
// and the effect ends after the last wait. A handler with a full-stage draw
// ends on the target; the others end on the screen they have built, which
// the schedule covers completely.
func (e *StripEffect) Update() (IndexedFrame, bool, bool) {
	if e == nil {
		return IndexedFrame{}, false, true
	}
	if e.done {
		return e.CurrentFrame(), false, true
	}
	e.elapsed++
	if e.elapsed < e.ticksPerStep {
		return e.CurrentFrame(), false, false
	}
	e.elapsed = 0
	if e.applied < len(e.steps) {
		e.applyNext()
		return e.CurrentFrame(), true, false
	}
	e.done = true
	if e.finalDraw {
		e.pixels = append([]byte(nil), e.target.Pixels...)
		e.rgba = clonePuppetRGBA(e.targetRGBA)
	}
	return e.CurrentFrame(), e.finalDraw, true
}
