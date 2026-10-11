package scripts

import "fmt"

// The dialog builtins. notedialog 12079 is a command and questiondialog 20068 and
// textdialog 20069 are values. Their handlers, from the cached decompilation:
//
//	notedialog     FUN_00425B10: FUN_004220A0, FUN_00421FC0, FUN_0042DFB0 (MessageBoxA, 0x2040)
//	questiondialog FUN_00415970: FUN_004220A0, FUN_00421FC0, FUN_0042E030 (MessageBoxA, 0x2024)
//	textdialog     FUN_004158D0: FUN_00417070, FUN_00421FC0 twice, FUN_0042DF90, FUN_00427FC0
//
// The dispatchers check the "(" before a handler runs (FUN_00424890 and FUN_004137B0 both
// compare the record after the opcode with 0xFB2) and the ")" after it. So the argument is
// evaluated and the box is shown before the closing check: a call with extra arguments still
// shows its box and then reports status 2.

// DialogPresenter is the game side of the MessageBoxA boxes. Ask opens a box with the given
// caption, text and buttons over the frame on screen and returns at once. The script then
// waits until Answered reports the button the player closed the box with, which is index 0
// for the first button.
type DialogPresenter interface {
	Ask(caption, text string, buttons []string) error
	Answered() (button int, ok bool)
}

// The captions, read from the reference string table through FUN_00428D40, which is
// LoadStringA: resource 0x98 is "Information", the caption notedialog's box carries, and
// resource 0x99 is "Question", the caption questiondialog's box carries.
const (
	DialogNoteCaption     = "Information"
	DialogQuestionCaption = "Question"
)

// dialogOpen reports the "(" the dispatcher requires after the opcode.
func dialogOpen(call *ScriptCall) bool { return call.Kind(call.Start+1) == opOpen }

// dialogExpression evaluates the expression at index the way FUN_004220A0 does for a dialog
// argument and decodes it, returning the value and the index after it. A ")" where an
// argument should be fails with 0x0E: the evaluator hands it to FUN_00422040, which accepts
// only an identifier record and reports the wrong type otherwise.
func dialogExpression(call *ScriptCall, index int) (ScriptValue, int, uint16, error) {
	if call.Kind(index) == opClose {
		return ScriptValue{}, index, ScriptStatusWrongType, nil
	}
	record, consumed, status, err := call.Eval(index)
	if err != nil || status != 0 {
		return ScriptValue{}, index, status, err
	}
	value, status, err := call.Interpreter.Value(record)
	if err != nil || status != 0 {
		return ScriptValue{}, index, status, err
	}
	return value, index + consumed, 0, nil
}

// dialogText is dialogExpression for a string argument. FUN_00421FC0 refuses anything that
// is not a string with 0x0E before any box is shown.
func dialogText(call *ScriptCall, index int) (string, int, uint16, error) {
	value, next, status, err := dialogExpression(call, index)
	if err != nil || status != 0 {
		return "", index, status, err
	}
	if value.Kind != 3 {
		return "", index, ScriptStatusWrongType, nil
	}
	return value.Text, next, 0, nil
}

// askDialog opens a box and suspends the script until the player closes it, the way the
// native MessageBoxA blocks its caller. Like the other blocking commands it needs a script
// task to suspend, so outside one it reports ErrNotInTask before showing anything.
func (h *GameHost) askDialog(name, caption, text string, buttons []string) (int, error) {
	if h.Env.Dialogs == nil {
		return 0, fmt.Errorf("%w: %s has no dialog presenter", ErrHostOpcodeUnimplemented, name)
	}
	if !h.task.Running() {
		return 0, ErrNotInTask
	}
	if err := h.Env.Dialogs.Ask(caption, text, buttons); err != nil {
		return 0, err
	}
	if err := h.wait(func() bool { _, ok := h.Env.Dialogs.Answered(); return ok }); err != nil {
		return 0, err
	}
	button, _ := h.Env.Dialogs.Answered()
	return button, nil
}

// noteDialog is notedialog 12079. The box has one OK button and the command returns no
// value, so the status is 0 once the box has closed and the closing ")" is in place.
func (h *GameHost) noteDialog(call *ScriptCall) (int, uint16, error) {
	if !dialogOpen(call) {
		return 0, ScriptStatusMalformed, nil
	}
	text, next, status, err := dialogText(call, call.Start+2)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if _, err := h.askDialog("notedialog", DialogNoteCaption, text, []string{"OK"}); err != nil {
		return 0, 0, err
	}
	if call.Kind(next) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return next + 1 - call.Start, 0, nil
}

// questionDialog is questiondialog 20068. The buttons are Yes then No, so the first button
// is IDYES, the value the native compares against (`return iVar2 == 6`). Every other answer,
// No or a closed box included, is false, and none of them is an error.
func (h *GameHost) questionDialog(call *ScriptCall) (Record, int, uint16, error) {
	if !dialogOpen(call) {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	text, next, status, err := dialogText(call, call.Start+2)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	button, err := h.askDialog("questiondialog", DialogQuestionCaption, text, []string{"Yes", "No"})
	if err != nil {
		return Record{}, 0, 0, err
	}
	if call.Kind(next) != opClose {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	return Record{Kind: 2, Data: boolWord(button == 0)}, next + 1 - call.Start, 0, nil
}

// textDialog is textdialog 20069. It takes two strings separated by a comma: FUN_00417070
// reports 0x1C without the comma, and FUN_00421FC0 reports 0x0E for a non-string.
//
// The native then calls FUN_0042DF90, which raises FUN_0042C470(0, 0x1A0). That is the Fatal
// Error box (resource 0xB5, OK and Cancel). Its text is resource 0xB3 when GetLastError holds
// none of the codes the reporter maps, and another resource otherwise. Cancel exits the
// process. This port does not present the box: the text depends on the OS error state, which
// the port does not reproduce, so the box is logged instead. The builtin returns the empty
// string: FUN_00427FC0 measures the text at DAT_0045D4E0, and the byte at 0x0045D4E1 is the
// terminator, so the length is 0.
func (h *GameHost) textDialog(call *ScriptCall) (Record, int, uint16, error) {
	if !dialogOpen(call) {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	first, next, status, err := dialogExpression(call, call.Start+2)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if call.Kind(next) != opComma {
		return Record{}, 0, ScriptStatusMissingComma, nil
	}
	second, next, status, err := dialogExpression(call, next+1)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if first.Kind != 3 || second.Kind != 3 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	h.logf("textdialog: the native Fatal Error box (FUN_0042C470(0, 0x1A0)) is not presented")
	record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: ""})
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if call.Kind(next) != opClose {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	return record, next + 1 - call.Start, 0, nil
}
