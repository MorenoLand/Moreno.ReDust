package render

import (
	"image"
	"image/color"
	"image/draw"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// The native game raises Win32 common dialogs and message boxes for Save, Open
// and its Yes/No questions (FUN_0042DB70 GetSaveFileNameA, FUN_0042DCF0
// GetOpenFileNameA, FUN_0042DFB0/FUN_0042E030 MessageBoxA). ReDust has no
// operating-system dialogs, so these draw the same controls over the current
// frame in the colours of the options flat.
var (
	dialogShadow = color.RGBA{R: 30, G: 16, B: 8, A: 255}
	dialogEdge   = color.RGBA{R: 74, G: 44, B: 22, A: 255}
	dialogBrown  = color.RGBA{R: 143, G: 89, B: 48, A: 255}
	dialogLight  = color.RGBA{R: 196, G: 140, B: 88, A: 255}
	dialogCream  = color.RGBA{R: 243, G: 230, B: 196, A: 255}
	dialogField  = color.RGBA{R: 255, G: 250, B: 232, A: 255}
	dialogSelect = color.RGBA{R: 143, G: 89, B: 48, A: 255}
	dialogInk    = color.RGBA{R: 58, G: 30, B: 12, A: 255}
	dialogLabel  = color.RGBA{R: 255, G: 240, B: 208, A: 255}
)

// DialogView describes one modal box. Lines are wrapped message text; Input,
// List and Buttons are optional.
type DialogView struct {
	Title string
	Lines []string
	// InputLabel/Input/ShowInput draw a single-line edit box; a caret follows
	// the text when Caret is set.
	ShowInput  bool
	InputLabel string
	Input      string
	Caret      bool
	// ListLabel names the file list; List holds the visible entries starting
	// at ListTop, ListSelected indexes List (-1 for none) and ListRows is the
	// number of rows the box shows.
	ShowList     bool
	ListLabel    string
	List         []string
	ListTop      int
	ListSelected int
	ListRows     int
	// FilterNote is the "Saved games (.RTD)" line under the list.
	FilterNote string
	Buttons    []string
	// Pressed is the button index drawn pushed in, or -1.
	Pressed int
	// Default is the button the Enter key activates; it is drawn with a
	// heavier edge.
	Default int
}

// DialogGeometry is where DrawDialog put each control, for hit testing.
type DialogGeometry struct {
	Box     image.Rectangle
	Input   image.Rectangle
	Rows    []image.Rectangle
	Buttons []image.Rectangle
	// ScrollUp and ScrollDown are the list arrows.
	ScrollUp, ScrollDown image.Rectangle
}

const (
	dialogLineHeight   = 16
	dialogRowHeight    = 16
	dialogPad          = 12
	dialogButtonHeight = 24
)

func textWidth(face font.Face, text string) int {
	return font.MeasureString(face, text).Ceil()
}

// WrapDialogText breaks text into lines no wider than width pixels.
func WrapDialogText(text string, width int) ([]string, error) {
	face, err := nativeSubtitleFace()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		current := ""
		for _, word := range strings.Fields(paragraph) {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if current != "" && textWidth(face, candidate) > width {
				lines = append(lines, current)
				current = word
			} else {
				current = candidate
			}
		}
		lines = append(lines, current)
	}
	return lines, nil
}

// LayoutDialog computes the box and control rectangles for a view on a frame
// of the given size.
func LayoutDialog(view DialogView, bounds image.Rectangle) DialogGeometry {
	width := 320
	if view.ShowList || view.ShowInput {
		width = 352
	}
	height := dialogPad
	if view.Title != "" {
		height += dialogLineHeight + 6
	}
	height += len(view.Lines) * dialogLineHeight
	if len(view.Lines) > 0 {
		height += 8
	}
	if view.ShowInput {
		height += dialogLineHeight + 24 + 8
	}
	if view.ShowList {
		height += dialogLineHeight + view.ListRows*dialogRowHeight + 8 + dialogLineHeight + 8
	}
	height += dialogButtonHeight + dialogPad
	box := image.Rect(0, 0, width, height).Add(image.Pt((bounds.Dx()-width)/2, (bounds.Dy()-height)/2-8))
	geometry := DialogGeometry{Box: box}
	y := box.Min.Y + dialogPad
	if view.Title != "" {
		y += dialogLineHeight + 6
	}
	y += len(view.Lines) * dialogLineHeight
	if len(view.Lines) > 0 {
		y += 8
	}
	if view.ShowInput {
		y += dialogLineHeight
		geometry.Input = image.Rect(box.Min.X+dialogPad, y, box.Max.X-dialogPad, y+22)
		y += 24 + 8
	}
	if view.ShowList {
		y += dialogLineHeight
		for row := 0; row < view.ListRows; row++ {
			geometry.Rows = append(geometry.Rows, image.Rect(box.Min.X+dialogPad, y+row*dialogRowHeight, box.Max.X-dialogPad-18, y+(row+1)*dialogRowHeight))
		}
		geometry.ScrollUp = image.Rect(box.Max.X-dialogPad-16, y, box.Max.X-dialogPad, y+16)
		geometry.ScrollDown = image.Rect(box.Max.X-dialogPad-16, y+view.ListRows*dialogRowHeight-16, box.Max.X-dialogPad, y+view.ListRows*dialogRowHeight)
		y += view.ListRows*dialogRowHeight + 8 + dialogLineHeight + 8
	}
	if count := len(view.Buttons); count > 0 {
		buttonWidth := 84
		gap := 12
		total := count*buttonWidth + (count-1)*gap
		x := box.Min.X + (width-total)/2
		for index := 0; index < count; index++ {
			geometry.Buttons = append(geometry.Buttons, image.Rect(x, y, x+buttonWidth, y+dialogButtonHeight))
			x += buttonWidth + gap
		}
	}
	return geometry
}

func fillRect(img *image.RGBA, rect image.Rectangle, ink color.Color) {
	draw.Draw(img, rect.Intersect(img.Bounds()), image.NewUniform(ink), image.Point{}, draw.Src)
}

func bevelRect(img *image.RGBA, rect image.Rectangle, fill, light, dark color.Color, pressed bool) {
	fillRect(img, rect, fill)
	top, bottom := light, dark
	if pressed {
		top, bottom = dark, light
	}
	fillRect(img, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+2), top)
	fillRect(img, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+2, rect.Max.Y), top)
	fillRect(img, image.Rect(rect.Min.X, rect.Max.Y-2, rect.Max.X, rect.Max.Y), bottom)
	fillRect(img, image.Rect(rect.Max.X-2, rect.Min.Y, rect.Max.X, rect.Max.Y), bottom)
}

func drawText(img *image.RGBA, face font.Face, text string, x, baseline int, ink color.Color) {
	drawer := font.Drawer{Dst: img, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(x, baseline)}
	drawer.DrawString(text)
}

// clipText shortens text with a trailing ellipsis-free cut so it fits width.
func clipText(face font.Face, text string, width int) string {
	for len(text) > 0 && textWidth(face, text) > width {
		text = text[:len(text)-1]
	}
	return text
}

// DrawDialog paints the view over the frame and returns the new frame and the
// rectangles of its controls.
func DrawDialog(frame IndexedFrame, view DialogView) (IndexedFrame, DialogGeometry, error) {
	face, err := nativeSubtitleFace()
	if err != nil {
		return IndexedFrame{}, DialogGeometry{}, err
	}
	background, err := frame.rgbaImage()
	if err != nil {
		return IndexedFrame{}, DialogGeometry{}, err
	}
	rgba := image.NewRGBA(background.Bounds())
	draw.Draw(rgba, rgba.Bounds(), background, image.Point{}, draw.Src)
	geometry := LayoutDialog(view, rgba.Bounds())
	box := geometry.Box
	fillRect(rgba, box.Add(image.Pt(4, 4)), dialogShadow)
	bevelRect(rgba, box, dialogCream, dialogLight, dialogEdge, false)
	fillRect(rgba, box.Inset(2), dialogBrown)
	fillRect(rgba, box.Inset(5), dialogCream)
	y := box.Min.Y + dialogPad
	if view.Title != "" {
		fillRect(rgba, image.Rect(box.Min.X+5, y-4, box.Max.X-5, y+dialogLineHeight+2), dialogBrown)
		drawText(rgba, face, view.Title, box.Min.X+dialogPad, y+dialogLineHeight-4, dialogLabel)
		y += dialogLineHeight + 6
	}
	for _, line := range view.Lines {
		drawText(rgba, face, line, box.Min.X+dialogPad, y+dialogLineHeight-4, dialogInk)
		y += dialogLineHeight
	}
	if view.ShowInput {
		drawText(rgba, face, view.InputLabel, box.Min.X+dialogPad, geometry.Input.Min.Y-4, dialogInk)
		bevelRect(rgba, geometry.Input, dialogField, dialogEdge, dialogLight, true)
		text := clipText(face, view.Input, geometry.Input.Dx()-12)
		if len(text) < len(view.Input) {
			text = view.Input[len(view.Input)-len(text):]
			for textWidth(face, text) > geometry.Input.Dx()-12 {
				text = text[1:]
			}
		}
		drawText(rgba, face, text, geometry.Input.Min.X+5, geometry.Input.Min.Y+15, dialogInk)
		if view.Caret {
			x := geometry.Input.Min.X + 5 + textWidth(face, text) + 1
			fillRect(rgba, image.Rect(x, geometry.Input.Min.Y+4, x+1, geometry.Input.Max.Y-4), dialogInk)
		}
	}
	if view.ShowList {
		if len(geometry.Rows) > 0 {
			drawText(rgba, face, view.ListLabel, box.Min.X+dialogPad, geometry.Rows[0].Min.Y-4, dialogInk)
			listBox := image.Rect(geometry.Rows[0].Min.X, geometry.Rows[0].Min.Y, geometry.ScrollUp.Max.X, geometry.Rows[len(geometry.Rows)-1].Max.Y)
			bevelRect(rgba, listBox.Inset(-2), dialogField, dialogEdge, dialogLight, true)
			fillRect(rgba, listBox, dialogField)
			for row, rect := range geometry.Rows {
				entry := view.ListTop + row
				if entry < 0 || entry >= len(view.List) {
					continue
				}
				ink := color.Color(dialogInk)
				if entry == view.ListSelected {
					fillRect(rgba, rect, dialogSelect)
					ink = dialogLabel
				}
				drawText(rgba, face, clipText(face, view.List[entry], rect.Dx()-8), rect.Min.X+4, rect.Min.Y+dialogRowHeight-4, ink)
			}
			bevelRect(rgba, geometry.ScrollUp, dialogBrown, dialogLight, dialogEdge, false)
			bevelRect(rgba, geometry.ScrollDown, dialogBrown, dialogLight, dialogEdge, false)
			drawText(rgba, face, "^", geometry.ScrollUp.Min.X+5, geometry.ScrollUp.Min.Y+13, dialogLabel)
			drawText(rgba, face, "v", geometry.ScrollDown.Min.X+5, geometry.ScrollDown.Min.Y+12, dialogLabel)
			drawText(rgba, face, view.FilterNote, box.Min.X+dialogPad, listBox.Max.Y+dialogLineHeight, dialogInk)
		}
	}
	for index, label := range view.Buttons {
		if index >= len(geometry.Buttons) {
			break
		}
		rect := geometry.Buttons[index]
		pressed := index == view.Pressed
		if index == view.Default {
			fillRect(rgba, rect.Inset(-2), dialogEdge)
		}
		bevelRect(rgba, rect, dialogBrown, dialogLight, dialogEdge, pressed)
		offset := 0
		if pressed {
			offset = 1
		}
		drawText(rgba, face, label, rect.Min.X+(rect.Dx()-textWidth(face, label))/2+offset, rect.Min.Y+16+offset, dialogLabel)
	}
	frame.rgba = rgba
	return frame, geometry, nil
}
