package scripts

import "fmt"

// Name tables: the two fixed-stride tables the engine searches by name.
//
// Both resolvers were decompiled and both have the same shape, differing only in
// their base offsets and row size:
//
//	FUN_00413590  flats    count at +0x834, rows at +0x838, 7 DWORDs per row
//	FUN_0041B7C0  scenes   count at +0x1C0C, rows at +0x1C10, 8 DWORDs per row
//
// In both the count is a DWORD immediately *before* the first row, the name is
// compared at row offset 12 using FUN_0042E5B0, the case-insensitive ASCII
// Pascal compare already implemented as equalASCIIFold, and a miss returns the
// same status 0x0A. The guards differ: the flat resolver needs an open stage and
// returns -1 for "no stage", while the scene resolver needs an active set and
// returns 0x28, the same status setscript's guard uses.
//
// The row name is a Pascal record at offset 12 of a row that is 28 or 32 bytes,
// so the name can be at most 16 bytes in a flat row and 20 in a scene row.

// StatusNameNotFound is native status 0x0A, returned by both name resolvers when
// no row matches. It is distinct from the 0x0B no-stage status, the 0x28 no-set
// status and the 0x34 malformed-stage-name status, so a caller can tell a missing
// entry from a missing context.
const StatusNameNotFound uint16 = 0x0A

// NameTable describes a table of fixed-stride rows inside a locked global, with
// the name to search for at a fixed offset inside each row.
type NameTable struct {
	// Kind names the table, for error messages.
	Kind string
	// CountOffset is where the row count is read, immediately before the rows.
	CountOffset int
	// RowsOffset is where the first row begins.
	RowsOffset int
	// RowSize is the stride between rows in bytes.
	RowSize int
	// NameOffset is where the Pascal name sits inside a row.
	NameOffset int
}

// Verified table geometry, from the two resolvers.
var (
	// FlatNameTable is the flat table of an open stage, behind FUN_00413590.
	FlatNameTable = NameTable{
		Kind:        "flat",
		CountOffset: 0x834,
		RowsOffset:  0x838,
		RowSize:     7 * 4, // 28
		NameOffset:  12,
	}
	// SceneNameTable is the scene table of an active set, behind FUN_0041B7C0.
	SceneNameTable = NameTable{
		Kind:        "scene",
		CountOffset: 0x1C0C,
		RowsOffset:  0x1C10,
		RowSize:     8 * 4, // 32
		NameOffset:  12,
	}
)

// CountSeparate marks a table whose row count lives in a different global rather
// than immediately before the rows, which is the case for the id-keyed stage
// object table in hittest.go. Such a table cannot be counted from its own buffer.
const CountSeparate = -1

// validate checks the table description is self-consistent, so a typo in a
// geometry constant produces a clear error rather than a slice panic.
func (t NameTable) validate() error {
	if t.RowsOffset < 0 || t.RowSize <= 0 {
		return fmt.Errorf("%s table geometry is not usable: rows %d size %d",
			t.Kind, t.RowsOffset, t.RowSize)
	}
	if t.NameOffset < 0 || t.NameOffset >= t.RowSize {
		return fmt.Errorf("%s table name offset %d is outside a %d byte row", t.Kind, t.NameOffset, t.RowSize)
	}
	if t.CountOffset == CountSeparate {
		// The count is supplied out of band, so there is nothing to check for
		// adjacency and Count must not be used on this table.
		return nil
	}
	if t.CountOffset < 0 {
		return fmt.Errorf("%s table count offset %d is neither a real offset nor CountSeparate",
			t.Kind, t.CountOffset)
	}
	// The reference reads the count as a DWORD sitting immediately before the
	// first row, so the two offsets must be adjacent. A gap would mean the count
	// is not the field the resolver reads.
	if t.RowsOffset < t.CountOffset+4 {
		return fmt.Errorf("%s table rows at %d do not follow the %d byte count at %d",
			t.Kind, t.RowsOffset, 4, t.CountOffset)
	}
	return nil
}

// Count returns the number of rows the table holds. A non-positive count is zero
// rows, matching the reference's `if (0 < count)` guard.
func (t NameTable) Count(data []byte) (int, error) {
	if err := t.validate(); err != nil {
		return 0, err
	}
	if t.CountOffset == CountSeparate {
		return 0, fmt.Errorf("%s table keeps its count out of band; pass it to LookupByID instead", t.Kind)
	}
	if t.CountOffset+4 > len(data) {
		return 0, fmt.Errorf("%s table count at +%#x is past the %d byte table", t.Kind, t.CountOffset, len(data))
	}
	// A signed read, because the resolver tests the count as a signed int and a
	// set high bit would otherwise be a huge row count.
	count := int(int32(uint32(data[t.CountOffset]) |
		uint32(data[t.CountOffset+1])<<8 |
		uint32(data[t.CountOffset+2])<<16 |
		uint32(data[t.CountOffset+3])<<24))
	if count <= 0 {
		return 0, nil
	}
	// Reject a count that could not possibly fit, rather than reading past the
	// buffer on the first row.
	if t.RowsOffset+count*t.RowSize > len(data) {
		return 0, fmt.Errorf("%s table claims %d rows of %d bytes from +%#x, past the %d byte table",
			t.Kind, count, t.RowSize, t.RowsOffset, len(data))
	}
	return count, nil
}

// Row returns row index of the table, or an error if it is out of range.
func (t NameTable) Row(data []byte, index int) ([]byte, error) {
	if err := t.validate(); err != nil {
		return nil, err
	}
	if index < 0 {
		return nil, fmt.Errorf("%s table row %d is negative", t.Kind, index)
	}
	start := t.RowsOffset + index*t.RowSize
	if start+t.RowSize > len(data) {
		return nil, fmt.Errorf("%s table row %d spans +%#x..%#x, past the %d byte table",
			t.Kind, index, start, start+t.RowSize, len(data))
	}
	return data[start : start+t.RowSize], nil
}

// RowName returns the Pascal name of row index.
func (t NameTable) RowName(data []byte, index int) (string, error) {
	row, err := t.Row(data, index)
	if err != nil {
		return "", err
	}
	return pascalTextOf(row[t.NameOffset:]), nil
}

// Lookup finds the row whose name matches, using the reference's
// FUN_0042E5B0 case-insensitive ASCII compare. It returns the row index and the
// whole row, so a caller that needs more than the name has it without a second
// lookup. Not found is reported through the returned status, matching the native
// 0x0A.
func (t NameTable) Lookup(data []byte, name string) (index int, row []byte, status uint16, err error) {
	count, err := t.Count(data)
	if err != nil {
		return 0, nil, 0, err
	}
	for i := 0; i < count; i++ {
		candidate, rowErr := t.Row(data, i)
		if rowErr != nil {
			return 0, nil, 0, rowErr
		}
		if equalASCIIFold(pascalTextOf(candidate[t.NameOffset:]), name) {
			return i, candidate, 0, nil
		}
	}
	return 0, nil, StatusNameNotFound, nil
}

// LookupCached reproduces the cached name lookup the engine uses for both the prop
// table and the actor table. Verified `FUN_004213C0` and `FUN_0040D730` have the
// same shape, differing only in their geometry:
//
//	if (cache < count && matches(row[cache], name)) return copy of row[cache];
//	for (i = 0; i < count; i++)
//	    if (matches(row[i], name)) { cache = i; return copy of row[i]; }
//	return 0x0A;
//
// So the cache is checked first and **re-validated by name**, a stale index is
// ignored rather than trusted, a scan hit replaces it, and a miss is the same
// 0x0A the flat and scene resolvers use. Both callers were transcribed separately
// before their shared shape was known; this is the single implementation, and
// `LookupProp` and `LookupActor` both delegate to it.
func LookupCached(table NameTable, records []byte, count StageObjectCount, name string, cache *PropCache) (index int, record []byte, status uint16, err error) {
	if err = table.validate(); err != nil {
		return 0, nil, 0, err
	}
	if count <= 0 {
		return 0, nil, StatusNameNotFound, nil
	}
	row := func(i int) ([]byte, error) { return table.Row(records, i) }

	// Step one: the cached index, if it is in range and still matches by name.
	if cache != nil && cache.Valid && cache.Index >= 0 && cache.Index < int(count) {
		candidate, rowErr := row(cache.Index)
		if rowErr != nil {
			return 0, nil, 0, rowErr
		}
		if equalASCIIFold(pascalTextOf(candidate[table.NameOffset:]), name) {
			return cache.Index, candidate, 0, nil
		}
	}

	// Step two: a linear scan, updating the cache on a hit.
	for i := 0; i < int(count); i++ {
		candidate, rowErr := row(i)
		if rowErr != nil {
			return 0, nil, 0, rowErr
		}
		if equalASCIIFold(pascalTextOf(candidate[table.NameOffset:]), name) {
			if cache != nil {
				cache.Index = i
				cache.Valid = true
			}
			return i, candidate, 0, nil
		}
	}
	return 0, nil, StatusNameNotFound, nil
}

// NameResolverGuard is the precondition a name resolver checks before it searches
// its table. Both resolvers have one, and they differ: the flat table belongs to
// an open stage while the scene table belongs to an active set.
type NameResolverGuard uint8

const (
	// GuardStageOpen is the flat resolver's precondition, FUN_00413590, which
	// returns -1 for "no stage" rather than a status.
	GuardStageOpen NameResolverGuard = iota
	// GuardSetActive is the scene resolver's precondition, FUN_0041B7C0, which
	// returns 0x28.
	GuardSetActive
)

// ResolveName applies the resolver's precondition and then searches its table.
// The stage precondition yields a not-found of -1 in the reference, which is a
// negative index rather than a status, so it is surfaced as a distinct result
// instead of being folded into 0x0A.
func (t NameTable) ResolveName(data []byte, name string, guard NameResolverGuard, ctx ScriptContext) (NameResolution, int, []byte, uint16, error) {
	switch guard {
	case GuardStageOpen:
		if !ctx.StageOpen {
			// Verified: FUN_00413590 returns -1 immediately, so nothing is
			// searched and the caller sees a missing index, not status 0x0A.
			return NameNoContext, -1, nil, 0, nil
		}
	case GuardSetActive:
		if !ctx.SetActive {
			return NameNoContext, 0, nil, StatusNoActiveSet, nil
		}
	}
	index, row, status, err := t.Lookup(data, name)
	switch {
	case err != nil:
		return NameUnsearchable, 0, nil, 0, err
	case status != 0:
		return NameMissing, -1, nil, StatusNameNotFound, nil
	}
	return NameFound, index, row, 0, nil
}

// NameResolution is the outcome of a guarded name lookup.
type NameResolution uint8

const (
	// NameFound means a row matched.
	NameFound NameResolution = iota
	// NameMissing means the precondition held but no row matched.
	NameMissing
	// NameNoContext means the precondition failed and nothing was searched.
	NameNoContext
	// NameUnsearchable means the table could not be read at all.
	NameUnsearchable
)
