package scripts

import "fmt"

// SceneButton is one row of a flat's button block, the rows pointinbutton
// searches. Name is the row's text (the Pascal name at row +0x10, decoded by
// the caller) and Rect the row's four shorts at +4, in the order FUN_0042E340
// reads them: rect[0] and rect[2] bound the low word of a point and rect[1] and
// rect[3] its high word. The port's assets.SceneHandler names those four Top,
// Left, Bottom and Right in that order.
type SceneButton struct {
	Name string
	Rect [4]int16
}

// pointInButton is FUN_00412740, pointinbutton 20043. Verified from the cached
// decompile, in the order the native runs it:
//
//	if (DAT_00459A00 == 0) return 0x0B;            // no stage, before any argument
//	FUN_00417100: three comma separated arguments; a missing comma is 0x1C
//	FUN_00421FC0(arg1): arg1 must be a string, else 0x0E
//	FUN_00413530(arg1): the flat by name; unknown gives 0x0A
//	FUN_00421FC0(arg2): arg2 must be a string, else 0x0E
//	FUN_00413630(arg2): the button by name in that flat; unknown gives 0x0A
//	if (arg3 is not numeric) return 0x0E;         // checked after both lookups
//	result: a type-2 boolean, FUN_0042E340(arg3, row rectangle)
//
// The argument count is only partly traced: fewer than three is 0x1C, the
// missing-comma status. A surplus argument is not traced and returns the port's
// generic malformed status.
func (h *GameHost) pointInButton(call *ScriptCall) (Record, int, uint16, error) {
	if h.Env.StageOpen == nil || h.Env.FlatButtons == nil {
		return Record{}, 0, 0, fmt.Errorf("%w: pointinbutton", ErrHostOpcodeUnimplemented)
	}
	if !h.Env.StageOpen() {
		return Record{}, 0, StatusNoActiveStage, nil
	}
	args, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	switch {
	case len(args) < 3:
		return Record{}, 0, ScriptStatusMissingComma, nil
	case len(args) > 3:
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	if args[0].Kind != 3 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	buttons, found, err := h.Env.FlatButtons(args[0].Text)
	if err != nil {
		return Record{}, 0, 0, err
	}
	if !found {
		return Record{}, 0, StatusNameNotFound, nil
	}
	if args[1].Kind != 3 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	button, found := findSceneButton(buttons, args[1].Text)
	if !found {
		return Record{}, 0, StatusNameNotFound, nil
	}
	if args[2].Kind != 4 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	hit := pointInNativeRect(uint32(args[2].Int), button.Rect)
	return Record{Kind: 2, Data: boolWord(hit)}, consumed, 0, nil
}

// findSceneButton returns the first row whose name matches. FUN_00413720 walks
// the block from row 0 and stops at the first FUN_0042E5B0 match, which is an
// ASCII case-insensitive comparison.
func findSceneButton(buttons []SceneButton, name string) (SceneButton, bool) {
	for _, button := range buttons {
		if EqualASCIIFold(button.Name, name) {
			return button, true
		}
	}
	return SceneButton{}, false
}

// pointInNativeRect is FUN_0042E340 on a packed point and four shorts, read the
// way the native reads them:
//
//	if (point.high < rect[1]) return false;
//	if (rect[3] <= point.high) return false;
//	if (point.low < rect[0]) return false;
//	return point.low < rect[2];
//
// The near edges are inclusive and the far edges exclusive. pointx reads the
// high word as x (FUN_00415CB0), so rect[1] and rect[3] are a row's x bounds.
// This is the same test the click path applies in assets.Stage.HitTestSceneHandler.
func pointInNativeRect(point uint32, rect [4]int16) bool {
	low, high := int16(uint16(point)), int16(uint16(point>>16))
	if high < rect[1] {
		return false
	}
	if rect[3] <= high {
		return false
	}
	if low < rect[0] {
		return false
	}
	return low < rect[2]
}
