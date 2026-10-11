package render

import (
	"bytes"
	"fmt"
	"image/color"
)

// ScreenPalette is the port's copy of the native live screen palette, DAT_004590FC,
// held as a 2048-byte CLUT in the layout of MixPaletteRaw.
//
// The native game draws every indexed frame through one palette. The port keeps
// the palette each frame was built with, so with no palette statement run the
// output is unchanged. Once a palette statement applies a palette (mixclut, or
// the native clut through the same apply), an unmodified indexed frame whose
// palette is the base the change was made from is shown through the live
// palette. Frames with any other palette keep their own, which approximates the
// single native palette. Movie and fade frames are composed as RGBA and are not
// shown through it.
//
// The zero value is ready to use. It is not safe for concurrent use; the
// presenter calls it from the update goroutine only.
type ScreenPalette struct {
	raw        []byte
	colors     color.Palette
	touched    bool
	base       []byte
	lastSource []byte
}

// Show returns frame as the screen shows it. A frame without a changed live
// palette is returned unchanged. Once a palette statement has applied a palette,
// an unmodified indexed frame whose source is the base palette takes the live
// colours. The source of the last indexed frame shown is recorded, so that
// Raw can report the palette the screen is showing.
func (s *ScreenPalette) Show(frame IndexedFrame) IndexedFrame {
	if frame.rgba != nil || frame.source == nil || !frame.paletteIntact() {
		return frame
	}
	s.lastSource = frame.source
	if !s.touched {
		return frame
	}
	if s.base == nil {
		s.base = frame.source
	}
	if bytes.Equal(frame.source, s.base) {
		frame.Palette = s.colors
	}
	return frame
}

// Raw returns a copy of the live palette as a 2048-byte CLUT, which is the
// palette the screen shows. Before any palette statement has changed it, that is
// the palette of the last indexed frame shown, the way the native live palette
// starts from the frame's palette. It returns nil when there is none.
func (s *ScreenPalette) Raw() []byte {
	if s.touched {
		return append([]byte(nil), s.raw...)
	}
	if s.lastSource != nil {
		return append([]byte(nil), s.lastSource...)
	}
	return nil
}

// Apply runs the native apply (FUN_0042F060) with clut and makes the result the
// live palette. Entries 1..254 are copied with their index words. The RGB of
// entry 0 becomes black and that of entry 255 white, and their index words are
// left as they were. base is the palette source the screen should show the live
// palette through; nil means the palette of the last indexed frame shown.
func (s *ScreenPalette) Apply(clut, base []byte) error {
	if len(clut) != clutRawSize {
		return fmt.Errorf("screen palette apply has %d bytes, want %d", len(clut), clutRawSize)
	}
	live := s.Raw()
	if live == nil {
		live = make([]byte, clutRawSize)
	}
	copy(live[8:clutRawSize-8], clut[8:clutRawSize-8])
	for _, offset := range []int{2, 4, 6} {
		live[offset], live[offset+1] = 0, 0
	}
	for _, offset := range []int{clutRawSize - 6, clutRawSize - 4, clutRawSize - 2} {
		live[offset], live[offset+1] = 0xff, 0xff
	}
	palette, err := PaletteStateFromRaw(live)
	if err != nil {
		return err
	}
	if !s.touched {
		s.base = base
		if s.base == nil {
			s.base = s.lastSource
		}
	}
	s.raw = live
	s.colors = palette.Colors()
	s.touched = true
	return nil
}

// Reset drops any palette statement's change, as opening a new set does. The
// live palette then starts again from the palette of the next indexed frame
// shown.
func (s *ScreenPalette) Reset() {
	s.raw, s.colors, s.touched, s.base = nil, nil, false, nil
}
