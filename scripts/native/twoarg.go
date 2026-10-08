package native

import (
	. "redust/scripts"
	"fmt"
)

// The general two-argument call form, and the two sides of opcode 16011, which
// turn out not to be a getter and setter of the same thing.

// `FUN_00417070` evaluates an argument list of the shape `arg1 , arg2`:
//
//	u = FUN_004220A0(frame, at, out1, &consumed1);
//	if (u == 0) {
//	    next = at + consumed1 * 8;
//	    if (*next != 0x0FB4) return 0x1C;         // the comma is REQUIRED
//	    next += 4;
//	    u = FUN_004220A0(frame, next, out2, &consumed2);
//	    if (u == 0) { *consumed = (next + (consumed2*8 - at)) >> 3; return 0; }
//	}
//	return u;
//
// **This corrects the scope of status `0x1C`.** It was first found in the sendto
// inner handlers and recorded there as sendto's rule. It is not: this function is
// the general two-argument evaluator, and the reference has at least fifteen
// callers of it, among them `propxy` 16018, `pointinprop` 20040 and `substring`
// 20055. So `0x1C` means "a two-argument list is missing its comma" everywhere in
// the engine, and the sendto handlers are simply two more callers. The consumed
// count is in eight-byte record units, as everywhere else.
const (
	// TwoArgFormCallerCountFloor is a floor on how many handlers call
	// FUN_00417070, from the cross-references. It is recorded as a floor because
	// the reference list was truncated, and because the point is that the count is
	// well above one.
	TwoArgFormCallerCountFloor = 15
	// TwoArgFormHandler is the evaluator's own address, for traceability.
	TwoArgFormHandler = "FUN_00417070"
	// ArgRecordBytes is the record size the form walks in, 8.
	ArgRecordBytes = 8
)

// TwoArgForm is the result of parsing a two-argument list.
type TwoArgForm struct {
	// First and Second are the two evaluated argument records.
	First  Record
	Second Record
	// Consumed is how many eight-byte records the whole list spanned, including
	// the comma.
	Consumed int
}

// ParseTwoArgForm validates the shape of a two-argument list without evaluating
// it. records is the kind stream starting at the first argument, and the comma
// must sit at index 1. It returns the index just past the second argument, so a
// caller that evaluates in two steps knows where to resume.
func ParseTwoArgForm(kinds []uint16) (afterSecond int, status uint16, err error) {
	if len(kinds) < 2 {
		return 0, StatusMissingComma, fmt.Errorf("a two-argument list needs a second record to hold the comma")
	}
	if kinds[1] != SendToCommaKind {
		return 0, StatusMissingComma, fmt.Errorf("a two-argument list expects %q after its first argument, found kind %d",
			",", kinds[1])
	}
	// The second argument begins at index 2. Its own extent is not known without
	// evaluating it, so the resume point is the start of it.
	return 2, 0, nil
}

// TwoArgConsumed computes the consumed record count the reference records, given
// the first argument's extent and the second's. It is the second argument's start
// plus its extent, relative to the list start, in eight-byte records.
func TwoArgConsumed(firstRecords, secondRecords int) int {
	return firstRecords + 1 + secondRecords
}

// FacingNameToCode is the command side's name-to-code mapping, from the verified
// tail of `FUN_0041ACE0`. The four facings map to **1 through 4**, and a name that
// is none of them yields status `0x0A`.
const (
	// FacingCodeNorth is 1.
	FacingCodeNorth = 1
	// FacingCodeSouth is 2.
	FacingCodeSouth = 2
	// FacingCodeEast is 3.
	FacingCodeEast = 3
	// FacingCodeWest is 4.
	FacingCodeWest = 4
	// FacingCodeCount is how many facings the setter accepts.
	FacingCodeCount = 4
)

// FacingCode maps a facing name to the command side's code, and reports whether
// the name was one of the four. The comparison is case-folded, since it uses the
// same `FUN_0042E5B0` as every other name comparison in the engine.
func FacingCode(name string) (int, bool) {
	switch {
	case EqualASCIIFold(name, PoolFacingNorth):
		return FacingCodeNorth, true
	case EqualASCIIFold(name, PoolFacingSouth):
		return FacingCodeSouth, true
	case EqualASCIIFold(name, PoolFacingEast):
		return FacingCodeEast, true
	case EqualASCIIFold(name, PoolFacingWest):
		return FacingCodeWest, true
	}
	return 0, false
}

// The two sides of opcode 16011 operate on **different state with different
// encodings**, which is the finding that matters here and the reason they are not
// modelled as a getter and setter of one value.
//
// The value side, `FUN_0041B070`, takes no argument and returns a **name**. It
// switches on the global `DAT_00459A6A` with four cases at `0x00`, `0x40`, `0x80`
// and `0xC0`, a stride of `0x40`.
//
// The command side, `FUN_0041ACE0`, takes a **name** and stores a **code** into
// the global `DAT_00459A62` with values **1 through 4**. The two globals differ by
// eight bytes and the encodings do not correspond: the setter's 1 is north, while
// the getter's case `0x00` is east and its `0x40` is south.
//
// So a port that treated the pair as one value would read a field the setter never
// writes. Verified command-side body, in full:
//
//	if (DAT_00459A24 == 0) return 0x28;
//	evaluate the name argument
//	code = 0;
//	if (name matches "east")  code = 3;
//	if (name matches "north") code = 1;
//	if (name matches "south") code = 2;
//	if (name matches "west")  code = 4;
//	if (code == 0) return 0x0A;
//	if (DAT_00459A62 == code) return 0;              // already facing that way
//	saved = *(int *)(DAT_00459A2C + 0xD34);
//	if (FUN_00419C70() == 0) {
//	    if (DAT_00459A24 == 0) return status;
//	    if (*(int *)(DAT_00459A2C + 0xD34) != saved) return success;
//	    FUN_00405EA0(-1, -1, code);
//	    return FUN_00419BC0() remapped;
//	}
//
// Two more details are load-bearing. Setting the direction the actor already faces
// is a **no-op that returns success**, checked before any work. And the setter
// carries **the same re-entrancy guard as `closepuppetfile`**: it samples the
// field at `+0xD34`, and either the set disappearing or that field changing makes
// it return success without doing the work.
const (
	// CurrentDirSetterSlot is the global the command side writes, DAT_00459A62.
	CurrentDirSetterSlot = "DAT_00459A62"
	// CurrentDirGetterSlot is the global the value side reads, DAT_00459A6A. The
	// two are distinct, which is why the two sides are modelled separately.
	CurrentDirGetterSlot = "DAT_00459A6A"
	// CurrentDirGuardFieldOffset is the re-entrancy field both this setter and
	// closepuppetfile sample, at 0xD34. The offset is shared; the block it indexes
	// is not, since one is a set and the other a puppet.
	CurrentDirGuardFieldOffset = 0xD34
	// CurrentDirSetterCall is the function that performs the turn, taking -1, -1
	// and the code.
	CurrentDirSetterCall = "FUN_00405EA0"
	// CurrentDirSetterArgs is the literal first two arguments the turn receives.
	// They are -1 and -1, meaning "no explicit target".
	CurrentDirSetterArgs = -1
)

// SetCurrentDirOutcome is what the command side did.
type SetCurrentDirOutcome uint8

const (
	// FacingSet means the turn was performed.
	FacingSet SetCurrentDirOutcome = iota
	// FacingUnchanged means the actor already faced that way, so nothing was done
	// and success was still returned.
	FacingUnchanged
	// FacingSetVanished means the set disappeared across the re-entrancy window.
	FacingSetVanished
	// FacingSetDeferred means the underlying call refused.
	FacingSetDeferred
)

// SetCurrentDir reproduces the command side's decision. name is the requested
// facing, current is the code already stored in DAT_00459A62, setActive reports the
// set slot, and guardBefore/guardAfter are the field at 0xD34 sampled either side of
// the underlying call.
func SetCurrentDir(name string, current int, setActive bool, guardBefore, guardAfter int32, underlying uint16) (SetCurrentDirOutcome, int, uint16) {
	if !setActive {
		return FacingUnchanged, 0, StatusNoActiveSet
	}
	code, known := FacingCode(name)
	if !known {
		// A name that is none of the four facings is not a facing at all.
		return FacingUnchanged, 0, StatusNameNotFound
	}
	if current == code {
		// Already facing that way: a no-op that still reports success.
		return FacingUnchanged, code, 0
	}
	if underlying != 0 {
		return FacingSetDeferred, code, underlying
	}
	if !setActive {
		return FacingSetVanished, code, 0
	}
	if guardAfter != guardBefore {
		// The block changed underneath, so report success without turning.
		return FacingSetVanished, code, 0
	}
	return FacingSet, code, 0
}

// FacingCodeIsNotTheGetterEncoding asserts the finding that the setter's codes and
// the getter's cases are different numberings of different things. It exists so
// nobody later "fixes" one to match the other.
func FacingCodeIsNotTheGetterEncoding() bool {
	// The setter writes 1..4; the getter reads 0x00, 0x40, 0x80, 0xC0.
	for _, code := range []int{FacingCodeNorth, FacingCodeSouth, FacingCodeEast, FacingCodeWest} {
		if code == int(CurrentDirStride) || code == 2*int(CurrentDirStride) ||
			code == 3*int(CurrentDirStride) || code == 4*int(CurrentDirStride) {
			return false
		}
	}
	// And the globals differ.
	return CurrentDirSetterSlot != CurrentDirGetterSlot
}

// SetNumericField is `FUN_0041B450`, a numeric accessor the value dispatcher calls
// directly. Verified body:
//
//	*out_status = 0;
//	if (DAT_00459A24 == 0) return 0x28;               // no set
//	block = GlobalLock(DAT_00459A30);
//	value = *(short *)(block + 0x26);
//	GlobalUnlock;
//	*out = 4;                                          // type 4, numeric
//	*(int *)(out + 1) = value;
//	out[3] = 0;
//
// So it reads a 16-bit field at offset `0x26` of the set block and returns it as a
// number, with the same set guard the other set accessors use.
//
// Its opcode is **not** claimed. It is called from the value dispatcher at
// `0x00414080`, but that call site is not one of the switch cases the transcribed
// tables record, so identifying which opcode routes to it needs the case label at
// that address. The address is recorded so the question is answerable rather than
// forgotten.
const (
	// SetNumericFieldHandler is the native function.
	SetNumericFieldHandler = "FUN_0041B450"
	// SetNumericFieldDispatchAddress is where the value dispatcher calls it.
	SetNumericFieldDispatchAddress uint32 = 0x00414080
	// SetNumericFieldOffset is the field it reads, at block offset 0x26.
	SetNumericFieldOffset = 0x26
	// SetNumericFieldWidth is that field's width, 16 bits, so the value is
	// sign-extended on the way into the 32-bit result.
	SetNumericFieldWidth = 16
)

// ReadSetNumericField reproduces FUN_0041B450. block is the locked set block.
func ReadSetNumericField(block []byte, setActive bool) (ValueResult, uint16, error) {
	if !setActive {
		return ValueResult{}, StatusNoActiveSet, nil
	}
	if SetNumericFieldOffset+2 > len(block) {
		return ValueResult{}, 0, fmt.Errorf("the set field at +%#x is past the %d byte block",
			SetNumericFieldOffset, len(block))
	}
	value := int32(int16(uint16(block[SetNumericFieldOffset]) | uint16(block[SetNumericFieldOffset+1])<<8))
	return NumericResult(value), 0, nil
}
