package scripts

import (
	"fmt"
	"strconv"
)

// The sendto*fx value senders, and the statement senders sendtoset and
// sendtofloor that share their delivery.
//
// Every command-side sendto is a one-line forwarder to the value handler of the
// same name with a zero result slot, for example FUN_0041A920 calls
// FUN_0041A940(p1, p2, p3, 0, p4) and FUN_00418400 calls FUN_00418420(..., 0, ...).
// So one function serves both forms here: a nil result is the statement, and a
// non-nil result receives the message's return value through FUN_0041C890.
//
// Each native handler checks its context before it parses anything:
//
//	FUN_00424890 / FUN_004137B0   the "(" after the keyword, status 0x02
//	handler                        its guard, 0x0B (stage) or 0x28 (set)
//	FUN_004220A0                   the target expression
//	                               the comma record, 0x1C
//	handler                        the target's kind, 0x0E
//	                               the target's lookup, 0x0A
//
// The senders whose target lookup and chain delivery the port has are here:
// sendtoactorfx, sendtoscenefx, sendtopropfx, sendtobootfx, sendtosetfx,
// sendtofloorfx, sendtoflatfx and sendtobuttonfx, plus the statements sendtoset and
// sendtofloor. sendtoshopfx is in puppetValue. The flat senders run the flat and
// button chains that gamehost_flats.go builds, and refuse the flats the port answers
// by hand.

// floorCount is the number of floor indexes the set's area table holds: FUN_0041A5C0
// accepts 0 through 0x1F and reads the table at SET metadata +0x1B8C, 32 dwords.
const floorCount = 0x20

// sendToBootLabel and sendToBootName are FUN_00418420's frame: the label is
// "Boot Message: " (DAT_0045D5B8), and the Me and Target strings are both
// DAT_0045D060, the Pascal string "System" (FUN_00427FC0 writes the length over the
// placeholder byte before the text). The statement sendtoboot runs the same frame.
const (
	sendToBootLabel = "Boot Message: "
	sendToBootName  = "System"
)

// sendToOpen is the "(" check the dispatcher makes before any sendto handler runs.
func sendToOpen(call *ScriptCall) uint16 {
	if call.Kind(call.Start+1) != opOpen {
		return ScriptStatusMalformed
	}
	return 0
}

// sendToTarget evaluates the target expression after the "(" and requires the
// comma after it, in the order the native target parser and the comma test run.
// It returns the target unchecked, because each handler tests the target's kind
// after the comma. message is the index of the first record of the message.
func (h *GameHost) sendToTarget(call *ScriptCall) (target ScriptValue, message int, status uint16, err error) {
	record, consumed, status, err := call.Eval(call.Start + 2)
	if err != nil || status != 0 {
		return ScriptValue{}, 0, status, err
	}
	target, status, err = call.Interpreter.Value(record)
	if err != nil || status != 0 {
		return ScriptValue{}, 0, status, err
	}
	message = call.Start + 2 + consumed
	if call.Kind(message) != opComma {
		return ScriptValue{}, 0, ScriptStatusMissingComma, nil
	}
	return target, message + 1, 0, nil
}

// setOpen is the set gate every set-guarded sendto tests, DAT_00459A24.
func (h *GameHost) setOpen() bool {
	_, open := h.setRect()
	return open
}

// sendToChainFx runs message against chain and returns the records the whole call
// spans, including its closing parenthesis. result is nil for the statement form.
func (h *GameHost) sendToChainFx(call *ScriptCall, chain []ScriptFrame, message int, result *Record) (int, uint16, error) {
	used, status, err := call.Interpreter.callCode(chain, 0, call.Chain, call.FrameIndex, call.Locals, call.Program, message, result)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(message+used) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return message + used + 1 - call.Start, 0, nil
}

// sendToActorFx is sendtoactorfx (20084), FUN_0040CB60 with a result slot. The
// target is an actor name and the chain is the actor's [actor script, cast script]
// pair, the same chain sendtoactor runs. FUN_0040CB60 has no guard: its first call
// is the target parser, which the cached assembly shows, so no stage or set is
// required. A hand-written actor has no script in the port, so its message value
// is not reproduced and the call is a gap, not a guessed return value.
func (h *GameHost) sendToActorFx(call *ScriptCall, result *Record) (int, uint16, error) {
	if status := sendToOpen(call); status != 0 {
		return 0, status, nil
	}
	target, message, status, err := h.sendToTarget(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if target.Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	actor, status := h.Actors.Lookup(target.Text)
	if status != 0 && h.managed(target.Text) {
		return 0, status, nil
	}
	if status != 0 || !h.managed(actor.Name) {
		return 0, 0, fmt.Errorf("%w: sendtoactorfx(%q) addresses an actor the port runs by hand, which has no script value", ErrHostOpcodeUnimplemented, target.Text)
	}
	if h.Env.Chain == nil {
		return 0, 0, fmt.Errorf("%w: sendtoactorfx has no chain provider", ErrHostOpcodeUnimplemented)
	}
	chain, err := h.Env.Chain(actor)
	if err != nil {
		return 0, 0, err
	}
	return h.sendToChainFx(call, chain, message, result)
}

// sendToSceneFx is sendtoscenefx (20085), FUN_0041A1A0 with a result slot. It is
// guarded by the set (0x28), then names a scene. The chain is the one sendtoscene
// builds: the scene's own cell script when it is a cell, the area script and the
// set script. SceneChain is shared with the statement form.
func (h *GameHost) sendToSceneFx(call *ScriptCall, result *Record) (int, uint16, error) {
	if status := sendToOpen(call); status != 0 {
		return 0, status, nil
	}
	if !h.setOpen() {
		return 0, StatusNoActiveSet, nil
	}
	target, message, status, err := h.sendToTarget(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if target.Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	if h.Env.SceneChain == nil {
		return 0, 0, fmt.Errorf("%w: sendtoscenefx has no scene chain", ErrHostOpcodeUnimplemented)
	}
	chain, found, err := h.Env.SceneChain(target.Text)
	if err != nil {
		return 0, 0, err
	}
	if !found {
		return 0, StatusNameNotFound, nil
	}
	return h.sendToChainFx(call, chain, message, result)
}

// sendToPropFx is sendtopropfx (20088), FUN_00420CE0 with a result slot. It has no
// guard. The chain is the prop's script, then the shop script that owns it, the
// same two frames sendtoprop builds.
func (h *GameHost) sendToPropFx(call *ScriptCall, result *Record) (int, uint16, error) {
	if status := sendToOpen(call); status != 0 {
		return 0, status, nil
	}
	target, message, status, err := h.sendToTarget(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if target.Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	if status := nativeName(target.Text); status != 0 {
		return 0, status, nil
	}
	if h.Env.PropScript == nil || h.Env.Program == nil {
		return 0, 0, fmt.Errorf("%w: sendtopropfx has no prop script resolver", ErrHostOpcodeUnimplemented)
	}
	file, resource, shop, found := h.Env.PropScript(target.Text)
	if !found {
		return 0, StatusNameNotFound, nil
	}
	program, err := h.Env.Program(file, resource)
	if err != nil {
		return 0, 0, err
	}
	shared, me, status, err := h.shopScript(shop)
	if err != nil || status != 0 {
		return 0, status, err
	}
	chain := []ScriptFrame{
		{Program: program, Me: target.Text, Target: target.Text, Label: "Prop Script: "},
		{Program: shared, Me: me, Target: target.Text, Label: "Shop Script: ", Last: true},
	}
	return h.sendToChainFx(call, chain, message, result)
}

// sendToBootFx is sendtobootfx (20095), FUN_00418420 with a result slot. It takes
// the message alone, with no target, and runs it against the BOOTFILE script
// (resource 1 of BOOTFILE, DAT_00443C20). It has no guard.
func (h *GameHost) sendToBootFx(call *ScriptCall, result *Record) (int, uint16, error) {
	if status := sendToOpen(call); status != 0 {
		return 0, status, nil
	}
	if h.Env.BootScript == nil {
		return 0, 0, fmt.Errorf("%w: sendtobootfx has no boot script", ErrHostOpcodeUnimplemented)
	}
	program, err := h.Env.BootScript()
	if err != nil {
		return 0, 0, err
	}
	chain := []ScriptFrame{{Program: program, Me: sendToBootName, Target: sendToBootName, Label: sendToBootLabel, Last: true}}
	return h.sendToChainFx(call, chain, call.Start+2, result)
}

// sendToSetFx is sendtosetfx (20091) and the statement sendtoset (12067), which is
// FUN_0041A940 with a zero result slot. It takes the message alone and runs it
// against the open set's script (the u32 at SET +0x1B78). It is guarded by the set
// (0x28). Me and Target are the set's name, as the native frame builds them.
func (h *GameHost) sendToSetFx(call *ScriptCall, result *Record) (int, uint16, error) {
	if status := sendToOpen(call); status != 0 {
		return 0, status, nil
	}
	if !h.setOpen() {
		return 0, StatusNoActiveSet, nil
	}
	if h.Env.SetScript == nil {
		return 0, 0, fmt.Errorf("%w: sendtosetfx has no set script", ErrHostOpcodeUnimplemented)
	}
	program, err := h.Env.SetScript()
	if err != nil {
		return 0, 0, err
	}
	name := h.currentSet()
	chain := []ScriptFrame{{Program: program, Me: name, Target: name, Label: "Set Script: ", Last: true}}
	return h.sendToChainFx(call, chain, call.Start+2, result)
}

// sendToFloorFx is sendtofloorfx (20090) and the statement sendtofloor (12066),
// FUN_0041A5C0 with a zero result slot. The target is a number, not a name: the
// floor index 0 through 0x1F, which selects the area script from the set's table at
// +0x1B8C. The chain is that floor script, then the set script, and both Me and
// Target of the floor frame are the decimal text of index+1 (FUN_0042E7D0 is called
// with index+1, unlike sendtoscene's area frame, which uses the reference index
// unchanged). Guards: the set (0x28), then the comma (0x1C), then the kind (0x0E),
// then the index range (0x0A).
func (h *GameHost) sendToFloorFx(call *ScriptCall, result *Record) (int, uint16, error) {
	if status := sendToOpen(call); status != 0 {
		return 0, status, nil
	}
	if !h.setOpen() {
		return 0, StatusNoActiveSet, nil
	}
	target, message, status, err := h.sendToTarget(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if target.Kind != 4 {
		return 0, ScriptStatusWrongType, nil
	}
	if target.Int < 0 || target.Int >= floorCount {
		return 0, StatusNameNotFound, nil
	}
	if h.Env.FloorScript == nil || h.Env.SetScript == nil {
		return 0, 0, fmt.Errorf("%w: sendtofloorfx has no floor or set script", ErrHostOpcodeUnimplemented)
	}
	floor, found, err := h.Env.FloorScript(int(target.Int))
	if err != nil {
		return 0, 0, err
	}
	if !found {
		return 0, StatusNameNotFound, nil
	}
	set, err := h.Env.SetScript()
	if err != nil {
		return 0, 0, err
	}
	name := strconv.Itoa(int(target.Int) + 1)
	chain := []ScriptFrame{
		{Program: floor, Me: name, Target: name, Label: "Floor Script: "},
		{Program: set, Me: h.currentSet(), Target: name, Label: "Set Script: ", Last: true},
	}
	return h.sendToChainFx(call, chain, message, result)
}

// flatHandWritten reports whether the port answers a flat's messages by hand.
func (h *GameHost) flatHandWritten(name string) bool {
	return h.Env.FlatHandWritten != nil && h.Env.FlatHandWritten(name)
}

// sendToFlatFx is sendtoflatfx (20093), FUN_00412EE0 with a result slot. It runs the
// message against the flat's script and then the stage script, the chain sendtoflat
// builds, and returns the message's value. The checks run in the statement's order:
// the stage (0x0B), the "(", the comma (0x1C), the name's kind (0x0E), the flat (0x0A).
// A flat the port answers by hand has no value to return, so it reports a gap, and so
// does a flat whose script is not runnable.
func (h *GameHost) sendToFlatFx(call *ScriptCall, result *Record) (int, uint16, error) {
	if !h.flatHooksWired() {
		return 0, 0, h.gap(call)
	}
	if !h.Env.StageOpen() {
		return 0, StatusNoActiveStage, nil
	}
	if status := sendToOpen(call); status != 0 {
		return 0, status, nil
	}
	target, message, status, err := h.sendToTarget(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if target.Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	flat, found := h.Env.FlatIndex(target.Text)
	if !found {
		return 0, StatusNameNotFound, nil
	}
	flatName, ok := h.Env.FlatName(flat)
	if !ok {
		return 0, StatusNameNotFound, nil
	}
	if h.flatHandWritten(flatName) {
		return 0, 0, h.gap(call)
	}
	flatProgram, ok, err := h.Env.FlatScript(flat)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, h.gap(call)
	}
	stageProgram, err := h.Env.StageScript("stage")
	if err != nil {
		return 0, 0, err
	}
	chain := []ScriptFrame{
		{Program: flatProgram, Me: flatName, Target: flatName, Label: PoolLabelFlatScript},
		{Program: stageProgram, Me: h.stageFrameName(), Target: flatName, Label: PoolLabelStageScript, Last: true},
	}
	return h.sendToChainFx(call, chain, message, result)
}

// sendToButtonFx is sendtobuttonfx (20092), FUN_00412A90 with a result slot. It runs
// the message against the button's script, then the flat's, then the stage's, the
// chain sendtobutton builds, and returns the message's value. The checks follow
// FUN_00417070 and FUN_00412A90: the stage (0x0B), the "(", both commas (0x1C) before
// either name's kind, the flat's kind (0x0E) and lookup (0x0A), then the button's kind
// (0x0E) and lookup (0x0A). A hand-written flat or an unrunnable script is a gap.
func (h *GameHost) sendToButtonFx(call *ScriptCall, result *Record) (int, uint16, error) {
	if !h.buttonHooksWired() {
		return 0, 0, h.gap(call)
	}
	if !h.Env.StageOpen() {
		return 0, StatusNoActiveStage, nil
	}
	if status := sendToOpen(call); status != 0 {
		return 0, status, nil
	}
	flatValue, buttonAt, status, err := h.sendToTarget(call)
	if err != nil || status != 0 {
		return 0, status, err
	}
	buttonTarget, buttonConsumed, status, err := call.Eval(buttonAt)
	if err != nil || status != 0 {
		return 0, status, err
	}
	buttonValue, status, err := call.Interpreter.Value(buttonTarget)
	if err != nil || status != 0 {
		return 0, status, err
	}
	comma := buttonAt + buttonConsumed
	if call.Kind(comma) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	message := comma + 1
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
	if h.flatHandWritten(flatName) {
		return 0, 0, h.gap(call)
	}
	buttonProgram, ok, err := h.Env.FlatButtonScript(flat, button)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, h.gap(call)
	}
	flatProgram, ok, err := h.Env.FlatScript(flat)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, h.gap(call)
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
	return h.sendToChainFx(call, chain, message, result)
}

// sendToValue dispatches the value senders in this file. handled is false for a
// name it does not own, so the caller's other handlers still see it.
func (h *GameHost) sendToValue(name string, call *ScriptCall) (value Record, consumed int, status uint16, handled bool, err error) {
	var result Record
	switch name {
	case "sendtoactorfx":
		consumed, status, err = h.sendToActorFx(call, &result)
	case "sendtoscenefx":
		consumed, status, err = h.sendToSceneFx(call, &result)
	case "sendtopropfx":
		consumed, status, err = h.sendToPropFx(call, &result)
	case "sendtobootfx":
		consumed, status, err = h.sendToBootFx(call, &result)
	case "sendtosetfx":
		consumed, status, err = h.sendToSetFx(call, &result)
	case "sendtofloorfx":
		consumed, status, err = h.sendToFloorFx(call, &result)
	case "sendtoflatfx":
		consumed, status, err = h.sendToFlatFx(call, &result)
	case "sendtobuttonfx":
		consumed, status, err = h.sendToButtonFx(call, &result)
	default:
		return Record{}, 0, 0, false, nil
	}
	if err != nil || status != 0 {
		return Record{}, 0, status, true, err
	}
	return result, consumed, 0, true, nil
}
