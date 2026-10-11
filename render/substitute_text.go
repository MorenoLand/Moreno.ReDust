package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
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

// DrawSubstituteText draws drawstring's text on frame with its baseline-left
// point at baseline. It is a substitute, not the native face. drawstring names
// "Raven Digital" weight 400 for selector 0x10 and "Arial" weight 700 otherwise
// (FUN_0042CE00). Neither face is in the game's assets, and the reference's
// Windows host has no Raven face (evidence ledger, 2026-10-11). The closest
// bundled faces are golang.org/x/image's
// Go Regular for the Raven branch and Go Bold for the Arial branch. Both are
// sans-serif, as Arial is, but their glyph shapes and advance widths are not
// Arial's, and neither is Raven's. The size is -nHeight pixels when nHeight is
// negative (a character height, as GDI reads it), |nHeight| otherwise, and
// substituteTextPixelSize when it is zero.
//
// The text is drawn as given: it is already the ASCII that scripts/gamehost_text.go
// sanitised, so the Mac Roman mapping nativeText applies to dialogue is not used.
func DrawSubstituteText(frame IndexedFrame, text string, baseline image.Point, bold bool, nHeight int32, ink color.Color) (IndexedFrame, error) {
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
	background, err := frame.rgbaImage()
	if err != nil {
		return IndexedFrame{}, err
	}
	rgba := image.NewRGBA(background.Bounds())
	draw.Draw(rgba, rgba.Bounds(), background, image.Point{}, draw.Src)
	drawer := font.Drawer{Dst: rgba, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(baseline.X, baseline.Y)}
	drawer.DrawString(text)
	frame.rgba = rgba
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
