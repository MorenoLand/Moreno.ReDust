package scripts

import (
	"encoding/binary"
	"fmt"
)

type DispatchRoute uint8

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
