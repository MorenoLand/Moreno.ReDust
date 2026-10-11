package scripts

import "errors"

// The flat and button targets of the sendto family: sendtoflat (12069,
// FUN_00412EE0) and sendtobutton (12068, FUN_00412A90). Both resolve their
// target by name in the open stage and then run the message against the
// target's script chain, which FUN_0041C890 searches in frame order.
//
//	sendtoflat(name, message(args))            name: FUN_00413530 over the stage's flat table
//	sendtobutton(flat, button, message(args))  flat as above, then button: FUN_00413630
//	                                           over the flat's button table
//
// Frames, in the order the handlers build them (and so search them):
//
//	sendtoflat:   [Flat Script: , Stage Script: (last)]
//	sendtobutton: [Button Script: , Flat Script: , Stage Script: (last)]
//
// Me is the owner's row name: the flat's for the flat frame, the button's for the
// button frame. Target is the addressed row name in every frame: the flat's for
// sendtoflat, the button's for sendtobutton. The stage frame's Me is the stage's
// name, DAT_00459A14 (see stageFrameName).
//
// Statuses, in the order the handlers reach them: 0x0B with no stage; 0x1C for a
// missing comma, checked before the name's type as FUN_00412EE0 checks 0x0FB4
// before FUN_00421FC0 converts the name (sendtobutton has two commas, and both are
// checked before either name's type, as FUN_00417070 and FUN_00412A90 order them);
// 0x0E for a non-string name; 0x0A for a flat or button the stage does not have;
// then the chain's own status. The open parenthesis belongs to the command call
// frame (FUN_00424890), and the port's malformed status covers a call without it.
//
// The script path runs only after those checks, and only after Fallback has been
// offered the call (flatFallback). Fallback therefore keeps the hand-written flat
// messages (the mainpanel openflat and tiphat, and the death death) at their
// existing behavior, even though those flats' scripts could run. When a flat's or
// button's script is not runnable (FlatScript or FlatButtonScript reports ok
// false, or the load fails), the send reports Fallback's gap, or the load error.
// With the lookup and script hooks unwired, the send goes to Fallback alone
// (flatUnrun).

// flatFallback offers the call to Fallback. handled is false when Fallback does
// not claim it, and err then carries Fallback's gap error (nil without a
// Fallback). When handled, err is whatever Fallback returned.
func (h *GameHost) flatFallback(call *ScriptCall) (consumed int, status uint16, handled bool, err error) {
	if h.Env.Fallback == nil {
		return 0, 0, false, nil
	}
	consumed, status, err = h.Env.Fallback.Command(call)
	if errors.Is(err, ErrHostOpcodeUnimplemented) {
		return 0, 0, false, err
	}
	return consumed, status, true, err
}

// flatGap is the error for a flat-targeted send the script path does not run:
// Fallback's gap when it gave one, else the host's own gap.
func (h *GameHost) flatGap(call *ScriptCall, fallbackGap error) error {
	if fallbackGap != nil {
		return fallbackGap
	}
	return h.gap(call)
}

// flatUnrun answers a flat-targeted send without running a script: Fallback's
// answer when it claims the call, otherwise its gap.
func (h *GameHost) flatUnrun(call *ScriptCall) (int, uint16, error) {
	consumed, status, handled, gap := h.flatFallback(call)
	if handled {
		return consumed, status, gap
	}
	return 0, 0, h.flatGap(call, gap)
}

// flatHooksWired reports whether the flat lookups and the loaders the flat sendtos
// need are all present.
func (h *GameHost) flatHooksWired() bool {
	return h.Env.StageOpen != nil && h.Env.FlatIndex != nil && h.Env.FlatName != nil &&
		h.Env.FlatScript != nil && h.Env.StageScript != nil
}

// buttonHooksWired adds the button lookups and the button script loader.
func (h *GameHost) buttonHooksWired() bool {
	return h.flatHooksWired() && h.Env.FlatButtonCount != nil &&
		h.Env.FlatButtonName != nil && h.Env.FlatButtonScript != nil
}

// flatButtonIndex is FUN_00413720 over one flat's button block: the first row, from
// row 0, whose name matches under the ASCII case-insensitive compare.
func (h *GameHost) flatButtonIndex(flat int, name string) (int, bool, error) {
	count, err := h.Env.FlatButtonCount(flat)
	if err != nil {
		return 0, false, err
	}
	for index := 0; index < count; index++ {
		row, err := h.Env.FlatButtonName(flat, index)
		if err != nil {
			return 0, false, err
		}
		if EqualASCIIFold(row, name) {
			return index, true, nil
		}
	}
	return 0, false, nil
}

// sendToFlat is FUN_00412EE0: `sendtoflat(name, message(args))`. The message runs
// against the flat's script and then the stage script, the flat's handler first.
func (h *GameHost) sendToFlat(call *ScriptCall) (int, uint16, error) {
	if !h.flatHooksWired() {
		return h.flatUnrun(call)
	}
	if !h.Env.StageOpen() {
		return 0, StatusNoActiveStage, nil
	}
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, nil
	}
	target, consumed, status, err := call.Eval(call.Start + 2)
	if err != nil || status != 0 {
		return 0, status, err
	}
	value, status, err := call.Interpreter.Value(target)
	if err != nil || status != 0 {
		return 0, status, err
	}
	message := call.Start + 2 + consumed
	if call.Kind(message) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	message++
	if value.Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	flat, found := h.Env.FlatIndex(value.Text)
	if !found {
		return 0, StatusNameNotFound, nil
	}
	flatName, ok := h.Env.FlatName(flat)
	if !ok {
		return 0, StatusNameNotFound, nil
	}
	consumed, status, handled, gap := h.flatFallback(call)
	if handled {
		return consumed, status, gap
	}
	flatProgram, ok, err := h.Env.FlatScript(flat)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, h.flatGap(call, gap)
	}
	stageProgram, err := h.Env.StageScript("stage")
	if err != nil {
		return 0, 0, err
	}
	chain := []ScriptFrame{
		{Program: flatProgram, Me: flatName, Target: flatName, Label: PoolLabelFlatScript},
		{Program: stageProgram, Me: h.stageFrameName(), Target: flatName, Label: PoolLabelStageScript, Last: true},
	}
	used, status, err := call.Interpreter.Call(chain, call.Chain, call.FrameIndex, call.Locals, call.Program, message)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(message+used) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return message + used + 1 - call.Start, 0, nil
}

// sendToButton is FUN_00412A90: `sendtobutton(flat, button, message(args))`. The
// message runs against the button's script, then the flat's, then the stage's.
func (h *GameHost) sendToButton(call *ScriptCall) (int, uint16, error) {
	if !h.buttonHooksWired() {
		return h.flatUnrun(call)
	}
	if !h.Env.StageOpen() {
		return 0, StatusNoActiveStage, nil
	}
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, nil
	}
	flatTarget, flatConsumed, status, err := call.Eval(call.Start + 2)
	if err != nil || status != 0 {
		return 0, status, err
	}
	flatValue, status, err := call.Interpreter.Value(flatTarget)
	if err != nil || status != 0 {
		return 0, status, err
	}
	comma := call.Start + 2 + flatConsumed
	if call.Kind(comma) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	buttonTarget, buttonConsumed, status, err := call.Eval(comma + 1)
	if err != nil || status != 0 {
		return 0, status, err
	}
	buttonValue, status, err := call.Interpreter.Value(buttonTarget)
	if err != nil || status != 0 {
		return 0, status, err
	}
	message := comma + 1 + buttonConsumed
	if call.Kind(message) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	message++
	if flatValue.Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	flat, found := h.Env.FlatIndex(flatValue.Text)
	if !found {
		return 0, StatusNameNotFound, nil
	}
	if buttonValue.Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	button, found, err := h.flatButtonIndex(flat, buttonValue.Text)
	if err != nil {
		return 0, 0, err
	}
	if !found {
		return 0, StatusNameNotFound, nil
	}
	flatName, ok := h.Env.FlatName(flat)
	if !ok {
		return 0, StatusNameNotFound, nil
	}
	buttonName, err := h.Env.FlatButtonName(flat, button)
	if err != nil {
		return 0, 0, err
	}
	consumed, status, handled, gap := h.flatFallback(call)
	if handled {
		return consumed, status, gap
	}
	buttonProgram, ok, err := h.Env.FlatButtonScript(flat, button)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, h.flatGap(call, gap)
	}
	flatProgram, ok, err := h.Env.FlatScript(flat)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, h.flatGap(call, gap)
	}
	stageProgram, err := h.Env.StageScript("stage")
	if err != nil {
		return 0, 0, err
	}
	chain := []ScriptFrame{
		{Program: buttonProgram, Me: buttonName, Target: buttonName, Label: PoolLabelButtonScript},
		{Program: flatProgram, Me: flatName, Target: buttonName, Label: PoolLabelFlatScript},
		{Program: stageProgram, Me: h.stageFrameName(), Target: buttonName, Label: PoolLabelStageScript, Last: true},
	}
	used, status, err := call.Interpreter.Call(chain, call.Chain, call.FrameIndex, call.Locals, call.Program, message)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(message+used) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return message + used + 1 - call.Start, 0, nil
}
