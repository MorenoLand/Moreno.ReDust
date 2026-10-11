package scripts

import "fmt"

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

// closeStageCommand is closestagefile(), opcode 12061, FUN_004119C0. The reference
// runs, in order:
//
//	*result = 0
//	if (DAT_00459A00 == 0) return 0x0B;              // no open stage; no argument is read
//	status = FUN_00411E80();                           // the leave step, leaveStage
//	if status != 0 return status
//	if (DAT_00459A00 == 0) return 0;                  // the leave step may have closed it
//	if the guard DAT_00459A04+0xD34 changed, return 0 // not modelled, see below
//	FUN_00411A70(&DAT_00459A00);                      // clears the stage-open word; nothing else
//	FUN_00427EC0(picture, w*h); FUN_004055C0(2);      // picture clear and redraw, not ported
//	return 0
//
// The dispatcher requires "(" first and, after a status 0 result, ")" after that.
// The close runs before that ")" check, as the native dispatcher does, so a surplus
// argument closes the stage and then reports 2.
//
// Verified: the 0x0B gate, the leave step's order, FUN_00411A70 writing only the
// gate word, and the unhandled-message rule (see leaveStage). Not carried:
//
//   - FUN_00411A70's cache release, FUN_00401670 (status 0x1134) and FUN_004019F0
//     (status 0x1135). The port's stage file stays open until the run ends.
//   - The picture clear (FUN_00427EC0) and the redraw level (FUN_004055C0(2)). The
//     port keeps drawing the stage after a close, because it has no stage bitmap.
//   - The guard word DAT_00459A04+0xD34, which the native rechecks. The port has no
//     such field. The stage object and the current flat name stand in for it.
//
// The gate closes through CloseStage. A nil StageOpen, CloseStage or leave-step hook
// is a gap before anything runs, so a gap never leaves the leave step half done.
func (h *GameHost) closeStageCommand(call *ScriptCall) (int, uint16, error) {
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, nil
	}
	if h.Env.StageOpen == nil {
		return 0, 0, h.gap(call)
	}
	if !h.Env.StageOpen() {
		return 0, StatusNoActiveStage, nil
	}
	if !h.leaveStageWired() {
		return 0, 0, h.gap(call)
	}
	status, err := h.leaveStage(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if h.Env.StageOpen() {
		h.Env.CloseStage()
	}
	closeAt := call.Start + 2
	if call.Kind(closeAt) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return closeAt + 1 - call.Start, 0, nil
}

// leaveStageWired reports whether every hook the leave step and the close need is
// present, so that nothing runs before a gap can be reported.
func (h *GameHost) leaveStageWired() bool {
	return h.flatHooksWired() && h.Env.CurrentFlat != nil && h.Env.CloseStage != nil
}

// leaveStage is FUN_00411E80, the leave step, with the stage open. The reference:
//
//	status = FUN_00413130("closestage()");            // the stage script, the Stage Script frame
//	if status != 0 return status
//	if the gate, the guard or DAT_00459A08 changed return 0
//	FUN_00412DE0("\"<current flat>\", closeflat()")   // sendtoflat(name, closeflat())
//	return status of that send, or 0 when it is 0
//
// closestage() and closeflat() are named boundaries (FUN_0041DC00, checked against
// the reference's name table). A frame with no such block passes, so a stage or flat
// script without the block gives status 0 here, which is verified from FUN_0041C890
// and FUN_0041C9A0. The stage frame's Me and Target are DAT_00459A14 in the reference;
// no cached export writes that global, so the port uses "stage", as sendtostage does.
// The recheck covers the gate and the current flat name (the port's stand-in for the
// DAT_00459A08 index); the guard word is not modelled.
func (h *GameHost) leaveStage(call *ScriptCall) (uint16, error) {
	flat := h.Env.CurrentFlat()
	stageProgram, err := h.Env.StageScript("stage")
	if err != nil {
		return 0, err
	}
	stageMessage, err := CompileText([]byte(PoolNameStageShut))
	if err != nil {
		return 0, err
	}
	stageChain := []ScriptFrame{{Program: stageProgram, Me: "stage", Target: "stage", Label: PoolLabelStageScript, Last: true}}
	_, status, err := call.Interpreter.Call(stageChain, call.Chain, call.FrameIndex, call.Locals, &stageMessage, 0)
	if err != nil || status != 0 {
		return status, err
	}
	if !h.Env.StageOpen() || h.Env.CurrentFlat() != flat {
		return 0, nil
	}
	// FUN_00412DE0 tokenizes the Pascal text `"<flat>", closeflat()` and runs it
	// through the sendtoflat core, so the native send is the statement below.
	sendText := fmt.Sprintf("sendtoflat(%q, closeflat())", flat)
	send, err := CompileText([]byte(sendText))
	if err != nil {
		return 0, err
	}
	sendCall := &ScriptCall{Interpreter: call.Interpreter, Chain: call.Chain, FrameIndex: call.FrameIndex, Locals: call.Locals, Program: &send, Start: 0}
	_, status, err = h.sendToFlat(sendCall)
	return status, err
}
