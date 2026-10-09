package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"sync"
	"unicode/utf8"

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
	text = nativeText(text)
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

func DrawNativeTextAt(frame IndexedFrame, text string, position image.Point) (IndexedFrame, error) {
	return DrawNativeTextAtColor(frame, text, position, color.White)
}

func DrawNativeTextAtColor(frame IndexedFrame, text string, position image.Point, ink color.Color) (IndexedFrame, error) {
	text = nativeText(text)
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
	drawer := font.Drawer{Dst: rgba, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(position.X, position.Y)}
	drawer.DrawString(text)
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
		drawer.DrawString(nativeText(choice))
	}
	frame.rgba = rgba
	return frame, nil
}

func DrawNativePuppetChoiceBevel(frame IndexedFrame, index int) (IndexedFrame, error) {
	if index < 0 || index >= 5 || len(frame.Palette) <= 0xfb {
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

// macRomanHigh maps bytes 0x80..0xFF of the game's text to Unicode. The game
// stores its dialogue in Mac Roman: byte 0xD5 is the apostrophe in "I'd"
// (Windows-1252 would read it as a capital O with a tilde).
var macRomanHigh = [128]rune{
	0x00C4, 0x00C5, 0x00C7, 0x00C9, 0x00D1, 0x00D6, 0x00DC, 0x00E1,
	0x00E0, 0x00E2, 0x00E4, 0x00E3, 0x00E5, 0x00E7, 0x00E9, 0x00E8,
	0x00EA, 0x00EB, 0x00ED, 0x00EC, 0x00EE, 0x00EF, 0x00F1, 0x00F3,
	0x00F2, 0x00F4, 0x00F6, 0x00F5, 0x00FA, 0x00F9, 0x00FB, 0x00FC,
	0x2020, 0x00B0, 0x00A2, 0x00A3, 0x00A7, 0x2022, 0x00B6, 0x00DF,
	0x00AE, 0x00A9, 0x2122, 0x00B4, 0x00A8, 0x2260, 0x00C6, 0x00D8,
	0x221E, 0x00B1, 0x2264, 0x2265, 0x00A5, 0x00B5, 0x2202, 0x2211,
	0x220F, 0x03C0, 0x222B, 0x00AA, 0x00BA, 0x03A9, 0x00E6, 0x00F8,
	0x00BF, 0x00A1, 0x00AC, 0x221A, 0x0192, 0x2248, 0x2206, 0x00AB,
	0x00BB, 0x2026, 0x00A0, 0x00C0, 0x00C3, 0x00D5, 0x0152, 0x0153,
	0x2013, 0x2014, 0x201C, 0x201D, 0x2018, 0x2019, 0x00F7, 0x25CA,
	0x00FF, 0x0178, 0x2044, 0x20AC, 0x2039, 0x203A, 0xFB01, 0xFB02,
	0x2021, 0x00B7, 0x201A, 0x201E, 0x2030, 0x00C2, 0x00CA, 0x00C1,
	0x00CB, 0x00C8, 0x00CD, 0x00CE, 0x00CF, 0x00CC, 0x00D3, 0x00D4,
	0xFFFD, 0x00D2, 0x00DA, 0x00DB, 0x00D9, 0x0131, 0x02C6, 0x02DC,
	0x00AF, 0x02D8, 0x02D9, 0x02DA, 0x00B8, 0x02DD, 0x02DB, 0x02C7,
}

// nativeText turns script text into UTF-8: text that already is UTF-8 stays
// as it is; otherwise bytes at 0x80 and above are read through macRomanHigh.
func nativeText(text string) string {
	if utf8.ValidString(text) {
		return text
	}
	var out strings.Builder
	for index := 0; index < len(text); index++ {
		b := text[index]
		if b < 0x80 {
			out.WriteByte(b)
			continue
		}
		out.WriteRune(macRomanHigh[b-0x80])
	}
	return out.String()
}
