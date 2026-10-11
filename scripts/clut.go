package scripts

import (
	"fmt"
	"strconv"
	"strings"

	"redust/render"
)

// ScreenPaletteHost is the live screen palette (DAT_004590FC) and the palettes of
// the open set, stage and puppet that the CLUT name rules read. A nil host on
// GameHostEnv makes every palette name a gap.
type ScreenPaletteHost interface {
	// Named returns the 2048-byte raw palette for "current", "set", "stage" or
	// "puppet" (lower case). ok is false when that source is not open, and for
	// "current" when no palette has been shown yet.
	Named(name string) (raw []byte, ok bool, err error)
	// Apply makes clut (the blended 2048-byte raw CLUT) the live palette, as
	// FUN_0042F060 does. base is the palette source the screen shows the live
	// palette through.
	Apply(clut, base []byte) error
}

// clutResourceName returns the RCDATA name the CLUT registry looks up for a
// name value. FUN_004269F0 sends a number to FUN_0042ED60, which formats "%s%d"
// from the fourCC text "clut" and the number's low word read as signed, so 5
// gives CLUT5 and -1 gives CLUT-1. It sends a string to FUN_0042ECC0, which
// formats "%s.%s" from "clut" and the name, so "black" gives CLUT.BLACK. FUN_00441668
// then upper-cases the result. It changes only a to z on its default path, which
// assumes DAT_00460A68 is zero (not decoded). FUN_0042ECC0 formats into a 32-byte
// buffer, so a string over 26 bytes overflows it; that is undefined natively and
// reports ok false here.
func clutResourceName(name ScriptValue) (string, bool) {
	switch name.Kind {
	case 4:
		return clutUpper("clut" + strconv.Itoa(int(int16(name.Int)))), true
	case 3:
		if len(name.Text) > 26 {
			return "", false
		}
		return clutUpper("clut." + name.Text), true
	}
	return "", false
}

// clutUpper maps a to z to A to Z and leaves every other byte alone.
func clutUpper(text string) string {
	bytes := []byte(text)
	for index, c := range bytes {
		if 'a' <= c && c <= 'z' {
			bytes[index] = c - 'a' + 'A'
		}
	}
	return string(bytes)
}

// clutSource says where the port would have to read a mixclut name from, for the
// gap message. FUN_004269F0 tries the string names "current", "set", "stage" and
// "puppet" first, against the live palette and the open set, stage and puppet,
// and falls back to the registry.
func clutSource(name ScriptValue) string {
	if name.Kind == 3 {
		switch upper := clutUpper(name.Text); upper {
		case "CURRENT":
			return "the live screen palette"
		case "SET", "STAGE", "PUPPET":
			return "the open " + strings.ToLower(upper) + " palette or its registry name"
		}
	}
	resource, ok := clutResourceName(name)
	if !ok {
		return "a registry name over 26 bytes (undefined natively)"
	}
	return "registry resource " + resource
}

// clutPalette resolves one mixclut name to a 2048-byte raw CLUT, following the
// order of FUN_004269F0. A number names a registry CLUT<n>, and the registry
// resources are not readable from the user's assets, so it is a gap. The
// strings "current", "set", "stage" and "puppet" name the live palette and the
// open sources. "set", "stage" and "puppet" fall through to the registry when
// their source is not open, which is undefined natively, so that is a gap too.
// Any other string is a registry name. The only one the port can supply is
// CLUT.BLACK, from render.BlackPaletteRaw: its RGB is zero in entries 1..255
// (2026-10-10 evidence), and its entry 0 RGB is overwritten by the apply.
func (h *GameHost) clutPalette(name ScriptValue) ([]byte, bool, error) {
	if h.Env.Palette == nil {
		return nil, false, nil
	}
	if name.Kind == 3 {
		switch upper := clutUpper(name.Text); upper {
		case "CURRENT", "SET", "STAGE", "PUPPET":
			return h.Env.Palette.Named(strings.ToLower(upper))
		}
	}
	resource, ok := clutResourceName(name)
	if !ok || resource != "CLUT.BLACK" {
		return nil, false, nil
	}
	return render.BlackPaletteRaw(), true, nil
}

// clutValue evaluates one mixclut argument at position and releases its string
// register, returning the value and the position after it.
func clutValue(call *ScriptCall, position int) (ScriptValue, int, uint16, error) {
	record, consumed, status, err := call.Eval(position)
	if err != nil || status != 0 {
		return ScriptValue{}, 0, status, err
	}
	value, status, err := call.Interpreter.Value(record)
	if err != nil || status != 0 {
		return ScriptValue{}, 0, status, err
	}
	return value, position + consumed, 0, nil
}

// mixclutStatement runs mixclut(name, name, lo, hi, t), command 12082 (FUN_00425E90).
// The arguments are read in the native order, so a malformed call returns the
// status the native readers give: 0x1C for a missing comma, 0x0E for a name that
// is neither a string nor a number and for lo, hi or t that is not a number, and
// 2 for a missing closing parenthesis. The first name is the source: the result
// starts as a copy of its CLUT, and entries lo..hi move toward the second name's
// CLUT by t/255 (render.MixPaletteRaw). The result is applied to the live screen
// palette (FUN_0042F060). The palette base is the source name's palette, the set
// for "set" and the live palette for "current"; a black registry name is never
// the base.
func mixclutStatement(h *GameHost, call *ScriptCall) (int, uint16, error) {
	if !hostOpen(call) {
		return 0, ScriptStatusMalformed, nil
	}
	first, next, status, err := clutValue(call, call.Start+2)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(next) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	second, next, status, err := clutValue(call, next+1)
	if err != nil || status != 0 {
		return 0, status, err
	}
	for _, name := range [2]ScriptValue{first, second} {
		if name.Kind != 3 && name.Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
	}
	if call.Kind(next) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	lo, next, status, err := clutValue(call, next+1)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(next) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	hi, next, status, err := clutValue(call, next+1)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(next) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	t, next, status, err := clutValue(call, next+1)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if lo.Kind != 4 || hi.Kind != 4 || t.Kind != 4 {
		return 0, ScriptStatusWrongType, nil
	}
	if call.Kind(next) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	from, okFrom, err := h.clutPalette(first)
	if err != nil {
		return 0, 0, err
	}
	to, okTo, err := h.clutPalette(second)
	if err != nil {
		return 0, 0, err
	}
	if !okFrom || !okTo {
		return 0, 0, fmt.Errorf("%w: mixclut needs %s and %s, and the port cannot resolve both",
			ErrHostOpcodeUnimplemented, clutSource(first), clutSource(second))
	}
	blended, err := render.MixPaletteRaw(from, to, lo.Int, hi.Int, t.Int)
	if err != nil {
		return 0, 0, err
	}
	base := from
	if first.Kind == 3 && clutUpper(first.Text) == "BLACK" {
		base = to
	}
	if err := h.Env.Palette.Apply(blended, base); err != nil {
		return 0, 0, err
	}
	return next - call.Start + 1, 0, nil
}
