package scripts

import "fmt"

// The no-argument accessors. A large share of the boot script is calls that read
// one piece of global state and return it, with no argument parsing and no
// failure path. They are grouped here because they are the cheapest handlers to
// get right and the easiest to get subtly wrong: a wrong result type or a
// missing guard shows up as a script comparing a number against a string.
//
// Every one of them is `*status = 0; result.Type = T; result.Value = V;
// result.Tail = 0; return 0`, with T either 2 for a boolean or 4 for a number.

// FlagAccessor describes a verified no-argument reader. The distinction that
// matters is ResultType: a type-2 result is a boolean and a type-4 result is a
// number, and a script comparing a boolean against a number would behave
// differently than one comparing two numbers.
type FlagAccessor struct {
	// Opcode is the command or value opcode the dispatcher routes here.
	Opcode uint16
	// Name is the script keyword.
	Name string
	// Handler is the native function the verified switch calls.
	Handler string
	// ResultType is the verified result record type, 2 or 4.
	ResultType uint16
	// Source describes where the value comes from, for error messages and for
	// the tests to assert the mapping is traceable.
	Source FlagSource
	// SignExtendedFrom records the width the reference sign-extends from, when
	// the value comes from a narrower field. Zero means the value is already a
	// 32-bit field or a naturally widened short.
	SignExtendedFrom int
}

// FlagSource names where an accessor reads from.
type FlagSource uint8

const (
	// SourceGlobal32 reads a 32-bit global directly.
	SourceGlobal32 FlagSource = iota
	// SourceGlobalShort reads a global and sign-extends it from 16 bits.
	SourceGlobalShort
	// SourceQuery reads the result of a no-argument native query.
	SourceQuery
)

// FlagAccessors is the verified set, transcribed from live Ghidra decompilation:
//
//	menuvisible 16037 FUN_00414F20  type 2  *(int *)(out + 1) = DAT_00459680
//	keyaborts   16045 FUN_00414F50  type 2  *(int *)(out + 1) = DAT_00459996
//	optionkey   20058 FUN_00416BC0  type 2  value = (int)FUN_0042EB70()
//	wavevolume  16033 FUN_00415BF0  type 4  value = (int)FUN_00434970()
//	framerate   16022 FUN_00416540  type 4  value = DAT_004599CE
//
// The two boolean readers copy a raw global with no normalisation, exactly as
// `propvisible` does, so a slot holding 7 yields a boolean 7.
var FlagAccessors = []FlagAccessor{
	{16037, "menuvisible", "FUN_00414F20", ValueTypeBool, SourceGlobal32, 0},
	{16045, "keyaborts", "FUN_00414F50", ValueTypeBool, SourceGlobal32, 0},
	{20058, "optionkey", "FUN_00416BC0", ValueTypeBool, SourceQuery, 16},
	{16033, "wavevolume", "FUN_00415BF0", ValueTypeNumeric, SourceQuery, 16},
	{16022, "framerate", "FUN_00416540", ValueTypeNumeric, SourceGlobal32, 0},
}

// The global slots the two boolean readers copy. They are recorded by name so a
// diagnostic can point at the reference's storage.
const (
	// MenuVisibleSlot is DAT_00459680, the slot menuvisible reports.
	MenuVisibleSlot = "DAT_00459680"
	// KeyAbortsSlot is DAT_00459996, the slot keyaborts reports.
	KeyAbortsSlot = "DAT_00459996"
	// FrameRateSlot is DAT_004599CE, the slot framerate reports.
	FrameRateSlot = "DAT_004599CE"
)

// FlagAccessorFor returns the verified accessor for an opcode.
func FlagAccessorFor(opcode uint16) (FlagAccessor, bool) {
	for _, accessor := range FlagAccessors {
		if accessor.Opcode == opcode {
			return accessor, true
		}
	}
	return FlagAccessor{}, false
}

// ReadFlag produces the result record for an accessor from a raw slot value. The
// value is written into a type-2 or type-4 record with a zero tail, matching the
// reference, which writes the type word, the value dword and the tail in that
// order and never normalises the value.
func ReadFlag(accessor FlagAccessor, raw int32) ValueResult {
	result := ValueResult{Type: accessor.ResultType, Value: raw, Tail: 0}
	if accessor.SignExtendedFrom == 16 {
		// The reference casts a short, which sign-extends. A slot holding a
		// negative 16-bit value therefore arrives negative, not as its low bits.
		result.Value = int32(int16(uint16(raw)))
	}
	return result
}

// CursorResourceName is the value the `cursor` builtin 12039 passes when its
// argument is numeric. Verified `FUN_00426230`:
//
//	if (arg.Kind == 4) {
//	    if (FUN_0042ED60(0x43555253) == NULL) return 0x2B;
//	} else { ... compare the name against a pool string ... }
//
// **The byte order here is worth stating exactly, because it is not what it
// looks like.** The instruction is `68 53 52 55 43`, that is `PUSH 0x43555253`,
// confirmed by reading the reference image at `0x004262EE`. The four bytes in
// address order are therefore `53 52 55 43`, which read as ASCII in that order is
// "SRUC" and **not** "CURS". The four characters C, U, R, S are the immediate's
// nibble-pairs read from the most significant end, which is how the original
// author assembled it rather than as a string literal in memory.
//
// So a port must pass the value 0x43555253 and must not reconstruct it from a
// "CURS" string, which would produce 0x53525543 and a different resource. A named
// cursor argument takes a different path entirely: it is compared against a pool
// string, and a numeric argument means the default cursor. A missing default
// cursor resource is status 0x2B, distinct from every other status here.
const (
	// CursorDefaultResource is 0x43555253, the verified PUSH immediate.
	CursorDefaultResource uint32 = 0x43555253
	// CursorDefaultImmediateAddress is where that PUSH sits in the reference.
	CursorDefaultImmediateAddress uint32 = 0x004262EE
	// CursorDefaultMemoryBytes is the immediate's byte order in memory, verified
	// as 53 52 55 43, which is the opposite of the "CURS" order.
	CursorDefaultMemoryBytes = "SRUC"
	// StatusCursorUnavailable is native status 0x2B, returned when the default
	// cursor resource cannot be loaded.
	StatusCursorUnavailable uint16 = 0x2B
)

// CursorNamePoolAddress is the engine string pool address the `cursor` builtin
// compares a named argument against, verified from `FUN_00427FC0(&DAT_0045DA4C)`
// in the same function. It lies outside the pool this project transcribed, so the
// address is recorded rather than a guessed name.
const CursorNamePoolAddress uint32 = 0x0045DA4C

// The "None" fallback family. Three name accessors return the same capitalised
// fallback string when their context slot is empty, and all three reach it
// through the same pool address:
//
//	currentset   20053 FUN_0041B2F0  if (DAT_00459A24 == 0) -> "None"
//	currentpuppet 20049 FUN_00408CF0  if (DAT_00459A92 == 0) -> "None"
//	currentscene 16029 FUN_0041B3E0  if (DAT_00459A24 == 0) -> "None"
//
// The address is 0x0045D07C and the text begins one byte later, at 0x0045D07D.
// `currentflat` 20038 is the odd one out: it falls back to the lowercase "none"
// at 0x0045D368 instead. That difference is preserved because a script can
// compare against either spelling, and collapsing them would make one of the
// comparisons fail.

// NoneFallbackPoolAddress is the pool address of the capitalised fallback, whose
// text begins one byte later.
const NoneFallbackPoolAddress uint32 = 0x0045D07C

// NoneFallbackLowerPoolAddress is the pool address of the lowercase fallback
// used by currentflat, likewise one byte before its text.
const NoneFallbackLowerPoolAddress uint32 = 0x0045D368

// NoneNameAccessor describes one member of the capitalised-fallback family.
type NoneNameAccessor struct {
	// Opcode is the command or value opcode.
	Opcode uint16
	// Name is the script keyword.
	Name string
	// Handler is the native function.
	Handler string
	// GuardSlot is the global whose being zero means no context, and the
	// therefore reason for the fallback.
	GuardSlot string
	// LiveNameSlot is where the live name is read from when the guard passes.
	LiveNameSlot string
}

// NoneNameAccessors is the verified set. currentset and currentscene share the set
// slot DAT_00459A24, while currentpuppet has its own.
var NoneNameAccessors = []NoneNameAccessor{
	{20053, "currentset", "FUN_0041B2F0", "DAT_00459A24", "DAT_00459A82"},
	{20049, "currentpuppet", "FUN_00408CF0", "DAT_00459A92", "DAT_00459AAE"},
	{16029, "currentscene", "FUN_0041B3E0", "DAT_00459A24", "resolved"},
}

// NoneNameAccessorFor returns the verified accessor for an opcode.
func NoneNameAccessorFor(opcode uint16) (NoneNameAccessor, bool) {
	for _, accessor := range NoneNameAccessors {
		if accessor.Opcode == opcode {
			return accessor, true
		}
	}
	return NoneNameAccessor{}, false
}

// ResolveNoneName applies a member's guard and returns either the live name or the
// capitalised fallback. active reports whether the guard slot is non-zero, and
// liveName is the name to use when it is. The fallback is returned verbatim,
// including its capital N, because that is the reference's spelling.
func ResolveNoneName(accessor NoneNameAccessor, active bool, liveName string) string {
	if active {
		return liveName
	}
	return NameNoneUpper
}

// ResolveCurrentFlatName is the lowercase counterpart, used only by currentflat.
// It is kept separate from ResolveNoneName so the two spellings cannot be
// conflated, and it additionally resolves through the flat name table when a flat
// is active.
func ResolveCurrentFlatName(active bool, liveName string) (string, error) {
	if !active {
		return NameNone, nil
	}
	if liveName == "" {
		return "", fmt.Errorf("currentflat has an active flat but an empty name")
	}
	return liveName, nil
}

// StatusPassThrough describes a handler that does no parsing and simply returns
// another function's status, which the reference writes as the idiom
// `((status == 0) - 1) & status`, which is just the status unchanged. Two boot
// handlers use it: `forceupdate` 12052, which returns FUN_0040F4E0's status, and
// `message` 12001, which returns the string register load's status after storing
// the text into the message slot.
type StatusPassThrough struct {
	// Opcode is the command opcode.
	Opcode uint16
	// Name is the script keyword.
	Name string
	// Handler is the native function.
	Handler string
	// Inner is the native function whose status is propagated.
	Inner string
	// SideEffectSlot is the global the handler writes on the way, if any.
	SideEffectSlot string
}

// StatusPassThroughs is the verified set.
var StatusPassThroughs = []StatusPassThrough{
	{12052, "forceupdate", "FUN_00426200", "FUN_0040F4E0", ""},
	{12001, "message", "FUN_00425B70", "FUN_00421FC0", "DAT_00459CE0"},
}

// MessageSlot is the pending-message buffer the `message` builtin writes, which
// is distinct from the shared scratch DAT_00459AD0 that most string loads use.
const MessageSlot = "DAT_00459CE0"
