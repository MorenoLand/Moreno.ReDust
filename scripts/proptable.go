package scripts

import "fmt"

// The prop table and the prop accessors, from live Ghidra decompilation of
// FUN_004213C0 (fetch a prop by name) and the four boot-reachable accessors that
// call it: propvisible 16015 FUN_0041F9B0, propview 16025 FUN_0041F230, propxy
// 16018 FUN_0041FEE0, and setvisible 16007 FUN_0041AF90.

// Prop record geometry, verified from FUN_004213C0. This is the fourth table in
// the engine, and the largest stride of the four:
//
//	stride        0x9E   (158 bytes)
//	name          +0x4E  (a Pascal record, compared with FUN_0042E5B0)
//	copied out    0x27 DWORDs (156 bytes) plus one trailing word, that is the
//	                      whole 158-byte record
//	count         DAT_004599EC, a separate global
//
// So a prop record is 158 bytes with its name at byte 78, leaving 78 bytes of
// header and 80 bytes after the name.
const (
	// PropRecordSize is the verified stride, 158 bytes.
	PropRecordSize = 0x9E
	// PropNameOffset is where a record's Pascal name sits, byte 78.
	PropNameOffset = 0x4E
	// PropRecordCopyDwords is the 0x27 DWORDs copied out on a match.
	PropRecordCopyDwords = 0x27
	// PropRecordTailBytes is the size of the record's tail that propview returns
	// as a string. Ghidra reserves 34 bytes for it, which together with the
	// record's own size fixes it as the final 34 bytes of the record.
	PropRecordTailBytes = 34
	// PropRecordTailOffset is where that tail begins within a record.
	PropRecordTailOffset = PropRecordSize - PropRecordTailBytes
)

// PropNameTable is the id-keyed and name-keyed prop table. Unlike the flat and
// scene tables its count lives in a separate global, so it uses CountSeparate.
var PropNameTable = NameTable{
	Kind:        "prop",
	CountOffset: CountSeparate,
	RowsOffset:  0,
	RowSize:     PropRecordSize,
	NameOffset:  PropNameOffset,
}

// PropCache is the one-entry name lookup cache the reference keeps.
//
// Verified FUN_004213C0 checks a cached index before scanning:
//
//	if (DAT_004596FC < DAT_004599EC && match(row[cachedIndex], name)) return copy;
//	for (i = 0; i < count; i++)
//	    if (match(row[i], name)) { DAT_004596FC = i; return copy; }
//
// So the cache is checked first on every lookup and updated only on a scan hit.
// A cached index that is out of range, or whose name no longer matches, falls
// through to the scan. This is observable behaviour, not just speed: a stale
// cache entry is re-validated by name rather than trusted blindly.
type PropCache struct {
	// Index is the cached row index, DAT_004596FC.
	Index int
	// Valid reports whether the cache holds an index at all. The reference uses
	// the count as the bound, so a negative index is never cached.
	Valid bool
}

// Reset clears the cache, which is what a stage change needs since the prop table
// is rebuilt.
func (c *PropCache) Reset() {
	if c == nil {
		return
	}
	c.Index = 0
	c.Valid = false
}

// LookupProp finds a prop record by name, reproducing FUN_004213C0 including the
// one-entry cache. The cache is consulted first when its index is in range and
// its name still matches; a scan hit updates it. A miss is status 0x0A, the same
// not-found status the flat and scene resolvers use.
func LookupProp(table []byte, count StageObjectCount, name string, cache *PropCache) (index int, record []byte, status uint16, err error) {
	if err = PropNameTable.validate(); err != nil {
		return 0, nil, 0, err
	}
	if count <= 0 {
		return 0, nil, StatusNameNotFound, nil
	}
	row := func(i int) ([]byte, error) { return PropNameTable.Row(table, i) }

	// Step one: the cached index, if it is in range and still matches.
	if cache != nil && cache.Valid && cache.Index >= 0 && cache.Index < int(count) {
		candidate, rowErr := row(cache.Index)
		if rowErr != nil {
			return 0, nil, 0, rowErr
		}
		if equalASCIIFold(pascalTextOf(candidate[PropNameOffset:]), name) {
			return cache.Index, candidate, 0, nil
		}
	}

	// Step two: a linear scan, updating the cache on a hit.
	for i := 0; i < int(count); i++ {
		candidate, rowErr := row(i)
		if rowErr != nil {
			return 0, nil, 0, rowErr
		}
		if equalASCIIFold(pascalTextOf(candidate[PropNameOffset:]), name) {
			if cache != nil {
				cache.Index = i
				cache.Valid = true
			}
			return i, candidate, 0, nil
		}
	}
	return 0, nil, StatusNameNotFound, nil
}

// propxy's sub-field selector, verified from FUN_0041FEE0. propxy 16018 takes a
// prop name and a numeric selector, and the selector chooses which part of a
// packed 32-bit value to read:
//
//	selector 1 -> the 16-bit field at byte offset 2, sign-extended
//	selector 2 -> the 16-bit field at byte offset 0, sign-extended
//	selector 3 -> the whole 32-bit value, with no extension at all
//	anything else -> status 0x0E
//
// So the two narrow selectors read the **high and low halves** of a 32-bit value,
// not a byte and a half. Ghidra renders the high field as `local_8c._2_2_`,
// where the two indices are offset 2 and size 2, so it is a 16-bit field and not
// a byte. The reference settles it independently: each narrow case returns
// `(ushort)(field >> 0xf)`, a fifteen-bit shift, which is sign extraction for a
// sixteen-bit value. Sign-extending selector 3 would be wrong, since the
// reference copies the DWORD untouched.
const (
	// PropxySelectorHigh is 1, the signed 16-bit field at byte offset 2.
	PropxySelectorHigh = 1
	// PropxySelectorLow is 2, the signed 16-bit field at byte offset 0.
	PropxySelectorLow = 2
	// PropxySelectorDword is 3, the full 32-bit value, unextended.
	PropxySelectorDword = 3
	// PropxyHighFieldOffset is the byte offset of the selector-1 field.
	PropxyHighFieldOffset = 2
	// PropxyFieldBits is the width of both narrow selectors, from the `>> 0xf`
	// sign extraction in the reference.
	PropxyFieldBits = 16
)

// Propxy extracts the selected part of a prop's packed 32-bit value, which the
// reference holds as a single value read into local_8c.
func Propxy(packed uint32, selector int32) (int32, uint16, error) {
	switch selector {
	case PropxySelectorHigh:
		// The 16-bit field at offset 2, that is the high half, sign-extended.
		return int32(int16(uint16(packed >> 16))), 0, nil
	case PropxySelectorLow:
		// The 16-bit field at offset 0, that is the low half, sign-extended.
		return int32(int16(uint16(packed))), 0, nil
	case PropxySelectorDword:
		// The whole value. The reference copies the DWORD and clears only the
		// status word, so there is no extension of any kind.
		return int32(packed), 0, nil
	default:
		return 0, StatusWrongOperandType, fmt.Errorf("propxy selector %d is not 1, 2 or 3", selector)
	}
}

// PropVisible reproduces propvisible 16015, FUN_0041F9B0, which is startlingly
// simple once the guard chain is stripped:
//
//	evaluate name; string register; fetch prop;
//	*out = 2;                                  // type 2, boolean
//	*(int *)(out + 1) = (int)prop[0];          // the first 16-bit field, raw
//	out[3] = 0;
//
// The field is copied into a type-2 result **without any comparison or
// normalisation**. It is not "is it nonzero" and not "is it exactly 1"; the raw
// field value is the answer. A prop whose field holds 7 yields a boolean 7. The
// field is presumably already 0 or 1 in practice, so masking here would be
// harmless on the shipped data, but it would not be a faithful port.
func PropVisible(record []byte) (ValueResult, error) {
	if len(record) < 2 {
		return ValueResult{}, fmt.Errorf("prop record is %d bytes, too short for the visible field", len(record))
	}
	field := int32(int16(uint16(record[0]) | uint16(record[1])<<8))
	return ValueResult{Type: ValueTypeBool, Value: field, Tail: 0}, nil
}

// PropViewText reproduces propview 16025, FUN_0041F230, which fetches the prop
// and returns the tail of its record as a string register:
//
//	evaluate name; string register; fetch prop;
//	FUN_00421F60(&record[PropRecordTailOffset], out);
//
// Ghidra reserves 34 bytes for the source, which with the 158-byte record size
// fixes it as the record's final 34 bytes. The name sits at byte 78, so this
// tail is a distinct region from it.
func PropViewText(record []byte) ([]byte, error) {
	if len(record) < PropRecordTailOffset {
		return nil, fmt.Errorf("prop record is %d bytes, too short for a tail at +%#x",
			len(record), PropRecordTailOffset)
	}
	return PascalCopy(record[PropRecordTailOffset:]), nil
}

// SetVisible reproduces setvisible 16007, FUN_0041AF90, in full:
//
//	*out_status = 0;
//	*(u32 *)(out + 1) = 0;
//	if (DAT_00459A24 != 0) *(int *)(out + 1) = (int)DAT_00459A26;
//	out[3] = 0;
//	*out = 2;
//
// So it is always status 0 and always a type-2 result, holding DAT_00459A26 when
// a set is active and 0 when none is.
//
// The reason this matters is the relationship it has to pointinset. Verified
// FUN_0041B330, the pointinset handler, runs its box test only when
// `DAT_00459A24 != 0 && DAT_00459A26 != 0`, and otherwise returns a type-2
// result of 0. So **setvisible is the script-visible accessor for exactly the
// second of the two flags pointinset gates on**: a script that reads setvisible
// and gets false knows in advance that pointinset will short-circuit to false
// without testing anything. The two builtins are a matched pair, and that is why
// both appear in the boot script.
const (
	// SetSlotActive is the slot whose being zero means no set, DAT_00459A24.
	SetSlotActive = "DAT_00459A24"
	// SetSlotVisible is the slot setvisible reports, DAT_00459A26.
	SetSlotVisible = "DAT_00459A26"
)

// SetState is the two-slot set state the builtins read.
type SetState struct {
	// Active is DAT_00459A24, non-zero when a set is open.
	Active bool
	// Visible is DAT_00459A26, the flag setvisible reports and pointinset gates on.
	Visible bool
}

// SetVisibleValue resolves the setvisible builtin from the two slots. It always
// succeeds, because the reference has no failure path: no active set is a false
// result, not an error.
func SetVisibleValue(state SetState) ValueResult {
	if state.Active {
		return BoolResult(state.Visible)
	}
	return BoolResult(false)
}

// PointInSetWillTest reports whether the pointinset builtin will perform a real
// hit test given the same two slots, which is exactly the condition its handler
// tests. A script can therefore use setvisible to predict pointinset rather than
// discovering it by getting a false it cannot interpret.
func PointInSetWillTest(state SetState) bool {
	return state.Active && state.Visible
}
