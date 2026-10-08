package native

import (
	. "redust/scripts"
	"fmt"
)

// The dialog pair. `notedialog` 12079 and `questiondialog` 20068 both end in a
// real Win32 MessageBoxA, and the two calls differ in a way that is visible to
// the player, so both are transcribed in full from FUN_0042DFB0 and FUN_0042E030.
//
// questiondialog, FUN_0042E030:
//
//	n = *text;                                  // Pascal length
//	memcpy(buf, text + 1, n); buf[n] = 0;       // 256-byte C string
//	FUN_00428D70(buf, converted, DAT_00445884);  // codepage conversion
//	FUN_0042DA10();                             // enter the modal block
//	caption = FUN_00428D40(0x99);
//	i = MessageBoxA(NULL, buf, caption, 0x2024);
//	FUN_0042DA60();                             // leave the modal block
//	return i == 6;                              // IDYES
//
// notedialog, FUN_0042DFB0, is the same shape with `uType = 0x2040`, caption
// `FUN_00428D40(0x98)`, no return value, and it passes the **converted** buffer
// where questiondialog passes the **unconverted** one. That asymmetry is in the
// reference and is recorded rather than smoothed over, because a port that
// converted both would render the question box in a different code page from the
// note box.

// Windows MessageBox style flags, as they appear in the two verified uType
// values. They are spelled out so a port does not have to re-derive them, and so
// the resulting button and icon behaviour is checkable.
const (
	// MBYesNo is 0x0004, the low-nibble button combination. It is present in
	// 0x2024 and absent from 0x2040.
	MBYesNo uint32 = 0x0004
	// MBIconQuestion is 0x0020, the question-mark icon, present in 0x2024.
	MBIconQuestion uint32 = 0x0020
	// MBIconAsterisk is 0x0040, the information icon, present in 0x2040.
	MBIconAsterisk uint32 = 0x0040
	// MBTaskModal is 0x2000, which disables the parent window's other children.
	// Both boxes set it, so neither is application-modal.
	MBTaskModal uint32 = 0x2000
	// MBOK is 0x0000, the single-button form, which is what 0x2040 uses.
	MBOK uint32 = 0x0000
)

// StringResourceLookup resolves a string resource id to its text, FUN_00428D40. **It is
// not the dialogs' own**: the video driver's selector fetches its twenty driver names
// through the same routine, so naming it here makes the sharing structural rather than a
// coincidence of two comments.
const StringResourceLookup = "FUN_00428D40"

// The verified MessageBox type words and what they decompose to.
const (
	// DialogQuestionType is the uType questiondialog passes, 0x2024.
	DialogQuestionType uint32 = 0x2024
	// DialogNoteType is the uType notedialog passes, 0x2040.
	DialogNoteType uint32 = 0x2040
	// IDYes is the MessageBox return the question counts as true, 6.
	IDYes = 6
	// IDCaptionNoteResource is the string resource id notedialog's caption comes
	// from, 0x98.
	IDCaptionNoteResource = 0x98
	// IDCaptionQuestionResource is the string resource id questiondialog's caption
	// comes from, 0x99.
	IDCaptionQuestionResource = 0x99
	// DialogTextBufferSize is the 256-byte buffer both build the C string in. A
	// Pascal string longer than 255 would not fit, and the reference does not
	// check: it writes the terminator at buf[n] and the length byte cannot exceed
	// 255, so the write is always in range.
	DialogTextBufferSize = 256
)

// DecomposeMessageBoxType splits a verified uType into its flag names, which is
// what makes the two dialogs' visible behaviour checkable: questiondialog is a
// Yes/No question and notedialog is a single-button notice.
func DecomposeMessageBoxType(style uint32) []string {
	var flags []string
	buttons := style & 0x000F
	switch buttons {
	case MBOK:
		flags = append(flags, "MB_OK")
	case 0x0001:
		flags = append(flags, "MB_OKCANCEL")
	case 0x0002:
		flags = append(flags, "MB_ABORTRETRYIGNORE")
	case 0x0003:
		flags = append(flags, "MB_YESNOCANCEL")
	case MBYesNo:
		flags = append(flags, "MB_YESNO")
	case 0x0005:
		flags = append(flags, "MB_RETRYCANCEL")
	default:
		flags = append(flags, fmt.Sprintf("buttons(%#x)", buttons))
	}
	switch style & 0x0070 {
	case 0x0000:
		// No icon bit set.
	case 0x0010:
		flags = append(flags, "MB_ICONHAND")
	case MBIconQuestion:
		flags = append(flags, "MB_ICONQUESTION")
	case 0x0030:
		flags = append(flags, "MB_ICONEXCLAMATION")
	case MBIconAsterisk:
		flags = append(flags, "MB_ICONASTERISK")
	}
	switch style & 0x3000 {
	case 0:
		flags = append(flags, "application modal")
	case MBTaskModal:
		flags = append(flags, "MB_TASKMODAL")
	case 0x3000:
		flags = append(flags, "MB_SYSTEMMODAL")
	}
	if style&0x10000 != 0 {
		flags = append(flags, "MB_SETFOREGROUND")
	}
	return flags
}

// DialogKind distinguishes the two dialogs, which differ in more than their type
// word.
type DialogKind uint8

const (
	// DialogNote is notedialog 12079, a single-button notice.
	DialogNote DialogKind = iota
	// DialogQuestion is questiondialog 20068, a Yes/No question.
	DialogQuestion
)

// DialogSpec describes one verified dialog call.
type DialogSpec struct {
	// Kind is which dialog this is.
	Kind DialogKind
	// Opcode is the command or value opcode.
	Opcode uint16
	// Name is the script keyword.
	Name string
	// Handler is the opcode's native handler.
	Handler string
	// Trigger is the native function that shows the box.
	Trigger string
	// Style is the verified MessageBox uType.
	Style uint32
	// CaptionResource is the string resource id the caption comes from.
	CaptionResource int
	// PassesConvertedText records which buffer reaches MessageBoxA. It is true
	// for notedialog and false for questiondialog, and it is the reason the two
	// functions are transcribed separately rather than sharing one helper.
	PassesConvertedText bool
	// ReturnsAnswer records that the question reports IDYES as true and the note
	// reports nothing.
	ReturnsAnswer bool
}

// dialogSpecs is the verified pair. questiondialog's handler returns the answer as
// a type-2 boolean; notedialog's returns no value at all.
var dialogSpecs = []DialogSpec{
	{DialogNote, 12079, "notedialog", "FUN_00425B10", "FUN_0042DFB0", DialogNoteType, IDCaptionNoteResource, true, false},
	{DialogQuestion, 20068, "questiondialog", "FUN_00415970", "FUN_0042E030", DialogQuestionType, IDCaptionQuestionResource, false, true},
}

// DialogSpecFor returns the verified spec for an opcode.
func DialogSpecFor(opcode uint16) (DialogSpec, bool) {
	for _, spec := range dialogSpecs {
		if spec.Opcode == opcode {
			return spec, true
		}
	}
	return DialogSpec{}, false
}

// PascalToCBuffer converts a Pascal string to the NUL-terminated C string both
// dialogs build, reproducing the reference's `memcpy(buf, text + 1, n); buf[n] =
// 0`. A Pascal string with a length longer than the buffer would overflow in the
// native code, which does not check, so the Go port refuses it instead; a
// well-formed 255 byte string still fits exactly.
func PascalToCBuffer(pascal []byte) ([]byte, error) {
	if len(pascal) == 0 {
		return nil, fmt.Errorf("dialog text is empty, so it has no length byte")
	}
	length := int(pascal[0])
	if length+1 > len(pascal) {
		return nil, fmt.Errorf("dialog text declares %d bytes but holds %d", length, len(pascal)-1)
	}
	if length >= DialogTextBufferSize {
		return nil, fmt.Errorf("dialog text of %d bytes does not fit a %d byte buffer",
			length, DialogTextBufferSize)
	}
	out := make([]byte, length+1)
	copy(out, pascal[1:1+length])
	// The terminator is already zero, which is what the reference writes.
	return out, nil
}

// QuestionAnswer reports what a script sees from a MessageBox return value. The
// reference compares against IDYES, so every other answer including the cancel
// path and the window close is false. A port that treated "not yes" as an error
// would report a normal user choice as a failure.
func QuestionAnswer(messageBoxReturn int32) bool {
	return messageBoxReturn == IDYes
}

// optionkey 20058 is a verified stub. `FUN_0042EB70` has a body of exactly
// `return 0;` and nothing else, so the builtin always reports false regardless of
// any state. It is recorded as a stub rather than left looking unconverted,
// because "always false" is a real, testable behaviour and reimplementing it from
// the keyword would be an invention. If a later build wires up an actual option
// key, this is the place to change.
const OptionKeyOpcode uint16 = 20058

// OptionKeyValue is what optionkey always returns.
const OptionKeyValue int16 = 0

// ReadOptionKey resolves the optionkey builtin. It is deliberately a separate
// function rather than a FlagAccessor entry alone, because ReadFlag is a pure
// function of the raw slot value and would happily report whatever it was handed.
// The native `FUN_0042EB70` has a body of exactly `return 0;`, so the value is
// not read from anywhere and there is no slot to pass in. Keeping the resolver
// separate means a caller cannot accidentally wire optionkey to a live slot.
func ReadOptionKey() ValueResult {
	return BoolResult(false)
}

// wavevolume 16033 resolves through a guard the other accessors do not have.
// Verified FUN_00434970:
//
//	if (DAT_0045E068 == 0) return 0;      // audio not initialised
//	FUN_0042FDD0(&value);
//	return value;                         // a 16-bit value
//
// So it returns zero when the audio system is uninitialised rather than reporting
// a failure, which is why it can never produce a non-zero status.
const (
	// WaveVolumeOpcode is the command opcode.
	WaveVolumeOpcode uint16 = 16033
	// AudioInitialisedSlot is the global that must be non-zero for a real
	// reading, DAT_0045E068.
	AudioInitialisedSlot = "DAT_0045E068"
)

// WaveVolume resolves the wavevolume builtin. initialised reports whether the
// audio system is up, and read gives the system's 16-bit reading, which is
// ignored when the system is down because the reference returns before asking.
func WaveVolume(initialised bool, read int16) int32 {
	if !initialised {
		return 0
	}
	return int32(read)
}

// CodepageSlot is the global holding the code page both dialogs convert through,
// DAT_00445884. It is passed to the conversion helper FUN_00428D70 as its third
// argument.
const CodepageSlot = "DAT_00445884"
