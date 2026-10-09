package render

import (
	"image"
	"image/color"
)

// DimFrame returns the frame with every colour scaled by level/256, the
// result of one step of a palette fade to black (screentoblack/blacktoscreen
// move the live palette toward the Black CLUT). Level 256 is the frame
// unchanged and 0 is black.
func DimFrame(frame IndexedFrame, level int) IndexedFrame {
	level = max(0, min(256, level))
	if level == 256 {
		return frame
	}
	if frame.rgba != nil {
		dimmed := image.NewRGBA(frame.rgba.Bounds())
		for index := 0; index+3 < len(frame.rgba.Pix); index += 4 {
			dimmed.Pix[index] = uint8(int(frame.rgba.Pix[index]) * level >> 8)
			dimmed.Pix[index+1] = uint8(int(frame.rgba.Pix[index+1]) * level >> 8)
			dimmed.Pix[index+2] = uint8(int(frame.rgba.Pix[index+2]) * level >> 8)
			dimmed.Pix[index+3] = frame.rgba.Pix[index+3]
		}
		frame.rgba = dimmed
		return frame
	}
	palette := make(color.Palette, len(frame.Palette))
	for index, entry := range frame.Palette {
		rgba := color.RGBAModel.Convert(entry).(color.RGBA)
		palette[index] = color.RGBA{R: uint8(int(rgba.R) * level >> 8), G: uint8(int(rgba.G) * level >> 8), B: uint8(int(rgba.B) * level >> 8), A: rgba.A}
	}
	frame.Palette = palette
	return frame
}
