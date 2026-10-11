package scripts

import "fmt"

// machineTypeName is what machinetype() returns. FUN_00415720 passes the
// Pascal string at 0x0045DA88 to FUN_004279B0, which first writes the string's
// length there (FUN_00427FC0) and then copies it into a string register
// (FUN_00421F60). The C string at 0x0045DA89 is "WIN" in the platform region
// transcription (scripts/native/platform.go, PlatformRegionFirstString). The
// value is a constant in the binary, not a probe of the machine.
const machineTypeName = "WIN"

// hostOpen reports whether a builtin is followed by its open parenthesis. Every
// builtin dispatcher checks this before the handler runs.
func hostOpen(call *ScriptCall) bool {
	return call.Kind(call.Start+1) == opOpen
}

// hostClose finishes a builtin whose handler takes no arguments. The handler
// leaves its consumed count at zero, so the record after the open parenthesis
// must be the close, or the call is malformed. It returns the records consumed,
// counting the opcode and both parentheses.
func hostClose(call *ScriptCall) (int, uint16) {
	if call.Kind(call.Start+2) != opClose {
		return 0, ScriptStatusMalformed
	}
	return 3, 0
}

// hostCommand handles showcursor and hidecursor. FUN_0042B7E0 (showcursor) and
// FUN_0042B7F0 (hidecursor) call ShowCursor(1) and ShowCursor(0), then run the
// message pump FUN_0042BD80. The count is the Win32 display count: the cursor
// shows while it is zero or more, so showcursor adds one and hidecursor takes
// one away. The Win32 count rule is documented API behaviour and is not decoded
// from the binary. The native count starts at zero, which is also unverified. The
// message pump has no port equivalent and is not reproduced.
func (h *GameHost) hostCommand(name string, call *ScriptCall) (int, uint16, bool, error) {
	switch name {
	case "showcursor", "hidecursor":
		if !hostOpen(call) {
			return 0, ScriptStatusMalformed, true, nil
		}
		// The handler acts before the close check (LAB_00425731 tests the
		// consumed count after the handler returns status 0), so the count moves
		// even when an argument follows and the call then fails.
		if name == "showcursor" {
			h.cursorCount++
		} else {
			h.cursorCount--
		}
		if h.Env.CursorVisible != nil {
			h.Env.CursorVisible(h.cursorCount >= 0)
		}
		consumed, status := hostClose(call)
		return consumed, status, true, nil
	}
	return 0, 0, false, nil
}

// hostValue handles machinetype, setwidth and setheight.
//
// machinetype is FUN_00415720, which takes no arguments. Its string register
// status is the status it returns: 0x36 when all twenty string slots are in
// use (FUN_00421F60).
//
// setwidth and setheight are FUN_0041B450 and FUN_0041B4A0. They return status
// 0x28 with no set loaded (DAT_00459A24 clear), and otherwise the 16-bit word
// at the set's metadata offset 0x26 (setwidth) or 0x28 (setheight), sign
// extended into a number.
func (h *GameHost) hostValue(name string, call *ScriptCall) (Record, int, uint16, bool, error) {
	switch name {
	case "machinetype":
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: machineTypeName})
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		return record, consumed, 0, true, nil
	case "setwidth", "setheight":
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		if h.Env.SetSize == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: %s has no open set", ErrHostOpcodeUnimplemented, name)
		}
		width, height, open := h.Env.SetSize()
		if !open {
			return Record{}, 0, 0x28, true, nil
		}
		value := height
		if name == "setwidth" {
			value = width
		}
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		return Record{Kind: 4, Data: uint32(int32(value))}, consumed, 0, true, nil
	}
	return Record{}, 0, 0, false, nil
}
