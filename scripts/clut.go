package scripts

import (
	"fmt"
	"strconv"
	"strings"
)

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

// clutSource says where the port would have to read a mixclut name from. FUN_004269F0
// tries the string names "current", "set", "stage" and "puppet" first, against the
// live palette and the open set, stage and puppet, and falls back to the registry.
// The live palette is not modelled, and the script host is not given the open
// set, stage or puppet palettes, so those names are described as sources the
// port lacks.
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
// 2 for a missing closing parenthesis. The native blend (render.MixPaletteRaw)
// writes its result to the live screen palette (FUN_0042F060). The port has no
// live palette, since each render.IndexedFrame carries its own, so a well-formed
// call is a host gap, as it was before this slice.
func mixclutStatement(call *ScriptCall) (int, uint16, error) {
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
	return 0, 0, fmt.Errorf("%w: mixclut needs %s and %s, and the port has no live screen palette to write the blend to",
		ErrHostOpcodeUnimplemented, clutSource(first), clutSource(second))
}
