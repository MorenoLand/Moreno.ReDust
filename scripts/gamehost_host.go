package scripts

import (
	"fmt"

	"redust/assets"
)

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

// hostCommand handles showcursor, hidecursor, exportclut and mixclut.
// FUN_0042B7E0 (showcursor) and FUN_0042B7F0 (hidecursor) call ShowCursor(1)
// and ShowCursor(0), then run the
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
	case "exportclut":
		// FUN_004279A0 is `return 0` (XOR AX,AX; RET): no arguments, no effect,
		// status 0. The command dispatcher never writes the consumed count on this
		// path, so the close check reads an uninitialised slot. The port applies
		// the zero-argument rule the other no-argument builtins use, which is not
		// verified for this opcode. No shipped script calls exportclut.
		if !hostOpen(call) {
			return 0, ScriptStatusMalformed, true, nil
		}
		consumed, status := hostClose(call)
		return consumed, status, true, nil
	case "mixclut":
		consumed, status, err := mixclutStatement(h, call)
		return consumed, status, true, err
	case "drawstring":
		consumed, status, err := h.drawStringCommand(call)
		return consumed, status, true, err
	}
	return 0, 0, false, nil
}

// hostValue handles machinetype, setwidth, setheight, freemem, machinespeed,
// cacheinfo, tick, button and commandkey.
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
	case "freemem":
		// freemem is FUN_00416510, value 20027. It takes no arguments and returns
		// status 0 on every path: FUN_0042C420 calls GlobalMemoryStatus and returns
		// the least of the available page file, the available virtual and the total
		// physical bytes, which assets.NativeAvailableMemory computes. The reading is
		// live, so the number is host-dependent by design and comes from the
		// FreeMemory hook. The close check runs before the reading, so a call with an
		// argument reports 2 whatever the memory is.
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		if h.Env.FreeMemory == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: freemem has no memory reading", ErrHostOpcodeUnimplemented)
		}
		pageFile, virtual, totalPhysical := h.Env.FreeMemory()
		return Record{Kind: 4, Data: assets.NativeAvailableMemory(pageFile, virtual, totalPhysical)}, consumed, 0, true, nil
	case "machinespeed":
		// machinespeed is FUN_00415750, value 20066. It takes no arguments.
		// FUN_004279D0 loads KERNEL32, resolves GetSystemInfo and returns the host's
		// dwProcessorType, or 0x182 (386) when the export is missing. The result is a
		// machine property, not a speed, and it has no port equivalent on every
		// platform, so the value comes from the ProcessorType hook. The argument
		// checks and status are the native ones.
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		if h.Env.ProcessorType == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: machinespeed has no processor type", ErrHostOpcodeUnimplemented)
		}
		return Record{Kind: 4, Data: h.Env.ProcessorType()}, consumed, 0, true, nil
	case "cacheinfo":
		// cacheinfo is FUN_00416F10, value 20082. It returns 0x28 with no set open
		// (DAT_00459A24, the SetRect gate) before it evaluates its argument.
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		if _, open := h.setRect(); !open {
			return Record{}, 0, 0x28, true, nil
		}
		record, consumed, status, err := h.cacheInfo(call)
		return record, consumed, status, true, err
	case "tick":
		// tick is FUN_00415E60, value 20008. FUN_0042B700 pumps the messages
		// (FUN_0042BD80, not reproduced) and returns timeGetTime() * 3 / 50 as a
		// dword, which is NativeFrameUnits(NativeTickMilliseconds()), the clock
		// Env.Ticks reads. The result is a number with status 0. The handler
		// consumes no arguments, so the call is "(" and ")" only.
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		if h.Env.Ticks == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: tick has no clock", ErrHostOpcodeUnimplemented)
		}
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		return Record{Kind: 4, Data: h.Env.Ticks()}, consumed, 0, true, nil
	case "button":
		// button is FUN_00415DF0, value 20005. FUN_0042C7C0 pumps the messages (not
		// reproduced) and returns GetKeyState(VK_LBUTTON) < 0, so the result is a
		// type-2 boolean that is true while the left button is down. The handler
		// consumes no arguments, so the call is "(" and ")" only.
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		if h.Env.LeftButton == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: button has no mouse state", ErrHostOpcodeUnimplemented)
		}
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		return Record{Kind: 2, Data: boolWord(h.Env.LeftButton())}, consumed, 0, true, nil
	case "commandkey":
		// commandkey is FUN_00416C20, value 20060. It takes no arguments. Its
		// handler zeroes the consumed count and returns FUN_0042EB80(), which is
		// GetKeyState(0x11) < 0: the sign bit of the returned SHORT, so the result
		// is true while a Control key is down (VK_CONTROL, either side, per the
		// Win32 generic key; that API rule is not decoded from the binary). The
		// dispatcher checks "(" before the handler and ")" after it, so the key is
		// read before hostClose. The result is a type-2 boolean with status 0. A
		// nil ControlDown reports up; that is a port default, not a native path.
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		down := h.Env.ControlDown != nil && h.Env.ControlDown()
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		return Record{Kind: 2, Data: boolWord(down)}, consumed, 0, true, nil
	case "optionkey":
		// optionkey is FUN_00416BC0, value 20058. Its handler zeroes the consumed
		// count and returns (int)FUN_0042EB70(). FUN_0042EB70 is `XOR AX,AX; RET`
		// in one block with no branch (cached export), so every path returns 0 and
		// the result is a type-2 boolean that is always false. No key is read. The
		// dispatcher checks "(" before the handler and ")" after it, as for commandkey.
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		return Record{Kind: 2, Data: boolWord(false)}, consumed, 0, true, nil
	case "shiftkey":
		// shiftkey is FUN_00416BF0, value 20059. Its callee FUN_0042EBA0 returns
		// GetKeyState(0x10) < 0 (VK_SHIFT, the same rule as FUN_0042EB80 for
		// commandkey). The handler writes that as a type-2 boolean with status 0,
		// and the key is read before the dispatcher's close check. A nil ShiftKey
		// reports up; that is a port default, not a native path.
		if !hostOpen(call) {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		down := h.Env.ShiftKey != nil && h.Env.ShiftKey()
		consumed, status := hostClose(call)
		if status != 0 {
			return Record{}, 0, status, true, nil
		}
		return Record{Kind: 2, Data: boolWord(down)}, consumed, 0, true, nil
	case "calcturn":
		record, consumed, status, err := h.calcTurn(call)
		return record, consumed, status, true, err
	case "type":
		record, consumed, status, err := h.typeName(call)
		return record, consumed, status, true, err
	case "starxyz":
		record, consumed, status, err := h.starXYZ(call)
		return record, consumed, status, true, err
	case "scenefloor":
		record, consumed, status, err := h.sceneFloor(call)
		return record, consumed, status, true, err
	case "putword":
		record, consumed, status, err := h.putWord(call)
		return record, consumed, status, true, err
	}
	return Record{}, 0, 0, false, nil
}

// cacheInfo reads cacheinfo's one argument and its selector. FUN_00416F10
// evaluates one expression (FUN_004220A0), which must be a number (0x0E
// otherwise). The selector picks a count over the APPL cache: 1 is the manager
// (node) count, 4 the cached rows whose reference count is positive, and 5 the
// rows at zero or below (FUN_00401100 heads the manager list, and each node's
// row count and rows are the port's ResourceCache slots). Selectors 2 and 3 are
// byte totals of native heap blocks (GlobalSize), which the port cannot match,
// so they are a gap. Any other selector is 0x0E, returned before the close
// check, as the native handler does.
func (h *GameHost) cacheInfo(call *ScriptCall) (Record, int, uint16, error) {
	arg, closeAt, status, err := builtinArgument(call)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	value, status, err := call.Interpreter.Value(arg)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if value.Kind != 4 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	switch value.Int {
	case 1, 2, 3, 4, 5:
	default:
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	consumed, status := builtinFrameEnd(call, closeAt)
	if status != 0 {
		return Record{}, 0, status, nil
	}
	if value.Int == 2 || value.Int == 3 {
		return Record{}, 0, 0, fmt.Errorf("%w: cacheinfo(%d) sums native heap blocks", ErrHostOpcodeUnimplemented, value.Int)
	}
	if h.Env.CacheCounts == nil {
		return Record{}, 0, 0, fmt.Errorf("%w: cacheinfo has no cache counts", ErrHostOpcodeUnimplemented)
	}
	nodes, referenced, unreferenced := h.Env.CacheCounts()
	count := nodes
	switch value.Int {
	case 4:
		count = referenced
	case 5:
		count = unreferenced
	}
	return Record{Kind: 4, Data: uint32(count)}, consumed, 0, nil
}
