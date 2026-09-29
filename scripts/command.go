package scripts

import "fmt"

// Verified command-statement dispatch, from the live Ghidra analysis of
// FUN_00424890 (DF386.EXE, preferred base 0x00400000).
//
// FUN_00424890 is the command statement dispatcher. Its verified structure is:
//
//	if (records[start+1].Kind != 0x0FB2) return 2;      // require "(" after the opcode
//	op := records[start].Kind
//	if op < 0x3E82 {
//	    if op == 0x3E81 { ... }                        // 16001 special case
//	    switch op - 0x2EE1 { ... }                     // 12001 .. 12088
//	} else {
//	    switch op - 0x3E82 { ... }                     // 16002 .. 16053
//	}
//	// shared epilogue, on status 0:
//	if records[start+2+argCount].Kind != 0x0FB3) return 2;   // require ")"
//	*consumed = argCount + 3;                                // opcode + "(" + args + ")"
//	return 1;                                                // default: unhandled
//
// Both switches are contiguous from their base, so the covered ranges are
// 12001..12088 and 16002..16053 with no holes. An earlier extraction of this
// table wrongly reported the ranges as starting at 12012 and 16013 because the
// case pattern only matched `case 0xN:` while Ghidra writes `case 0:` for
// single digits, which silently dropped cases 0 through 10.
const (
	// commandOpenParen and commandCloseParen are the verified 0x0FB2 / 0x0FB3
	// record kinds that frame every command statement.
	commandOpenParen  uint16 = 4018
	commandCloseParen uint16 = 4019

	commandPrimaryBase   = 12001 // 0x2EE1
	commandPrimaryFirst  = 12001
	commandPrimaryLast   = 12088
	commandSpecialOpcode = 16001 // 0x3E81, handled outside both switches
	commandSecondaryBase = 16002 // 0x3E82
	commandSecondaryLow  = 16002
	commandSecondaryHigh = 16053
)

// CommandRange classifies an opcode against FUN_00424890's verified switch ranges.
type CommandRange uint8

const (
	// CommandRangeNone means the opcode is not dispatched by FUN_00424890.
	CommandRangeNone CommandRange = iota
	// CommandRangePrimary is the contiguous 12001..12088 switch.
	CommandRangePrimary
	// CommandRangeSpecial is the single 16001 case.
	CommandRangeSpecial
	// CommandRangeSecondary is the contiguous 16002..16053 switch.
	CommandRangeSecondary
)

// ClassifyCommandRange reports which verified switch, if any, handles opcode.
func ClassifyCommandRange(opcode uint16) CommandRange {
	switch {
	case opcode >= commandPrimaryFirst && opcode <= commandPrimaryLast:
		return CommandRangePrimary
	case opcode == commandSpecialOpcode:
		return CommandRangeSpecial
	case opcode >= commandSecondaryLow && opcode <= commandSecondaryHigh:
		return CommandRangeSecondary
	default:
		return CommandRangeNone
	}
}

// The complete verified case-to-callee tables live in handlers.go:
// commandPrimaryHandlers, commandSecondaryHandlers, valuePrimaryHandlers and
// valueSecondaryHandlers. They are transcribed in full rather than as a subset,
// so an extraction error surfaces as a table-count assertion failure instead of
// a silent range error. Use CommandCommandHandler for the FUN_00424890 side and
// CommandValueHandler for the FUN_004137B0 side.

// CommandHandler returns the verified native handler for opcode as the native
// dispatcher chain would reach it: the value dispatcher is tried first for the
// 16000 band, matching verified FUN_004192D0, then the command dispatcher.
// A dispatchable opcode always resolves; an undispatchable one returns
// ok with an empty name.
func CommandHandler(opcode uint16) (handler string, dispatched bool) {
	if handler, found := CommandHandlerFor(opcode, PreferValue); found {
		return handler, true
	}
	return "", false
}

// CommandCommandHandler returns the command-dispatcher side of an opcode, which
// for the 16000 band is the fallback FUN_00424890 would reach only after the
// value dispatcher fails.
func CommandCommandHandler(opcode uint16) (handler string, found bool) {
	if h, ok := commandPrimaryHandlers[opcode]; ok {
		return normalizeHandlerName(h), true
	}
	if h, ok := commandSecondaryHandlers[opcode]; ok {
		return normalizeHandlerName(h), true
	}
	return "", false
}

// CommandValueHandler returns the value-dispatcher side of an opcode.
func CommandValueHandler(opcode uint16) (handler string, found bool) {
	if h, ok := valuePrimaryHandlers[opcode]; ok {
		return normalizeHandlerName(h), true
	}
	if h, ok := valueSecondaryHandlers[opcode]; ok {
		return normalizeHandlerName(h), true
	}
	return "", false
}

// CommandHandlerName returns the opcode's script name, for evidence logging.
func CommandHandlerName(opcode uint16) string {
	for name, id := range opcodeIDs {
		if id == opcode && len(name) > 0 && name[0] >= 33 && name[0] <= 126 {
			return name
		}
	}
	return ""
}

// UnhandledCommandOpcodes lists the opcodes that reach FUN_00424890 but fall
// through both of its switches to the default that returns native status 1.
// There is no interior gap: both switches are contiguous from their base, so
// only the band edges are unhandled.
//
//	12000        below the 12001 primary switch
//	12089        above the 12088 primary switch
//	16054        above the 16053 secondary switch
//
// Verified FUN_004192D0 sends the 12000 band to FUN_00424890 with no fallback,
// so these three are genuinely undispatched. The 16000 band is different: it is
// tried in the value dispatcher FUN_004137B0 first, whose own switch covers
// 16001..16053, and 16054 is outside both dispatchers.
func UnhandledCommandOpcodes(opcode uint16) bool {
	switch opcode {
	case 12000, 12089, 16054:
		return true
	default:
		return false
	}
}

// OwnedByValueDispatcher reports whether an opcode in the 16000 band is routed
// by the verified FUN_004192D0 ordering to the value dispatcher FUN_004137B0
// before the command dispatcher is tried.
func OwnedByValueDispatcher(opcode uint16) bool {
	return opcode >= 16000 && opcode <= 16053
}

var (
	// ErrCommandFrameOpen reports a command statement without the required "(".
	ErrCommandFrameOpen = fmt.Errorf("command statement is missing the opening parenthesis record")
	// ErrCommandFrameClose reports a command statement without the required ")".
	ErrCommandFrameClose = fmt.Errorf("command statement is missing the closing parenthesis record")
)

// CommandCallFrame validates and measures a command statement's call frame using
// the verified FUN_00424890 prologue and epilogue.
//
// Records are laid out as: opcode, "(", argCount argument records, ")". The
// verified prologue rejects a statement whose record after the opcode is not "("
// with status 2, and the verified epilogue rejects a missing trailing ")" with
// status 2. On success the native function reports argCount + 3 records
// consumed, which counts the opcode, the "(", the arguments and the ")".
//
// argCount is supplied by the caller because native handlers receive the argument
// cursor as psVar6 = &records[start+2] and return the number of argument records
// they consumed through the out parameter.
func CommandCallFrame(records []Record, start, argCount int) (consumed int, status uint16, err error) {
	if start < 0 || start+1 >= len(records) {
		return -1, 2, ErrCommandFrameOpen
	}
	if records[start+1].Kind != commandOpenParen {
		return -1, 2, ErrCommandFrameOpen
	}
	if argCount < 0 {
		return -1, 2, ErrCommandFrameClose
	}
	closeAt := start + 2 + argCount
	if closeAt >= len(records) {
		return -1, 2, ErrCommandFrameClose
	}
	if records[closeAt].Kind != commandCloseParen {
		return -1, 2, ErrCommandFrameClose
	}
	return argCount + 3, 0, nil
}

// MeasureCommandCall is a convenience wrapper that locates the terminating ")"
// itself and returns the native record count. It is intended for static analysis
// of shipped scripts, where the argument count is implicit.
func MeasureCommandCall(records []Record, start int) (consumed int, status uint16, err error) {
	if start < 0 || start+1 >= len(records) {
		return -1, 2, ErrCommandFrameOpen
	}
	if records[start+1].Kind != commandOpenParen {
		return -1, 2, ErrCommandFrameOpen
	}
	for index := start + 2; index < len(records); index++ {
		if records[index].Kind == commandCloseParen {
			return index - start + 1, 0, nil
		}
	}
	return -1, 2, ErrCommandFrameClose
}
