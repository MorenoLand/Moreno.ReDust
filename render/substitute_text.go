package render

import (
	"fmt"
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// substituteTextPixelSize is the em size in pixels when drawstring's nHeight is
// zero. FUN_0042CE00 then passes CreateFontA a height of zero, which makes GDI
// choose its default size. That default is not decoded, so this value is a guess.
const substituteTextPixelSize = 11

// substituteFaceKey selects one of the two bundled faces at one pixel size.
type substituteFaceKey struct {
	bold bool
	px   int
}

var substituteFaces = struct {
	mu    sync.Mutex
	faces map[substituteFaceKey]font.Face
}{faces: map[substituteFaceKey]font.Face{}}

// substituteInkIndex is the palette index the text is drawn in. drawstring calls
// FUN_0042CEC0(0), so the context's colour index (byte +0x14) is 0. On the DC
// FUN_0042AEA0 creates, FUN_0042CC00 sets the text colour to the COLORREF of table
// entry 0, which FUN_004282C0 gives that DC's colour table; on any other DC it sets
// PALETTEINDEX(0). Either way the text is palette index 0. The RGB of entry 0 is
// black in the port's palette (PaletteState.SetFixedEntry). The native RGB comes
// from the curve tables, which are not traced, so only the index is verified.
const substituteInkIndex = 0

// substituteCoverage is the glyph coverage at which a pixel takes the ink. Text
// is aliased, as GDI's raster text is on a palette DC, so the glyph edge is a
// threshold and not a blend.
const substituteCoverage = 128

// DrawSubstituteText draws drawstring's text on frame with its baseline-left
// point at baseline. It is a substitute, not the native face. drawstring names
// "Raven Digital" weight 400 for selector 0x10 and "Arial" weight 700 otherwise
// (FUN_0042CE00). Neither face is in the game's assets, and the reference's
// Windows host has no Raven face (evidence ledger, 2026-10-11). The closest
// bundled faces are golang.org/x/image's Go Regular for the Raven branch and Go
// Bold for the Arial branch. Both are sans-serif, as Arial is, but their glyph
// shapes and advance widths are not Arial's, and neither is Raven's. The size is
// -nHeight pixels when nHeight is negative (a character height, as GDI reads it),
// |nHeight| otherwise, and substituteTextPixelSize when it is zero.
//
// The ink is written in the frame's own index space: a paletted frame keeps its
// palette and gets palette index substituteInkIndex at each glyph pixel, and a
// true-colour frame gets the colour of that palette entry. Pixels outside the
// glyphs are unchanged, and frame's Pixels are copied, not written in place.
//
// The text is drawn as given: it is already the ASCII that scripts/gamehost_text.go
// sanitised, so the Mac Roman mapping nativeText applies to dialogue is not used.
func DrawSubstituteText(frame IndexedFrame, text string, baseline image.Point, bold bool, nHeight int32) (IndexedFrame, error) {
	px := substituteTextPixelSize
	if nHeight != 0 {
		px = int(nHeight)
		if px < 0 {
			px = -px
		}
	}
	face, err := substituteFace(bold, px)
	if err != nil {
		return IndexedFrame{}, err
	}
	if frame.Width <= 0 || frame.Height <= 0 {
		return IndexedFrame{}, fmt.Errorf("substitute text needs a frame, got %dx%d", frame.Width, frame.Height)
	}
	coverage := image.NewAlpha(image.Rect(0, 0, frame.Width, frame.Height))
	drawer := font.Drawer{Dst: coverage, Src: image.Opaque, Face: face, Dot: fixed.P(baseline.X, baseline.Y)}
	drawer.DrawString(text)
	if frame.rgba != nil {
		if frame.rgba.Bounds().Dx() != frame.Width || frame.rgba.Bounds().Dy() != frame.Height {
			return IndexedFrame{}, fmt.Errorf("true-color frame does not match %dx%d", frame.Width, frame.Height)
		}
		ink := color.RGBA{A: 255}
		if len(frame.Palette) == 256 {
			ink = color.RGBAModel.Convert(frame.Palette[substituteInkIndex]).(color.RGBA)
		}
		rgba := image.NewRGBA(frame.rgba.Bounds())
		copy(rgba.Pix, frame.rgba.Pix)
		for y := 0; y < frame.Height; y++ {
			for x := 0; x < frame.Width; x++ {
				if coverage.AlphaAt(x, y).A >= substituteCoverage {
					rgba.SetRGBA(x, y, ink)
				}
			}
		}
		frame.rgba = rgba
		return frame, nil
	}
	if _, err := frame.Image(); err != nil {
		return IndexedFrame{}, err
	}
	pixels := append([]byte(nil), frame.Pixels...)
	for index, cover := range coverage.Pix {
		if cover >= substituteCoverage {
			pixels[index] = substituteInkIndex
		}
	}
	frame.Pixels = pixels
	return frame, nil
}

// substituteFace returns the bundled face at the given pixel size, parsing it
// once per size.
func substituteFace(bold bool, px int) (font.Face, error) {
	substituteFaces.mu.Lock()
	defer substituteFaces.mu.Unlock()
	key := substituteFaceKey{bold: bold, px: px}
	if face, ok := substituteFaces.faces[key]; ok {
		return face, nil
	}
	data := goregular.TTF
	if bold {
		data = gobold.TTF
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse substitute drawstring face: %w", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, fmt.Errorf("make substitute drawstring face: %w", err)
	}
	substituteFaces.faces[key] = face
	return face, nil
}
