package scripts

import "fmt"

// Call frames for the small builtins that take at most one argument. The
// reference splits each one between its handler and its dispatcher. The
// dispatcher (FUN_00424890 for commands, FUN_004137B0 for values) requires "("
// straight after the opcode, and after a handler returns status 0 it requires
// ")" at the record the handler counted, then reports the records through it.
// A handler that takes an argument evaluates it with FUN_004220A0, which reports
// how many records the expression used.

// builtinArgument reads "(" and the one expression after it, evaluated the way
// FUN_004220A0 evaluates an argument. It returns the record and the index of the
// record the dispatcher will require to be ")".
func builtinArgument(call *ScriptCall) (Record, int, uint16, error) {
	if call.Kind(call.Start+1) != commandOpenParen {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	arg, consumed, status, err := call.Eval(call.Start + 2)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	return arg, call.Start + 2 + consumed, 0, nil
}

// builtinFrameEnd is the dispatcher's epilogue for a handler that returned status
// 0: the record at closeAt must be ")", and the statement then consumed every
// record through it, which is the argument count plus three.
func builtinFrameEnd(call *ScriptCall, closeAt int) (int, uint16) {
	if call.Kind(closeAt) != commandCloseParen {
		return 0, ScriptStatusMalformed
	}
	return closeAt + 1 - call.Start, 0
}

// debuggerCommand is debugger() 12040, FUN_00426370. The handler writes a zero
// argument count and returns status 0, and nothing else: the port has no debugger,
// so the call is only its frame. Any argument leaves the ")" check failing.
func debuggerCommand(call *ScriptCall) (int, uint16, error) {
	if call.Kind(call.Start+1) != commandOpenParen {
		return 0, ScriptStatusMalformed, nil
	}
	consumed, status := builtinFrameEnd(call, call.Start+2)
	return consumed, status, nil
}

// keyAbortsCommand is keyaborts(bool), opcode 16045, the command side of
// FUN_00426530. A statement reaches the value side first (FUN_004137B0 runs
// FUN_00414F50 for a bare call), so keyaborts() succeeds and stores nothing. With
// one argument the command stores the low word of a boolean in DAT_00459996 and
// answers 0x0E for any other kind. The store happens before the ")" check, so a
// malformed tail still leaves the flag written.
func (h *GameHost) keyAbortsCommand(call *ScriptCall) (int, uint16, error) {
	if call.Kind(call.Start+1) != commandOpenParen {
		return 0, ScriptStatusMalformed, nil
	}
	if call.Kind(call.Start+2) == commandCloseParen {
		return 3, 0, nil
	}
	arg, closeAt, status, err := builtinArgument(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if arg.Kind != ValueTypeBool {
		return 0, ScriptStatusWrongType, nil
	}
	h.KeyAborts = int16(uint16(arg.Data))
	consumed, status := builtinFrameEnd(call, closeAt)
	return consumed, status, nil
}

// keyAbortsValue is the no-argument reader FUN_00414F50. It returns a type-2
// boolean from DAT_00459996. The assembly is `MOVSX ECX, word ptr [0x459996]`, so
// the slot is a 16-bit word sign-extended, not the 32-bit global the decompile
// suggests.
func (h *GameHost) keyAbortsValue(call *ScriptCall) (Record, int, uint16, error) {
	if call.Kind(call.Start+1) != commandOpenParen {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	consumed, status := builtinFrameEnd(call, call.Start+2)
	if status != 0 {
		return Record{}, 0, status, nil
	}
	return Record{Kind: ValueTypeBool, Data: uint32(int32(h.KeyAborts))}, consumed, 0, nil
}

// findFileValue is findfile(name) 20067, FUN_00415780. The argument is evaluated
// with FUN_004220A0 and must be a string, since FUN_00421FC0 answers 0x0E for any
// other kind. The result is a type-2 boolean that is true when the name exists.
//
// The reference probes eight candidates, the path prefix table's slots 1 through 8
// with the name appended, and stops at the first that exists. The port has one
// asset root, so every candidate names the same file and a single lookup gives the
// answer the loop would. That collapse is a port decision, not a verified native
// behaviour: it holds only while the eight prefixes all denote the asset root,
// which the reference does not show.
func (h *GameHost) findFileValue(call *ScriptCall) (Record, int, uint16, error) {
	arg, closeAt, status, err := builtinArgument(call)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	name, status, err := call.Interpreter.Value(arg)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if name.Kind != 3 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	if h.Env.FileExists == nil {
		return Record{}, 0, 0, fmt.Errorf("%w: findfile has no asset lookup", ErrHostOpcodeUnimplemented)
	}
	found := h.Env.FileExists(name.Text)
	consumed, status := builtinFrameEnd(call, closeAt)
	if status != 0 {
		return Record{}, 0, status, nil
	}
	return Record{Kind: ValueTypeBool, Data: boolWord(found)}, consumed, 0, nil
}
