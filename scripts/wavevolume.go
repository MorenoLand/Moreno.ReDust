package scripts

import (
	"encoding/binary"
	"fmt"
)

func (r *ConditionRuntime) DispatchWaveVolumeStatement(context any, program Program, start int, scope any) (uint16, error) {
	if r == nil || start < 0 || start >= len(program.Records) || program.Records[start].Kind != LookupOpcode("wavevolume") {
		return 0, fmt.Errorf("wavevolume statement index %d is invalid", start)
	}
	if kindAt(program, start+1) != LookupOpcode("(") {
		return 2, nil
	}
	value, consumed, status, err := r.EvaluateValue(context, program, start+2, scope)
	if err != nil || status != 0 {
		return uint16(status), err
	}
	kind, raw := binary.LittleEndian.Uint16(value[:2]), int32(binary.LittleEndian.Uint32(value[2:6]))
	if kind != 4 {
		return 14, nil
	}
	if raw < 0 || raw > 9 {
		return 10, nil
	}
	if kindAt(program, start+2+int(consumed)) != LookupOpcode(")") {
		return 2, nil
	}
	if r.Services.SetWaveVolume == nil {
		return 0, fmt.Errorf("wave volume setter is unavailable")
	}
	return 0, r.Services.SetWaveVolume(int(raw))
}
