package scripts

import "fmt"

// The script-entry family, from live Ghidra decompilation of the five
// FUN_00424890 handlers that enter a child script context. All five share one
// shape, which the reference implements as four steps:
//
//	guard                       the required context must exist
//	scratch = label             FUN_0042E6C0(label, &DAT_00459BD0)
//	scratch = label + name      FUN_0042E660(name, &DAT_00459BD0)
//	clear the result buffer       FUN_00418930()
//
// The assembled name is therefore "Stage Script: " followed by the resolved
// stage name, and friends. Note the append direction: FUN_0042E660 appends its
// *first* argument onto its *second*, so the call sites read
// FUN_0042E660(name, scratch) and the label ends up first.

// StatusNoActiveStage is native status 0x0B, returned by every stage-context
// guard. Two handlers use it with opposite polarity: stagescript and flatscript
// return it when no stage is open, while openstagefile returns it when one
// already is.
const StatusNoActiveStage uint16 = 0x0B

// StatusNoActiveSet is native status 0x28, returned by the set-context guard.
const StatusNoActiveSet uint16 = 0x28

// PascalMaxLength is the 255-byte cap the native append enforces. The overflow
// test is `0xff < (int)(short)srcLen + (uint)dstLen`.
const PascalMaxLength = 255

// The two arguments the native handlers pass to FUN_0042C470 when an append
// overflows the 255-byte Pascal cap, and when a scene context teardown fails.
// FUN_0042C470 is the engine's diagnostic raiser; the pair is recorded so the
// Go port can report the same values the reference would.
const (
	// ScriptAppendDiagA and ScriptAppendDiagB are the arguments of
	// FUN_0042C470(0x6E, 0x325) raised by FUN_0042E660 on overflow.
	ScriptAppendDiagA uint16 = 0x6E
	ScriptAppendDiagB uint16 = 0x325
	// ScriptSceneTeardownDiagB is the second argument of the
	// FUN_0042C470(0, 0x1521) raised by scenescript when its teardown fails.
	ScriptSceneTeardownDiagB uint16 = 0x1521
)

// PascalCopy mirrors FUN_0042E6C0, the native Pascal string copy. The reference
// reads the length byte then runs a do-while, so it always copies the length
// byte plus exactly that many text bytes, and it truncates the declared length
// to 0xFF on read. A malformed source with no length byte yields an empty
// result rather than a panic.
func PascalCopy(source []byte) []byte {
	if len(source) == 0 {
		return []byte{0}
	}
	length := int(source[0])
	if length > PascalMaxLength {
		length = PascalMaxLength
	}
	if 1+length > len(source) {
		length = len(source) - 1
	}
	if length < 0 {
		length = 0
	}
	out := make([]byte, 0, 1+length)
	out = append(out, byte(length))
	return append(out, source[1:1+length]...)
}

// PascalAppend mirrors FUN_0042E660, the native Pascal string append:
//
//	*dst = *dst + *src
//	if 0xff < *src + *dst -> FUN_0042C470(0x6E, 0x325)
//	tail = dst + *dst + 1
//	for i in 0 .. *src-1: *tail++ = *src++
//
// so the result is the destination text with the source text appended, and an
// overflowing result is a native diagnostic rather than a silent truncation.
// The Go port returns the same condition as an error instead of raising, and
// carries the two diagnostic arguments so a caller can report them.
func PascalAppend(destination, source []byte) ([]byte, error) {
	dst := PascalCopy(destination)
	src := PascalCopy(source)
	if len(src) == 0 {
		return dst, nil
	}
	srcLen := int(src[0])
	dstLen := int(dst[0])
	if PascalMaxLength < srcLen+dstLen {
		return nil, fmt.Errorf("pascal append would reach %d bytes, over the %d byte cap (FUN_0042C470(%#x, %#x))",
			srcLen+dstLen, PascalMaxLength, ScriptAppendDiagA, ScriptAppendDiagB)
	}
	if 1+srcLen+dstLen > cap(dst) {
		grown := make([]byte, 1+srcLen+dstLen)
		copy(grown, dst[:1+dstLen])
		dst = grown
	}
	dst[0] = byte(dstLen + srcLen)
	copy(dst[1+dstLen:1+dstLen+srcLen], src[1:1+srcLen])
	return dst, nil
}

// ScriptEntryGuard is the precondition a script-entry handler checks before
// assembling a context name.
//
// The guard is not always in the handler itself. scenescript 12063's neighbour
// scenescript 12036 is the case in point: FUN_0041AAB0 carries no guard of its
// own, but the resolver it calls first, FUN_0041B750, opens with
// `if (DAT_00459A24 == 0) return 0x28;`, so the handler is guarded by a set
// exactly as if it had checked inline. An earlier draft recorded scenescript as
// unguarded; the resolver decompile is what corrected it.
type ScriptEntryGuard uint8

const (
	// GuardNone is bootscript, which has no precondition: verified FUN_004182F0
	// assembles its label and enters the context unconditionally, which is
	// correct because booting is the entry point.
	GuardNone ScriptEntryGuard = iota
	// GuardRequireStage demands an open stage.
	GuardRequireStage
	// GuardRequireSet demands an active set.
	GuardRequireSet
)

// ScriptEntrySpec describes one verified script-entry handler.
type ScriptEntrySpec struct {
	// Opcode is the command opcode the dispatcher routes here.
	Opcode uint16
	// Name is the opcode's script keyword.
	Name string
	// Handler is the native function the verified switch calls.
	Handler string
	// Label is the context label, transcribed from the engine string pool.
	Label string
	// Guard is the precondition the handler checks first.
	Guard ScriptEntryGuard
	// TakesName reports whether the handler resolves a name from an evaluated
	// argument. The three that do are flatscript, scenescript and openstagefile;
	// stagescript, setscript and bootscript read the name from a live slot.
	TakesName bool
}

// scriptEntrySpecs is the verified family. Every Handler and Label is
// cross-checked against the transcribed tables and the string pool by the tests,
// so this table cannot drift from either.
var scriptEntrySpecs = []ScriptEntrySpec{
	{12031, "bootscript", "FUN_004182F0", PoolLabelBootScript, GuardNone, false},
	{12035, "setscript", "FUN_0041AA40", PoolLabelSetScript, GuardRequireSet, false},
	{12036, "scenescript", "FUN_0041AAB0", PoolLabelSceneScript, GuardRequireSet, true},
	{12063, "stagescript", "FUN_00412840", PoolLabelStageScript, GuardRequireStage, false},
	{12064, "flatscript", "FUN_004128B0", PoolLabelFlatScript, GuardRequireStage, true},
}

// ScriptEntryFor returns the verified spec for a script-entry opcode.
func ScriptEntryFor(opcode uint16) (ScriptEntrySpec, bool) {
	for _, spec := range scriptEntrySpecs {
		if spec.Opcode == opcode {
			return spec, true
		}
	}
	return ScriptEntrySpec{}, false
}

// ScriptContext is the live state the guards read. A zero value means nothing
// is open, which is the state every shipped stage-context guard rejects.
type ScriptContext struct {
	// StageOpen reports an open stage, the DAT_00459A00 slot.
	StageOpen bool
	// SetActive reports an active set, the DAT_00459A24 slot.
	SetActive bool
	// HitDataReady is the second slot FUN_0041B330's pointinset hit test
	// requires alongside the set.
	HitDataReady bool
}

// ScriptContextState is the mutable state the entry path updates: the shared
// scratch the handlers assemble into and the result buffer FUN_00418930 clears.
type ScriptContextState struct {
	// Scratch is DAT_00459BD0, the shared Pascal buffer the family builds names in.
	Scratch []byte
	// ResultBuffer is DAT_00459720. It is the slot FUN_00418930 clears, and it is
	// also what the `result` builtin reads: verified FUN_00415C20, the handler
	// for opcode 16010, is just `*status = 0; FUN_00421F60(&DAT_00459720, out)`.
	//
	// An earlier draft called this the "running script name" slot. That was a
	// guess from the name; the `result` handler is the evidence, and it makes the
	// relationship concrete: the script-entry family clears the result buffer on
	// the way into a new context, and `result` hands the engine's stored result
	// back to the script.
	ResultBuffer []byte
}

// ClearResultBuffer mirrors FUN_00418930, which copies the empty C string at
// 0x0045D4E1 into DAT_00459720. Verified: the address holds four zero bytes
// between the "Stage Message: " terminator and "button", so the native call is a
// Pascal copy of length zero and the slot ends up holding a single zero byte.
func ClearResultBuffer(state *ScriptContextState) {
	if state == nil {
		return
	}
	state.ResultBuffer = []byte{0}
}

// ScriptEntryBlocked evaluates a spec's guard and returns the native status the
// handler would return, or 0 when the guard passes. A handler with no guard
// never blocks.
func ScriptEntryBlocked(spec ScriptEntrySpec, ctx ScriptContext) uint16 {
	switch spec.Guard {
	case GuardRequireStage:
		if !ctx.StageOpen {
			return StatusNoActiveStage
		}
	case GuardRequireSet:
		if !ctx.SetActive {
			return StatusNoActiveSet
		}
	}
	return 0
}

// OpenStageFileBlocked is the inverted guard on openstagefile, verified in
// FUN_00411860:
//
//	if (DAT_00459A00 != 0) return 0x0B;
//
// It fails when a stage is *already* open, which is the same status constant as
// the require-stage guard with the opposite polarity. Keeping that inversion
// explicit is the point: collapsing the two would let a script reopen a stage
// over the live one.
func OpenStageFileBlocked(ctx ScriptContext) uint16 {
	if ctx.StageOpen {
		return StatusNoActiveStage
	}
	return 0
}

// EnterScriptContext performs the verified three steps of a script-entry
// handler: check the guard, build the context name as the label followed by
// the resolved name, and clear the result buffer. It returns the native
// status; a non-zero status means the scratch was not touched, matching the
// reference, which returns before any copy.
func EnterScriptContext(spec ScriptEntrySpec, ctx ScriptContext, state *ScriptContextState, name string) (uint16, error) {
	if status := ScriptEntryBlocked(spec, ctx); status != 0 {
		return status, nil
	}
	if state == nil {
		return 0, fmt.Errorf("script entry %s needs a context state", spec.Name)
	}
	// Step one: scratch = label. FUN_00427FC0 turns the pool's NUL-terminated
	// text into the length-prefixed form the native code copies.
	scratch := PascalFromCString(spec.Label)
	// Step two: scratch = label + resolved name. This is FUN_0042E660 called
	// with the name first and the scratch second, so the name is appended.
	appended, err := PascalAppend(scratch, PascalFromCString(name))
	if err != nil {
		return 0, err
	}
	state.Scratch = appended
	// Step three: clear the result buffer.
	ClearResultBuffer(state)
	return 0, nil
}
