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

// stageFrameName is the Me and Target of a stage script frame: DAT_00459A14, the
// open stage's header name. FUN_00411900 copies it from header +0x824, and the stage
// frame builder FUN_00413230 copies it into both fields. sendtostage reaches it
// through FUN_00413210, sendtostagefx through FUN_004137B0, and closestage and
// openstage through FUN_00413130 and FUN_00413210. sendtoflat and sendtobutton copy
// it into their stage frames in FUN_00412EE0 and FUN_00412A90. The port falls back
// to "stage" only when the StageName hook is absent or empty, which is the unwired
// case.
func (h *GameHost) stageFrameName() string {
	if h.Env.StageName != nil {
		if name := h.Env.StageName(); name != "" {
			return name
		}
	}
	return "stage"
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
// and FUN_0041C9A0. The stage frame's Me and Target are DAT_00459A14, the stage's name
// (see stageFrameName).
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
	stageName := h.stageFrameName()
	stageChain := []ScriptFrame{{Program: stageProgram, Me: stageName, Target: stageName, Label: PoolLabelStageScript, Last: true}}
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

// openStageCommand is openstagefile(name), opcode 12060, FUN_00411860. The reference
// runs, in order:
//
//	if (DAT_00459A00 != 0) return 0x0B;              // a stage is already open; no argument is read
//	status = FUN_004220A0(argument); if status != 0 return status
//	if argument kind != 3 return 0x0E;               // FUN_00421FC0: only a string converts
//	status = FUN_004178C0(name); if status != 0 return status    // 0x34, 0x2A or 0x31
//	FUN_00411900(name, &DAT_00459A00);               // the gate, stage, name and stage script word
//	FUN_00413430(0);                                 // current flat 0, with its picture
//	return FUN_00411D40();                           // openstage(), then "<flat>", openflat()
//
// The dispatcher requires "(" before the handler and ")" after a status 0 result, so
// the install happens before that check, as it does natively: a surplus argument
// installs the stage and then reports 2. FUN_00411900 also sets DAT_00459A02, the
// stagevisible word, to 1; that is GameHost.StageVisible.
//
// Verified here: the inverted 0x0B gate, the argument's string check, the install
// order and FUN_00411D40's two messages (see openStageFrames). The name check and the
// search are FUN_004178C0's, which the host runs through scripts.ResolveStageFile.
// A nil hook is a gap before anything runs, so an unwired host never installs half a
// stage.
func (h *GameHost) openStageCommand(call *ScriptCall) (int, uint16, error) {
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, nil
	}
	if h.Env.StageOpen == nil {
		return 0, 0, h.gap(call)
	}
	if status := OpenStageFileBlocked(ScriptContext{StageOpen: h.stageIsOpen()}); status != 0 {
		return 0, status, nil
	}
	if !h.openStageWired() {
		return 0, 0, h.gap(call)
	}
	record, used, status, err := call.Eval(call.Start + 2)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if record.Kind != ValueTypeString {
		return 0, StatusWrongOperandType, nil
	}
	name, status, err := call.Interpreter.Value(record)
	if err != nil || status != 0 {
		return 0, status, err
	}
	status, err = h.Env.OpenStage(name.Text)
	if err != nil || status != 0 {
		return 0, status, err
	}
	h.StageVisible = 1
	status, err = h.openStageFrames(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	closeAt := call.Start + 2 + used
	if call.Kind(closeAt) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return closeAt + 1 - call.Start, 0, nil
}

// openStageWired reports whether every hook openstagefile uses is present, so that
// nothing is installed before a gap can be reported.
func (h *GameHost) openStageWired() bool {
	return h.Env.StageOpen != nil && h.Env.OpenStage != nil && h.Env.CurrentFlat != nil && h.flatHooksWired()
}

// openStageFrames is FUN_00411D40, which runs once the stage is installed. The reference:
//
//	status = FUN_00413130("openstage()");            // the stage script, the Stage Script frame
//	if status != 0 return status
//	if the gate or the current flat index changed return 0
//	FUN_00412DE0("\"<current flat>\", openflat()")   // sendtoflat(name, openflat())
//	return that status, or 0 when it is 0
//
// The stage frame's Me and Target are DAT_00459A14, the stage's own name (FUN_00413230).
// The recheck uses the current flat name as the stand-in for the index, as leaveStage
// does. The guard word DAT_00459A04+0xD34 is not modelled.
func (h *GameHost) openStageFrames(call *ScriptCall) (uint16, error) {
	flat := h.Env.CurrentFlat()
	stageProgram, err := h.Env.StageScript("stage")
	if err != nil {
		return 0, err
	}
	message, err := CompileText([]byte(PoolNameStageOpen))
	if err != nil {
		return 0, err
	}
	stageName := h.stageFrameName()
	stageChain := []ScriptFrame{{Program: stageProgram, Me: stageName, Target: stageName, Label: PoolLabelStageScript, Last: true}}
	_, status, err := call.Interpreter.Call(stageChain, call.Chain, call.FrameIndex, call.Locals, &message, 0)
	if err != nil || status != 0 {
		return status, err
	}
	if !h.stageIsOpen() || h.Env.CurrentFlat() != flat {
		return 0, nil
	}
	send, err := CompileText([]byte(fmt.Sprintf("sendtoflat(%q, openflat())", flat)))
	if err != nil {
		return 0, err
	}
	sendCall := &ScriptCall{Interpreter: call.Interpreter, Chain: call.Chain, FrameIndex: call.FrameIndex, Locals: call.Locals, Program: &send, Start: 0}
	_, status, err = h.sendToFlat(sendCall)
	return status, err
}
