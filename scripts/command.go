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
//	    switch op - 0x2EE1 { ... }                     // 12012 .. 12088
//	} else {
//	    switch op - 0x3E82 { ... }                     // 16013 .. 16053
//	}
//	// shared epilogue, on status 0:
//	if records[start+2+argCount].Kind != 0x0FB3) return 2;   // require ")"
//	*consumed = argCount + 3;                                // opcode + "(" + args + ")"
//	return 1;                                                // default: unhandled
//
// Note the verified range starts at 12012, not 12000. Opcodes below 12012 and
// above 12088 are NOT handled here and return status 1.
const (
	// commandOpenParen and commandCloseParen are the verified 0x0FB2 / 0x0FB3
	// record kinds that frame every command statement.
	commandOpenParen  uint16 = 4018
	commandCloseParen uint16 = 4019

	commandPrimaryBase   = 12001 // 0x2EE1
	commandPrimaryFirst  = 12012
	commandPrimaryLast   = 12088
	commandSpecialOpcode = 16001 // 0x3E81, handled outside both switches
	commandSecondaryBase = 16002 // 0x3E82
	commandSecondaryLow  = 16013
	commandSecondaryHigh = 16053
)

// CommandRange classifies an opcode against FUN_00424890's verified switch ranges.
type CommandRange uint8

const (
	// CommandRangeNone means the opcode is not dispatched by FUN_00424890.
	CommandRangeNone CommandRange = iota
	// CommandRangePrimary is the 12012..12088 switch.
	CommandRangePrimary
	// CommandRangeSpecial is the single 16001 case.
	CommandRangeSpecial
	// CommandRangeSecondary is the 16013..16053 switch.
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

// commandHandlers is the verified FUN_00424890 case-to-callee table. The values
// are native function names recorded so each Go route keeps a traceable evidence
// tag. It is a partial map on purpose: only the cases the shipped scripts and the
// converted Go port actually depend on are transcribed, and ClassifyCommandRange
// is the authority for range membership.
var commandHandlers = map[uint16]string{
	// 12000 range (switch on opcode - 0x2EE1)
	12013: "FUN_0040BFA0", // opencastfile
	12016: "FUN_0040CB40", // sendtoactor
	12017: "FUN_00426880", // playmovie
	12019: "FUN_0040E250", // opentrackfile
	12034: "FUN_0041A180", // sendtoscene
	12038: "FUN_00425E30", // clut
	12039: "FUN_00426230", // cursor
	12042: "FUN_00408120", // closepuppetfile
	12055: "FUN_00420CC0", // sendtoprop
	12056: "FUN_00420110", // openshopfile
	12062: "FUN_00411AD0", // gotoflat
	12068: "FUN_00412A70", // sendtobutton
	12069: "FUN_00412EC0", // sendtoflat
	12070: "FUN_00413210", // sendtostage
	12071: "FUN_00426580", // quit
	12074: "FUN_00408700", // puppetgrab
	12077: "FUN_00422C80", // savegame
	12078: "FUN_00422D40", // opengame
	12079: "FUN_00425B10", // notedialog

	// 16000 range (switch on opcode - 0x3E82)
	16015: "FUN_0041F910", // propvisible
	16029: "FUN_004199A0", // currentscene
}

// CommandHandler returns the verified native handler name for opcode and whether
// FUN_00424890 dispatches it. A dispatchable opcode whose handler is not yet
// transcribed returns ok with an empty name, so callers can distinguish
// "not handled here" from "handled, not yet converted".
func CommandHandler(opcode uint16) (handler string, dispatched bool) {
	handler = commandHandlers[opcode]
	return handler, ClassifyCommandRange(opcode) != CommandRangeNone
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

// CommandGap records a verified hole in FUN_00424890's coverage. The shipped
// boot script uses opcodes inside these holes, so they are dispatched by a
// different native function that has not been located yet. They are tracked
// explicitly so they are never mistaken for "handled" or for "unknown".
//
//	12000..12011  below the primary switch; message (12001) is in here
//	16002..16012  between the 16001 special case and the secondary switch;
//	              setvisible (16007), currentstage (16008), path (16009) and
//	              result (16010) are in here
var commandGaps = [...]struct{ low, high uint16 }{
	{12000, 12011},
	{16002, 16012},
}

// InCommandGap reports whether opcode falls in a verified FUN_00424890 dispatch
// gap, meaning some other native dispatcher owns it.
func InCommandGap(opcode uint16) bool {
	for _, gap := range commandGaps {
		if opcode >= gap.low && opcode <= gap.high {
			return true
		}
	}
	return false
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
