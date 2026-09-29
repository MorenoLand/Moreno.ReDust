package scripts

import (
	"fmt"
	"strings"
)

// Verified value-result protocol shared by the FUN_004137B0 value builtins,
// from live Ghidra decompilation of the 20000-band handlers the shipped boot
// script uses.
//
// The out parameter is an eight-byte result record, laid out exactly like a
// parsed script record: a 16-bit type at offset 0, a 32-bit value at offset 2
// and a 16-bit tail at offset 6. FUN_00415E20 (mouse) is the clearest witness:
//
//	*out_status = 0;
//	uVar1 = FUN_0042C820(0x45991A, &local_4);
//	*out_record  = 4;                       // type 4, numeric
//	*(dword *)(out_record + 1) = local_4;    // value
//	out_record[3] = 0;                      // tail
//
// Three result types are produced:
//
//	2  boolean, written as *(int *)(out_record + 1) = bVar1
//	3  string, produced by FUN_00421F60, the temporary string register store
//	4  numeric
//
// A handler whose argument is not the required type returns native status 0x0E
// without writing a result. FUN_00415CB0 (pointx), FUN_00415D10 (pointy),
// FUN_004153E0 (hittest), FUN_0041B330 (pointinset) and FUN_0041FA40
// (pointinprop) all test the evaluated argument against type 4 and return 0x0E
// on mismatch.
const (
	// ValueTypeBool is the verified result type 2.
	ValueTypeBool uint16 = 2
	// ValueTypeString is the verified result type 3.
	ValueTypeString uint16 = 3
	// ValueTypeNumeric is the verified result type 4.
	ValueTypeNumeric uint16 = 4
)

// StatusWrongOperandType is native status 0x0E, returned when an evaluated
// argument is not the type the handler requires.
const StatusWrongOperandType uint16 = 0x0E

// ValueResult is the eight-byte out record the value builtins write. It reuses
// the parsed record layout so a result can be pushed straight back onto the
// expression stack.
type ValueResult struct {
	Type  uint16
	Value int32
	Tail  uint16
}

// Record renders the result as a Record so it can enter the expression stack.
func (r ValueResult) Record() Record {
	return Record{
		Kind: r.Type,
		Data: uint32(r.Value),
		Tail: r.Tail,
	}
}

// NumericResult builds a verified type-4 result.
func NumericResult(value int32) ValueResult {
	return ValueResult{Type: ValueTypeNumeric, Value: value, Tail: 0}
}

// BoolResult builds a verified type-2 result.
func BoolResult(value bool) ValueResult {
	out := ValueResult{Type: ValueTypeBool, Tail: 0}
	if value {
		out.Value = 1
	}
	return out
}

// StringResult is the Go-side carrier for a verified type-3 result. The native
// handler allocates a temporary string register through FUN_00421F60 and
// returns that register's type-3 slot; the text itself lives in the register
// pool, so the value is carried here and resolved by the caller.
type StringResult struct {
	Text string
	// Slot is the temporary string register the native store would have
	// returned. It is meaningful once the runtime's StringRegisters is wired to
	// these handlers.
	Slot int
}

// Result builds the type-3 result record for a string.
func (s StringResult) Result() ValueResult {
	return ValueResult{Type: ValueTypeString, Tail: uint16(s.Slot)}
}

// RequireNumericArgument validates the evaluated argument of a value builtin.
// It reproduces the verified `if (type != 4) return 0x0E` guard shared by the
// numeric and coordinate builtins.
func RequireNumericArgument(kind uint16) (uint16, error) {
	if kind != ValueTypeNumeric {
		return StatusWrongOperandType, fmt.Errorf("value builtin requires a numeric argument, got type %d", kind)
	}
	return 0, nil
}

// Verified fallback names for the name-valued builtins, read from the reference
// image. FUN_00427FC0 resolves each of these NUL terminated C strings when the
// corresponding runtime slot is empty.
//
//	0x0045D368 -> "none"   currentflat with no active flat
//	0x0045D07C -> "None"   the capitalised fallback, shared by currentset,
//	                       currentpuppet and currentscene
//	0x0045D0EC -> "actor"  the stage name hittest walks
const (
	// NameNone is the lowercase fallback, used by currentflat.
	NameNone = "none"
	// NameNoneUpper is the capitalised fallback, used by currentset,
	// currentpuppet and currentscene. The reference stores both spellings and the
	// difference is preserved rather than normalised, because a script can
	// compare against either. See flags.go for the accessor family.
	NameNoneUpper = "None"
	// NameActor is the stage name the hittest handler walks.
	NameActor = "actor"
)

// CurrentFlatNameInputSize is the verified record size currentflat copies out of
// the flat table: FUN_004125C0 copies seven DWORDs, 28 bytes, from
// `base + index * 0x1C + 0x838`.
const (
	CurrentFlatNameInputSize = 0x1C
	CurrentFlatTableStride   = 0x1C
	CurrentFlatTableOffset   = 0x838
	CurrentFlatCopyDwords    = 7
)

// CurrentFlatTableEntry is the verified layout of one flat table row: a
// zero-based index, a name slot within the row and the name text.
type CurrentFlatTableEntry struct {
	// Index is the row's own index, matching the `index * 0x1C` addressing.
	Index int
	// Name is the 28-byte name record copied by the handler. The first byte is
	// the Pascal length.
	Name []byte
}

// ReadCurrentFlatName extracts the name from a flat table row exactly as
// FUN_004125C0 copies it: seven DWORDs are read and the leading length byte
// bounds the text.
func ReadCurrentFlatName(row []byte) (string, error) {
	if len(row) < CurrentFlatCopyDwords*4 {
		return "", fmt.Errorf("flat table row is %d bytes, want at least %d", len(row), CurrentFlatCopyDwords*4)
	}
	length := int(row[0])
	if length+1 > len(row) {
		return "", fmt.Errorf("flat table row declares a %d byte name but holds %d bytes", length, len(row))
	}
	return string(row[1 : 1+length]), nil
}

// CurrentFlatName resolves the currentflat builtin. It returns the lowercase
// "none" fallback when no flat is active, matching verified FUN_004125C0, and
// otherwise the name taken from the active flat's table row.
func CurrentFlatName(active bool, table [][]byte, index int) (string, error) {
	if !active {
		return NameNone, nil
	}
	if index < 0 || index >= len(table) {
		return "", fmt.Errorf("currentflat index %d is outside a %d entry table", index, len(table))
	}
	name, err := ReadCurrentFlatName(table[index])
	if err != nil {
		return "", err
	}
	return name, nil
}

// CurrentSetName resolves the currentset builtin. Verified FUN_0041B2F0 returns
// the live name from a runtime slot when a set is active and the capitalised
// "None" fallback otherwise.
func CurrentSetName(active bool, liveName string) string {
	if active {
		return liveName
	}
	return NameNoneUpper
}

// PointInSet resolves the pointinset builtin. Verified FUN_0041B330 always
// produces a type-2 boolean result; it only performs the hit test when both
// runtime slots it requires are non-zero, and yields false when either is empty.
func PointInSet(hasSet, hasHitData bool, hit bool) ValueResult {
	if !hasSet || !hasHitData {
		return BoolResult(false)
	}
	return BoolResult(hit)
}

// EqualValueName compares two builtin-returned names the way the native handlers
// feed them into the string register pool, which is case-insensitive for
// ASCII letters and exact for every other byte. That is the same mapping
// verified for FUN_0042E5B0 and implemented by equalASCIIFold below.
func EqualValueName(left, right string) bool {
	return equalASCIIFold(left, right)
}

func equalASCIIFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := 0; i < len(left); i++ {
		a, b := lowerASCII(left[i]), lowerASCII(right[i])
		if a != b {
			return false
		}
	}
	return true
}

// VerifyValueBuiltinArgument is a small helper used by the ported handlers to
// run the shared type guard and produce the verified status on mismatch.
func VerifyValueBuiltinArgument(kind uint16) (ValueResult, uint16, error) {
	status, err := RequireNumericArgument(kind)
	if err != nil {
		return ValueResult{}, status, err
	}
	return ValueResult{Type: ValueTypeNumeric}, 0, nil
}

// NormalizeResultName trims a builtin-returned name for comparison without
// altering its stored form, since the two "none" spellings are distinct in the
// reference.
func NormalizeResultName(name string) string {
	return strings.TrimSpace(name)
}
