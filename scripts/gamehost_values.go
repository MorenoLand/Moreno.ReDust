package scripts

import "fmt"

// nativeArgs reads count comma separated value arguments the way the native
// readers do: FUN_00417070 for two, FUN_00417100 for three and FUN_004171C0 for
// four. Each argument is one expression, evaluated by FUN_004220A0 and decoded
// to a value. A comma must follow every argument but the last, and a missing
// one is status 0x1C. The index returned is the record after the last argument.
// The caller checks that record for the closing parenthesis once its own status
// is zero, as the dispatcher does, so a surplus argument is never evaluated.
func nativeArgs(call *ScriptCall, count int) ([]ScriptValue, int, uint16, error) {
	position := call.Start + 2
	values := make([]ScriptValue, 0, count)
	for index := 0; index < count; index++ {
		record, consumed, status, err := call.Eval(position)
		if err != nil || status != 0 {
			return nil, 0, status, err
		}
		value, status, err := call.Interpreter.Value(record)
		if err != nil || status != 0 {
			return nil, 0, status, err
		}
		values = append(values, value)
		position += consumed
		if index < count-1 {
			if call.Kind(position) != opComma {
				return nil, 0, ScriptStatusMissingComma, nil
			}
			position++
		}
	}
	return values, position, 0, nil
}

// nativeTurnStep is FUN_004117D0: one step of a turn from param1 toward param2,
// moving by at most param3, on a circle whose wrap value is param4. The port keeps
// the native's 16-bit arithmetic, so the comparisons and sums wrap as the shorts
// do. The disassembly was not read; the transcription follows the decompile.
func nativeTurnStep(p1, p2, p3, p4 int16) int16 {
	if p1 < p2 {
		if int16(p2-p1) < (p4-p2)+p1 {
			step := p1 + p3
			if p2 < step {
				return p2
			}
			return step
		}
		step := p1 - p3
		if step < 0 {
			wrapped := step + p4
			if wrapped < p2 {
				return p2
			}
			return wrapped
		}
		return step
	}
	if int16(p1-p2) < (p4-p1)+p2 {
		step := p1 - p3
		if step < p2 {
			return p2
		}
		return step
	}
	step := p1 + p3
	if p4 <= step {
		step -= p4
		if p2 < step {
			return p2
		}
	}
	return step
}

// calcTurn is calcturn, FUN_00415330, value 20072. FUN_00417100 reads three
// arguments, and each must be a number (0x0E) once all three are read. The result
// is FUN_004117D0 on the low byte of argument 1, the low byte of argument 2, the
// low word of argument 3 raised to at least 1, and the wrap value 255, as a number
// with status 0.
func (h *GameHost) calcTurn(call *ScriptCall) (Record, int, uint16, error) {
	if !hostOpen(call) {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	args, end, status, err := nativeArgs(call, 3)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	for _, arg := range args {
		if arg.Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
	}
	step := int16(args[2].Int)
	if step < 1 {
		step = 1
	}
	turn := nativeTurnStep(int16(uint8(args[0].Int)), int16(uint8(args[1].Int)), step, 0xff)
	if call.Kind(end) != opClose {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	return Record{Kind: 4, Data: uint32(int32(turn))}, end + 1 - call.Start, 0, nil
}

// starXYZ is starxyz, FUN_00414FB0, value 20073. FUN_00417070 reads a name and a
// selector. The name must be a string (0x0E), and FUN_0041B850 then searches the
// open set's coordinate table for it, primary name or alias in row order. With no
// set open (DAT_00459A24 clear) or no row with the name, the status is 0x0A. The
// selector must be a number (0x0E): 1, 2 and 3 give x, y and z as numbers, and 4
// gives the packed point (x in the high word, y in the low). Any other selector is
// 0x0E. The lookup goes through the ResolveStar hook for the open set. Whether the
// hook's table is the native DAT_00459A44 table is recorded in the evidence ledger
// as partly verified.
func (h *GameHost) starXYZ(call *ScriptCall) (Record, int, uint16, error) {
	if !hostOpen(call) {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	args, end, status, err := nativeArgs(call, 2)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if args[0].Kind != 3 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	if h.Env.ResolveStar == nil {
		return Record{}, 0, 0, fmt.Errorf("%w: starxyz has no star table", ErrHostOpcodeUnimplemented)
	}
	var position [3]int16
	found := false
	if _, open := h.setRect(); open {
		position, found = h.Env.ResolveStar(h.currentSet(), args[0].Text)
	}
	if !found {
		return Record{}, 0, StatusNameNotFound, nil
	}
	if args[1].Kind != 4 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	var value int32
	switch args[1].Int {
	case 1:
		value = int32(position[0])
	case 2:
		value = int32(position[1])
	case 3:
		value = int32(position[2])
	case 4:
		value = packPoint(position)
	default:
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	if call.Kind(end) != opClose {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	return Record{Kind: 4, Data: uint32(value)}, end + 1 - call.Start, 0, nil
}

// typeName is type, FUN_00415AC0, value 20050. FUN_004220A0 evaluates one
// expression, and the name of its record kind is returned as a new string: 2 is
// "logic" (DAT_0045D4EC), 3 is "text" (DAT_0045D4FC), 4 is "number" (DAT_0045D4F4),
// and any other kind is "unknown" (DAT_0045D37C). FUN_00427FC0 writes each Pascal
// length byte, and FUN_00421F60 stores the string in a register, which reports
// 0x36 when all twenty are in use. The texts are the engine string pool entries at
// 0x0045D4ED, 0x0045D4FD, 0x0045D4F5 and 0x0045D37D (scripts/stringpool.go), and
// the native copies them from those addresses. A kind-2, -3 or -4 argument is
// released the way the port's other argument readers release it; whether the
// native frees a discarded argument's register was not traced.
func (h *GameHost) typeName(call *ScriptCall) (Record, int, uint16, error) {
	if !hostOpen(call) {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	record, consumed, status, err := call.Eval(call.Start + 2)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	name := "unknown"
	switch record.Kind {
	case 2:
		name = "logic"
	case 3:
		name = "text"
	case 4:
		name = "number"
	}
	if record.Kind >= 2 && record.Kind <= 4 {
		if _, status, err := call.Interpreter.Value(record); err != nil || status != 0 {
			return Record{}, 0, status, err
		}
	}
	result, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: name})
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	end := call.Start + 2 + consumed
	if call.Kind(end) != opClose {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	return result, end + 1 - call.Start, 0, nil
}
