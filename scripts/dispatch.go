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
	Route00424890
	Route004137B0
	Route004137B0Then00424890
	Route0041D680
	Route0041D6F0
)

func ClassifyDispatch(opcode uint16) DispatchRoute {
	switch {
	case opcode > 11999 && opcode < 0x2f3a:
		return Route00424890
	case opcode > 19999 && opcode < 0x4e8e:
		return Route004137B0
	case opcode > 15999 && opcode < 0x3eb7:
		return Route004137B0Then00424890
	case opcode == 0x0fa2 || opcode == 0x0fa3:
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
	FlatTarget   int
	VisualEffect uint16
	Duration     int
}

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
			if i+3 >= end || program.Records[i+1].Kind != 4018 || program.Records[i+2].Kind != 4 || program.Records[i+3].Kind != 4019 {
				return MouseDownAction{}, false, fmt.Errorf("mousedown gotoflat statement at record %d does not match the verified call form", i)
			}
			target := int32(program.Records[i+2].Data)
			if target < 1 {
				return MouseDownAction{}, false, fmt.Errorf("mousedown gotoflat target %d is invalid", target)
			}
			action.FlatTarget, found = int(target-1), true
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
