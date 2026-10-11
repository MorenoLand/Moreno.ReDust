package scripts

// stageVisibleCommand is stagevisible(bool), opcode 16008, the command side of
// FUN_00412450. The reference runs these steps in order:
//
//	if (DAT_00459A00 == 0) return 0x0B;          // no open stage, before any argument
//	argument = FUN_004220A0(records[start+2])    // its status is returned as is
//	if (argument kind != 2) return 0x0E;         // only a boolean is accepted
//	DAT_00459A02 = low word of the argument;     // stored before the ")" check
//	if (value == 0) FUN_00427EC0 clears the stage bitmap      // not ported
//	else FUN_004014F0, FUN_00427580, FUN_00401670 load the flat's picture // not ported
//	FUN_004055C0(2);                             // redraw level 2, not ported
//	return 0;
//
// The dispatcher has already required "(" after the opcode and, on status 0,
// requires ")" at the record after the argument. The handler returns the
// records consumed through that ")".
func (h *GameHost) stageVisibleCommand(call *ScriptCall) (int, uint16, error) {
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, nil
	}
	if h.Env.StageOpen == nil || !h.Env.StageOpen() {
		return 0, 0x0b, nil
	}
	arg, used, status, err := call.Eval(call.Start + 2)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if arg.Kind != ValueTypeBool {
		return 0, ScriptStatusWrongType, nil
	}
	h.StageVisible = int16(uint16(arg.Data))
	h.stageVisibleEffectsNotPorted(h.StageVisible != 0)
	closeAt := call.Start + 2 + used
	if call.Kind(closeAt) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return closeAt + 1 - call.Start, 0, nil
}

// stageVisibleEffectsNotPorted records, through the Log hook, the stage bitmap
// work that stagevisible performs natively and the port does not hold. The port
// has no stage bitmap and no picture decoder for FUN_00427580, so the flag is
// set and the bitmap is left as it is.
func (h *GameHost) stageVisibleEffectsNotPorted(visible bool) {
	if !visible {
		h.logf("stagevisible(false): FUN_00427EC0 stage bitmap clear not ported; bitmap unchanged")
		return
	}
	h.logf("stagevisible(true): picture load not ported (FUN_004014F0, FUN_00427580, FUN_00401670); bitmap unchanged")
}

// closeStageCommand is closestagefile(), opcode 12061, the command side of
// FUN_004119C0. The reference runs, in order:
//
//	*result = 0
//	if (DAT_00459A00 == 0) return 0x0B;             // no open stage; no argument is read
//	status = FUN_00411E80();                          // leaves the open flat (FUN_00413130, not traced)
//	if status != 0 return status
//	recheck DAT_00459A00 and the guard DAT_00459A04+0xD34; a change returns 0
//	FUN_00411A70(&DAT_00459A00);                      // releases the stage file (FUN_00401670, FUN_004019F0)
//	FUN_00427EC0(bitmap, w*h);                        // frees the stage picture
//	FUN_004055C0(2);                                  // redraw level 2, not ported
//	return 0
//
// The dispatcher requires "(" first and, after a status 0 result, ")" after
// that. The port holds its stage for the whole run and has no open path that
// could bring a stage back, so only the no-stage gate is carried here. With a
// stage open the close is a gap error, which is what the handler did before.
// A nil StageOpen hook is also a gap, since the port does not know the stage
// state then rather than knowing it is closed.
func (h *GameHost) closeStageCommand(call *ScriptCall) (int, uint16, error) {
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, nil
	}
	if h.Env.StageOpen == nil {
		return 0, 0, h.gap(call)
	}
	if !h.Env.StageOpen() {
		return 0, 0x0b, nil
	}
	return 0, 0, h.gap(call)
}
