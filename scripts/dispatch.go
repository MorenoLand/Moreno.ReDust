package scripts

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type DispatchRoute uint8

type StatementResultDispatcher func(start int) (recordsConsumed int32, status uint16, err error)

type ResultCodeSession struct {
	CodeSession
	DispatchResult StatementResultDispatcher
}

func (s *ResultCodeSession) DispatchStatementResult(start int) (int32, uint16, error) {
	if s == nil || s.CodeSession == nil || s.DispatchResult == nil {
		return -1, 0, ErrNoStatementResultDispatcher
	}
	return s.DispatchResult(start)
}

const (
	RouteOther DispatchRoute = iota
	// RouteUndispatched means the opcode falls in a dispatch band whose
	// dispatcher returns native status 1 for it. Verified: the 12000 band has
	// no fallback, so 12000..12011 are unroutable there.
	RouteUndispatched
	Route00424890
	Route004137B0
	Route004137B0Then00424890
	Route0041D680
	Route0041D6F0
)

// Verified dispatch topology, from the live Ghidra decompilation of
// FUN_004192D0 (the event-script classifier):
//
//	if (11999 < op && op < 0x2F3A) return FUN_00424890(...);        // 12000..12089
//	if (19999 < op && op < 0x4E8E) return FUN_004137B0(...);        // 20000..20109
//	if (15999 < op && op < 0x3EB7) {                                // 16000..16054
//	    u = FUN_004137B0(...);
//	    if ((short)u == 0) return u;                                // value route won
//	    FUN_00418FB0(param_4, &DAT_00459AD0);
//	    *param_4 = -1;
//	    return FUN_00424890(...);                                    // otherwise command
//	}
//	if (op == 0x0FA2) return FUN_0041D680(...);
//	if (op == 0x0FBD) return FUN_0041D6F0(...);
//	// fallback: resolve an identifier and assign, else evaluate
//
// Two consequences follow:
//
//   - The 16000 band is tried in the value dispatcher FIRST and only falls
//     back to the command dispatcher on failure. FUN_004137B0's own switches
//     are based at 0x3E81 (16001, covering 16001..16053) and 0x4E22 (20002,
//     covering 20002..20108).
//   - The 12000 range has NO fallback. FUN_00424890's primary switch is
//     contiguous from 0x2EE1, so it covers 12001..12088 and only the band
//     edges 12000 and 12089 return native status 1.
const (
	// dispatchCommandBandHigh bounds the 12000 range handed straight to the
	// command dispatcher.
	dispatchCommandBandHigh = 0x2F3A
	// dispatchValueBandHigh bounds the 20000 range.
	dispatchValueBandHigh = 0x4E8E
	// dispatchHybridBandHigh bounds the 16000 range, which tries the value
	// dispatcher before the command dispatcher.
	dispatchHybridBandHigh = 0x3EB7
)

func ClassifyDispatch(opcode uint16) DispatchRoute {
	switch {
	case opcode > 11999 && opcode < uint16(dispatchCommandBandHigh):
		// Verified: this band goes to FUN_00424890 with no fallback, so only
		// opcodes inside its verified switches are actually handled.
		if ClassifyCommandRange(opcode) != CommandRangeNone {
			return Route00424890
		}
		return RouteUndispatched
	case opcode > 19999 && opcode < uint16(dispatchValueBandHigh):
		return Route004137B0
	case opcode > 15999 && opcode < uint16(dispatchHybridBandHigh):
		return Route004137B0Then00424890
	case opcode == 0x0fa2:
		return Route0041D680
	case opcode == 0x0fbd:
		return Route0041D6F0
	default:
		return RouteOther
	}
}

func DispatchPlayMovie(runtime *ConditionRuntime, frame ConditionFrame, start int, playbackFlag *uint16, player func([]byte) (uint16, error)) (int32, uint16, error) {
	if runtime == nil || runtime.Expressions == nil {
		return -1, 0, fmt.Errorf("movie expression runtime is unavailable")
	}
	if start < 0 || start+2 >= len(frame.Program.Records) || frame.Program.Records[start].Kind != 12017 || frame.Program.Records[start+1].Kind != 4018 {
		return -1, 2, nil
	}
	value, consumed, status, err := runtime.EvaluateValue(frame.Context, frame.Program, start+2, frame.VariableScope)
	if err != nil || uint16(status) != 0 {
		return -1, uint16(status), err
	}
	strings := runtime.Strings
	if strings == nil {
		strings = runtime.Expressions.Strings
	}
	if strings == nil {
		return -1, 0, fmt.Errorf("movie string registers are unavailable")
	}
	result := Record{Kind: binary.LittleEndian.Uint16(value[:2]), Data: binary.LittleEndian.Uint32(value[2:6]), Tail: binary.LittleEndian.Uint16(value[6:8])}
	name, stringStatus, err := strings.Load(result)
	if err != nil || stringStatus != 0 {
		return -1, stringStatus, err
	}
	if playbackFlag == nil {
		return -1, 0, fmt.Errorf("movie playback flag is unavailable")
	}
	if player == nil {
		return -1, 0, fmt.Errorf("movie player is unavailable")
	}
	*playbackFlag = 0
	playStatus, err := player(name)
	if err != nil || playStatus != 0 {
		return -1, playStatus, err
	}
	next := start + 2 + int(consumed)
	if next < 0 || next >= len(frame.Program.Records) || frame.Program.Records[next].Kind != 4019 {
		return -1, 2, nil
	}
	return int32(next - start + 1), 0, nil
}

type MouseDownAction struct {
	FlatTarget       int
	VisualEffect     uint16
	Duration         int
	ReturnIdentifier string
}

// ErrUnresolvedFlatTarget reports a gotoflat argument form that the verified
// evidence does not cover. A numeric argument is a flat transition; an
// identifier argument is the dynamic return that restores the flat captured by
// an earlier assignment in the same handler.
var ErrUnresolvedFlatTarget = errors.New("mousedown gotoflat argument form is unsupported")

func MouseDownFlatAction(program Program) (MouseDownAction, bool, error) {
	start, err := FindCode(program, "mousedown")
	if errors.Is(err, ErrCodeNotFound) {
		return MouseDownAction{}, false, nil
	}
	if err != nil {
		return MouseDownAction{}, false, err
	}
	length, err := NextCodeOffset(program.Records, start)
	if err != nil {
		return MouseDownAction{}, false, err
	}
	end := start + int(length)
	if length < 0 {
		end = len(program.Records) - 1
	}
	action, found := MouseDownAction{}, false
	for i := start + 1; i < end; i++ {
		switch program.Records[i].Kind {
		case LookupOpcode("gotoflat"):
			if i+3 >= end || program.Records[i+1].Kind != 4018 || program.Records[i+3].Kind != 4019 {
				return MouseDownAction{}, false, fmt.Errorf("mousedown gotoflat statement at record %d does not match the verified call form", i)
			}
			switch program.Records[i+2].Kind {
			case 4:
				target := int32(program.Records[i+2].Data)
				if target < 1 {
					return MouseDownAction{}, false, fmt.Errorf("mousedown gotoflat target %d is invalid", target)
				}
				action.FlatTarget, found = int(target-1), true
			case 5:
				// Verified dynamic return. NEW.FLT resource 24 captures the
				// current flat into a local and returns to it after savegame:
				//   r20  arg = currentflat()
				//   r31  gotoflat(1)
				//   r43  savegame("dust 0.3")
				//   r48  gotoflat(arg)
				identifier, err := program.IdentifierPascal(i + 2)
				if err != nil || len(identifier) < 2 {
					return MouseDownAction{}, false, fmt.Errorf("mousedown gotoflat identifier at record %d is unreadable: %w", i, err)
				}
				if action.ReturnIdentifier != "" {
					return MouseDownAction{}, false, fmt.Errorf("mousedown has more than one dynamic gotoflat return, at record %d", i)
				}
				action.ReturnIdentifier = string(identifier[1:])
			default:
				return MouseDownAction{}, false, fmt.Errorf("%w: record %d argument kind %d", ErrUnresolvedFlatTarget, i, program.Records[i+2].Kind)
			}
		case LookupOpcode("visualeffect"):
			if i+5 >= end || program.Records[i+1].Kind != 4018 || program.Records[i+3].Kind != 4020 || program.Records[i+4].Kind != 4 || program.Records[i+5].Kind != 4019 {
				return MouseDownAction{}, false, fmt.Errorf("mousedown visualeffect statement at record %d does not match the verified call form", i)
			}
			effect := program.Records[i+2].Kind
			if effect < 24001 || effect > 24014 {
				return MouseDownAction{}, false, fmt.Errorf("mousedown visualeffect id %d is invalid", effect)
			}
			duration := int32(program.Records[i+4].Data)
			if duration < 1 {
				duration = 1
			} else if duration > 1000 {
				duration = 1000
			}
			action.VisualEffect, action.Duration = effect, int(duration)
		}
	}
	return action, found, nil
}
