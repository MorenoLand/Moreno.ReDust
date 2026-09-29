package scripts

import "fmt"

// The actor table and the random generator. Both were reached from the two
// out-of-table special cases, and both turned out to be fully specified.

// The actor table, from `FUN_0040D730`, the resolver both sides of `actorvisible`
// call. It is the **same table** the id-keyed lookup in `hittest` walks: same
// global, same count global, same stride.
//
//	entry = GlobalLock(DAT_004599D8);
//	if (DAT_00459EF0 < DAT_004599DC && matches(entry[cached] + 0x15, name)) {
//	    copy 41 DWORDs; return 0;                    // the whole record
//	}
//	for (i = 0; i < DAT_00459DC; i++)
//	    if (matches(entry[i] + 0x15, name)) {
//	        DAT_00459EF0 = i;                       // one-entry cache, as for props
//	        copy 41 DWORDs; return 0;
//	    }
//	return 0x0A;
//
// So records are `0x29` = 41 DWORDs = **164 bytes**, which is the same stride as the
// stage object table already transcribed. The record holds **two** name fields:
// a short lookup key at byte `0x15` = 21, which is what the resolver matches, and a
// longer name at byte `0x54` = 84, which is what the id-keyed lookup copies out.
// The earlier entry recorded only `0x54` and called it the name; it is the *output*
// name, while `0x15` is the one searches match against.
const (
	// ActorRecordSize is the record stride, 0x29 DWORDs.
	ActorRecordSize = 0x29 * 4
	// ActorRecordDwords is that count in DWORDs.
	ActorRecordDwords = 0x29
	// ActorLookupNameOffset is the short name the resolver matches, byte 21.
	ActorLookupNameOffset = 0x15
	// ActorOutputNameOffset is the longer name the id-keyed lookup copies, byte 84.
	ActorOutputNameOffset = 0x54
	// ActorVersionOffset is the DWORD at byte 48, the field the write-back guard
	// checks. See the note on ActorVersion below.
	ActorVersionOffset = 0x0C * 4
	// ActorTableCountSlot is the row count, DAT_004599DC.
	ActorTableCountSlot = "DAT_004599DC"
	// ActorTableCacheSlot is the one-entry cache index, DAT_00459EF0.
	ActorTableCacheSlot = "DAT_00459EF0"
	// ActorWriteRangeDiag and ActorWriteVersionDiag are the two diagnostics the
	// write-back guard raises.
	ActorWriteRangeDiag    uint16 = 0x0FA9
	ActorWriteVersionDiag uint16 = 0x0FAA
)

// ActorVersion explains what the field at byte 48 is, because the earlier reading
// of it was wrong.
//
// `FUN_0040E0C0` matches it, which reads like an identifier: "find the record whose
// field at 0x30 equals this value". But `FUN_0040D800`, the write-back, **checks
// that the same field has not changed** and raises a diagnostic if it has. A pure
// identifier could not fail that check, since the caller would only ever write back
// to the record it just read. A field that is *expected* to change is a **version
// or generation stamp**, and the check is a compare-and-swap: resolve, let the
// caller modify a copy, then write back only if nothing else touched the record in
// between.
//
// So the field serves both roles, and both readings are correct about what the code
// does with it. `ActorVersion` exposes it as a version because that is what the
// write-back guard requires, and the id-keyed lookup is documented as matching on
// it. Conflating the two would make the guard look pointless.
const ActorVersionFieldName = "version or generation stamp"

// ReadActorVersion reads the guarded field from a record.
func ReadActorVersion(record []byte) (int32, error) {
	if ActorVersionOffset+4 > len(record) {
		return 0, fmt.Errorf("the actor version at +%#x is past the %d byte record", ActorVersionOffset, len(record))
	}
	return int32(readUint32(record[ActorVersionOffset:])), nil
}

// ActorTable is the actor table's geometry, using the shared NameTable type. Its
// lookup name is the short key at byte 21, and the count is out of band.
var ActorTable = NameTable{
	Kind:        "actor",
	CountOffset: CountSeparate,
	RowsOffset:  0,
	RowSize:     ActorRecordSize,
	NameOffset:  ActorLookupNameOffset,
}

// ActorTableMatchesTheObjectTable pins the relationship: the actor resolver and the
// id-keyed object lookup walk the same rows, so their strides must agree, and
// neither name offset may be confused with the other.
func ActorTableMatchesTheObjectTable() bool {
	return ActorTable.RowSize == StageObjectTable.RowSize &&
		ActorTable.NameOffset != StageObjectTable.NameOffset &&
		ActorTable.NameOffset == ActorLookupNameOffset &&
		StageObjectTable.NameOffset == ActorOutputNameOffset
}

// ActorNames reads both name fields from a record, which is the quickest way to see
// that they are distinct regions rather than one field with two uses.
func ActorNames(record []byte) (lookup string, output string, err error) {
	if ActorOutputNameOffset >= len(record) {
		return "", "", fmt.Errorf("the actor record is %d bytes, too short for the name at +%#x",
			len(record), ActorOutputNameOffset)
	}
	if ActorLookupNameOffset >= ActorOutputNameOffset {
		return "", "", fmt.Errorf("the lookup name at +%#x must precede the output name at +%#x",
			ActorLookupNameOffset, ActorOutputNameOffset)
	}
	return pascalTextOf(record[ActorLookupNameOffset:]),
		pascalTextOf(record[ActorOutputNameOffset:]), nil
}

// WriteActorState reproduces `FUN_0040D800`, the guarded write-back. It returns
// false with a diagnostic when the guard fails rather than raising, since in Go a
// native diagnostic is a returned condition.
//
// The guard has two halves, in the reference's order: the cached index must be
// inside the table, and the record's version field must be unchanged since the
// resolve. Either failing means the write is refused.
func WriteActorState(records []byte, count StageObjectCount, cachedIndex int, resolvedVersion int32) (uint16, error) {
	if count <= 0 || cachedIndex < 0 || int(cachedIndex) >= int(count) {
		return ActorWriteRangeDiag, fmt.Errorf("the cached actor index %d is outside a table of %d (FUN_0042C470(0, %#x))",
			cachedIndex, count, ActorWriteRangeDiag)
	}
	start := cachedIndex * ActorRecordSize
	if start+ActorRecordSize > len(records) {
		return ActorWriteRangeDiag, fmt.Errorf("actor row %d spans +%d..%d, past the %d byte table",
			cachedIndex, start, start+ActorRecordSize, len(records))
	}
	current, err := ReadActorVersion(records[start : start+ActorRecordSize])
	if err != nil {
		return ActorWriteRangeDiag, err
	}
	if current != resolvedVersion {
		return ActorWriteVersionDiag, fmt.Errorf(
			"actor %d has version %d but %d was resolved, so the write is refused (FUN_0042C470(0, %#x))",
			cachedIndex, current, resolvedVersion, ActorWriteVersionDiag)
	}
	return 0, nil
}

// CopyActorRecord copies a resolved record out, which is the 41-DWORD copy the
// resolver performs on a hit. The whole record is copied, so the version field
// travels with it and the write-back guard can compare against it.
func CopyActorRecord(records []byte, index int) ([]byte, error) {
	if index < 0 {
		return nil, fmt.Errorf("actor index %d is negative", index)
	}
	start := index * ActorRecordSize
	if start+ActorRecordSize > len(records) {
		return nil, fmt.Errorf("actor row %d spans +%d..%d, past the %d byte table",
			index, start, start+ActorRecordSize, len(records))
	}
	out := make([]byte, ActorRecordSize)
	copy(out, records[start:start+ActorRecordSize])
	return out, nil
}

// The random generator, `FUN_0042E7A0`, which is short enough to convert exactly:
//
//	raw = FUN_0042E740() & 0x7FFF;
//	if (raw == 0x7FFF) raw = 0x7FFE;
//	return (raw * bound) / 0x7FFF + 1;
//
// So **`random(bound)` returns a value from 1 to bound inclusive**, not 0 to
// bound-1. The `+ 1` is the load-bearing part: a port written as `raw % bound` or
// `raw * bound / 0x7FFF` would return 0 sometimes, and a script using the result as
// an index would then be off by one at zero. The 0x7FFF clamp exists because a raw
// value of exactly 0x7FFF would scale to `bound` and the `+ 1` would push it to
// `bound + 1`, one past the end.
const (
	// RandomScaleMask is the 0x7FFF the raw generator is masked with.
	RandomScaleMask uint32 = 0x7FFF
	// RandomScaleTop is the value the raw draw is clamped away from.
	RandomScaleTop uint32 = 0x7FFF
	// RandomRawSource is the underlying generator, FUN_0042E740, which is unmapped.
	// The scaling above it is fully specified, so only the raw sequence is open.
	RandomRawSource = "FUN_0042E740"
)

// ScaleRandom reproduces the scaling half of FUN_0042E7A0 exactly, given a raw draw.
// The result is in 1..bound for any bound of 1 or more.
func ScaleRandom(raw uint32, bound int32) int32 {
	scaled := raw & RandomScaleMask
	if scaled == RandomScaleTop {
		scaled = RandomScaleTop - 1
	}
	return int32((scaled * uint32(bound)) / RandomScaleMask) + 1
}

// RandomInRange resolves `random` given a raw draw, which is what the script
// builtin amounts to once the generator is injected.
func RandomInRange(raw uint32, bound int32) (ValueResult, uint16, error) {
	if bound < 1 {
		// The reference does not check, so a bound of zero would divide by the
		// scaled value and yield 1. Reporting it is safer than reproducing that.
		return ValueResult{}, 0, fmt.Errorf("random bound %d must be 1 or more", bound)
	}
	return NumericResult(ScaleRandom(raw, bound)), 0, nil
}
