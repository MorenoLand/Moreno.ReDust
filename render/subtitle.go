package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

var nativeSubtitleFont struct {
	once sync.Once
	face font.Face
	err  error
}

func DrawNativeSubtitle(frame IndexedFrame, text string) (IndexedFrame, error) {
	face, err := nativeSubtitleFace()
	if err != nil {
		return IndexedFrame{}, err
	}
	background, err := frame.rgbaImage()
	if err != nil {
		return IndexedFrame{}, err
	}
	rgba := image.NewRGBA(background.Bounds())
	draw.Draw(rgba, rgba.Bounds(), background, image.Point{}, draw.Src)
	lines := nativeSubtitleLines(text, face)
	white := image.NewUniform(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	drawer := font.Drawer{Dst: rgba, Src: white, Face: face}
	for index, line := range lines {
		drawer.Dot = fixed.P(8, 240+16*index)
		drawer.DrawString(line)
	}
	frame.rgba = rgba
	return frame, nil
}

func DrawNativePuppetChoices(frame IndexedFrame, choices []string) (IndexedFrame, error) {
	if len(frame.Palette) <= 0xfa {
		return IndexedFrame{}, fmt.Errorf("puppet choice text palette has %d entries, want at least 251", len(frame.Palette))
	}
	face, err := nativeSubtitleFace()
	if err != nil {
		return IndexedFrame{}, err
	}
	background, err := frame.rgbaImage()
	if err != nil {
		return IndexedFrame{}, err
	}
	rgba := image.NewRGBA(background.Bounds())
	draw.Draw(rgba, rgba.Bounds(), background, image.Point{}, draw.Src)
	textColor := image.NewUniform(frame.Palette[0xfa])
	drawer := font.Drawer{Dst: rgba, Src: textColor, Face: face}
	for index, choice := range choices {
		drawer.Dot = fixed.P(8, 280+24*index)
		drawer.DrawString(choice)
	}
	frame.rgba = rgba
	return frame, nil
}

func DrawNativePuppetChoiceBevel(frame IndexedFrame, index int) (IndexedFrame, error) {
	if index < 0 || index >= 4 || len(frame.Palette) <= 0xfb {
		return IndexedFrame{}, fmt.Errorf("puppet choice bevel index %d or palette is invalid", index)
	}
	background, err := frame.rgbaImage()
	if err != nil {
		return IndexedFrame{}, err
	}
	rgba := image.NewRGBA(background.Bounds())
	draw.Draw(rgba, rgba.Bounds(), background, image.Point{}, draw.Src)
	pen := color.RGBAModel.Convert(frame.Palette[0xfb]).(color.RGBA)
	rect := image.Rect(0, 264+24*index, 512, 288+24*index).Intersect(rgba.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if x-rect.Min.X >= 3 && rect.Max.X-1-x >= 3 && y-rect.Min.Y >= 3 && rect.Max.Y-1-y >= 3 {
				continue
			}
			offset := rgba.PixOffset(x, y)
			rgba.Pix[offset] ^= pen.R
			rgba.Pix[offset+1] ^= pen.G
			rgba.Pix[offset+2] ^= pen.B
		}
	}
	frame.rgba = rgba
	return frame, nil
}

func nativeSubtitleFace() (font.Face, error) {
	nativeSubtitleFont.once.Do(func() {
		parsed, err := opentype.Parse(gobold.TTF)
		if err != nil {
			nativeSubtitleFont.err = err
			return
		}
		nativeSubtitleFont.face, nativeSubtitleFont.err = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 9, DPI: 96, Hinting: font.HintingFull})
	})
	if nativeSubtitleFont.err != nil {
		return nil, fmt.Errorf("load subtitle font: %w", nativeSubtitleFont.err)
	}
	return nativeSubtitleFont.face, nil
}

func nativeSubtitleLines(text string, face font.Face) []string {
	if font.MeasureString(face, text).Ceil() < 0x1f0 {
		return []string{text}
	}
	for index := len(text) - 1; index > 0; index-- {
		if text[index] == ' ' && font.MeasureString(face, strings.TrimSpace(text[:index])).Ceil() < 0x1f0 {
			return []string{strings.TrimSpace(text[:index]), strings.TrimSpace(text[index+1:])}
		}
	}
	return []string{text}
}
