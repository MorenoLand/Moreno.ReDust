package scripts

import "fmt"

// TextDraw is one drawstring call that passed the native checks, ready for the
// host to draw. Text is the Pascal string after FUN_0042D8B0's sanitising. X and
// Y are the baseline-left point: TA_BASELINE | TA_UPDATECP makes the text start
// at the MoveToEx point, and TextOutA's own x and y are ignored. Bold is false
// only for the face selector 0x10, which selects "Raven Digital" weight 400
// (FUN_0042CE00). Every other selector gives "Arial" weight 700. Height is
// nHeight, which is -ctx[0xF] (FUN_0042CE00 passes it to CreateFontA).
type TextDraw struct {
	Text     string
	X, Y     int
	Selector int16
	Bold     bool
	Height   int32
}

// The native text context is the 0x68-byte block at DAT_0045992C. FUN_0042D380
// makes the context the block at +0x10 when its first dword is zero, which it is
// at startup. The fields drawstring reads sit in the block's dwords: the origin
// that MoveToEx subtracts (ctx[0x14] and ctx[0x15]) is dwords 0x18 and 0x19, and
// the height (ctx[0xF]) is dword 0x13. The block is in .bss, so it starts at zero,
// and no cached export writes the origin or the height. Both are therefore zero
// here. This is a reading of the cached exports, not a probe of the binary's
// uncached writers.
const (
	drawstringOriginX = 0
	drawstringOriginY = 0
	drawstringHeight  = 0
)

// drawStringCommand is drawstring(text, point, index, selector), command 12080
// (FUN_00425A00).
//
// FUN_004171C0 evaluates the four arguments in order and requires a comma after
// the first three (0x1C otherwise). An argument's evaluation status is returned
// as it stands. The handler then requires argument one to be a string
// (FUN_00421FC0, 0x0E otherwise). Arguments two to four are not type checked: the
// handler reads the word at record +4 of argument two as x and the word at +2 as
// y, the word at +2 of argument four as the face selector, and it writes argument
// three's word and overwrites it with argument four's, so argument three has no
// effect. The index argument is otherwise unused (FUN_0042CEC0 gets the constant
// 0 from the handler).
//
// The handler draws before the dispatcher's close check. A missing close after
// the four arguments therefore still draws, and the call then returns status 2.
//
// Not reproduced: the gate DAT_00444568 (drawing is skipped while the app is
// deactivated; the port has no such flag, so the draw is always made), the
// string-slot diagnostics 0x1068 and 0x1069 (the port's strings are not slot
// references), and the raw record words of a non-number argument (the port's
// values have no raw words, so a string argument reads as 0 for x, y and the
// selector).
func (h *GameHost) drawStringCommand(call *ScriptCall) (int, uint16, error) {
	if !hostOpen(call) {
		return 0, ScriptStatusMalformed, nil
	}
	var values [4]ScriptValue
	position := call.Start + 2
	for index := range values {
		value, next, status, err := clutValue(call, position)
		if err != nil || status != 0 {
			return 0, status, err
		}
		values[index] = value
		position = next
		if index < len(values)-1 {
			if call.Kind(position) != opComma {
				return 0, ScriptStatusMissingComma, nil
			}
			position++
		}
	}
	if values[0].Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	closed := call.Kind(position) == opClose
	if h.Env.DrawString == nil {
		return 0, 0, fmt.Errorf("%w: drawstring", ErrHostOpcodeUnimplemented)
	}
	// The native code draws unconditionally once the arguments are good, so the
	// draw happens here even if the close check fails below.
	point := unpackPoint(int32(drawWord(values[1])))
	selector := int16(drawWord(values[3]))
	text := values[0].Text
	if len(text) >= 0x80 {
		// FUN_0042D900 passes the Pascal length as a signed char, so a length of
		// 0x80 or more is a negative count. What TextOutA does then is not
		// verified, so nothing is drawn.
		text = ""
	}
	draw := TextDraw{
		Text:     nativeGDIText(text),
		X:        int(point[0]) - drawstringOriginX,
		Y:        int(point[1]) - drawstringOriginY,
		Selector: selector,
		Bold:     selector != 0x10,
		Height:   -drawstringHeight,
	}
	if err := h.Env.DrawString(draw); err != nil {
		return 0, 0, err
	}
	if !closed {
		return 0, ScriptStatusMalformed, nil
	}
	return position + 1 - call.Start, 0, nil
}

// drawWord is the word the handler reads from an argument's record. A number or
// a boolean keeps its word in the port's Int (the low 16 bits are the word at
// record +2). A string keeps a string-slot index there, which the port does not
// track, so it reads as zero.
func drawWord(value ScriptValue) int32 {
	if value.Kind == 3 {
		return 0
	}
	return value.Int
}

// nativeGDIText is FUN_0042D8B0, the Pascal-to-C copy that drawstring makes before
// TextOutA. Bytes 0x20 to 0x7A are kept. Byte 0xD5 becomes an apostrophe, and
// every other byte becomes a space, including 0x7F.
func nativeGDIText(text string) string {
	out := make([]byte, len(text))
	for index := 0; index < len(text); index++ {
		b := text[index]
		switch {
		case b >= 0x20 && b <= 0x7a:
			out[index] = b
		case b == 0xd5:
			out[index] = '\''
		default:
			out[index] = ' '
		}
	}
	return string(out)
}
