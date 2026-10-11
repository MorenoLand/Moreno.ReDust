package scripts

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
