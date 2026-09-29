package scripts

import "fmt"

// The actor-visible setter, getter and the row store they share. The getter was
// recorded earlier as the in-table value handler; this maps the other two bodies and
// the store, which completes the engine's **only** matched getter/setter pair.

// The setter, `FUN_0040B580`, which the command dispatcher reaches out of table for
// 16001. Verified in full:
//
//	status = FUN_00417070(frame, at, ctx, &value, &count, &consumed);  // TWO arguments
//	if (status) return status;
//	status = FUN_00421FC0(&value, &scratch);        // the first into a scratch name
//	if (status) return status;
//	status = FUN_0040D730(&scratch, actorBlock);    // the shared actor resolver
//	if (status) return status;
//	if (value[0] != 2) return 0x0E;                 // the SECOND must be a boolean
//	FUN_0040D800(actorBlock);                      // the store
//	return 0;
//
// So the setter takes **`actorName , boolean`**, and the boolean requirement is
// explicit rather than a coercion: a second operand of any other type is
// `0x0E`, the wrong-operand-type status.
const (
	// ActorVisibleSetterArity is how many operands the setter takes, two.
	ActorVisibleSetterArity = 2
	// ActorVisibleSetterOperandType is the type the second operand must have, 2, a
	// boolean. The reference tests it rather than converting, so any other type is
	// refused.
	ActorVisibleOperandType uint16 = 2
	// ActorVisibleGetterArity is how many operands the getter takes, one. The getter
	// is the read side of the pair and so addresses the actor by name alone.
	ActorVisibleGetterArity = 1
	// ActorVisibleResultType is the getter's result type, also 2.
	ActorVisibleResultType uint16 = 2
)

// ActorVisibleOperand is one of the setter's two operands.
//
// **It carries a type, not a flag.** The reference tests `value[0] != 2`, which is the
// operand's *type* field, so a caller must say what type its operand is rather than
// whether it is "a boolean" — because `false` is a perfectly good boolean and must be
// accepted. An earlier version of this type carried an `IsBoolean bool`, which could
// not distinguish a false boolean from a non-boolean, and its own test caught that
// immediately. The type is the field the reference reads.
type ActorVisibleOperand struct {
	// Name is the actor's name, which the setter resolves through the shared resolver.
	Name string
	// Type is the operand's value-record type. The second operand must be
	// ActorVisibleOperandType; the first is not checked.
	Type uint16
	// Value is the operand's value, which the reference's store does not inspect — it
	// is carried so a caller can build a complete operand.
	Value uint32
}

// BooleanOperand returns a type-2 operand, which is what the setter's second operand
// must be. The value is passed through rather than encoded as a flag, so a `false`
// boolean is a type-2 operand with value 0 and is accepted.
func BooleanOperand(value bool) ActorVisibleOperand {
	v := uint32(0)
	if value {
		v = 1
	}
	return ActorVisibleOperand{Type: ActorVisibleOperandType, Value: v}
}

// NameOperand returns a name operand of the given type, for the first position.
func NameOperand(name string, typ uint16) ActorVisibleOperand {
	return ActorVisibleOperand{Name: name, Type: typ}
}

// IsBoolean reports whether the operand is of the boolean type, which is a test of the
// type and not of the value.
func (o ActorVisibleOperand) IsBoolean() bool {
	return o.Type == ActorVisibleOperandType
}

// ValidateActorVisibleOperands applies the setter's check to its two operands, which is
// the only validation between the parse and the store.
//
// The test is the reference's own and is exact: the second operand's type must be `2`,
// so a numeric, a string or a name is `0x0E` rather than being coerced. **A `false`
// boolean passes**, because the reference tests the type and not the value. The first
// operand has no type requirement at all — it is always a name to be resolved.
func ValidateActorVisibleOperands(first, second ActorVisibleOperand) (status uint16, err error) {
	if len(first.Name) == 0 {
		return StatusNameNotFound, fmt.Errorf(
			"the setter's first operand has no name to resolve (status %#x)", StatusNameNotFound)
	}
	if second.IsBoolean() {
		return 0, nil
	}
	return StatusWrongOperandType, fmt.Errorf(
		"the setter's second operand must be a type %d boolean, and it is type %d (status %#x)",
		ActorVisibleOperandType, second.Type, StatusWrongOperandType)
}

// The store, `FUN_0040D800`, verified in full:
//
//	block = GlobalLock(DAT_004599D8);                       // the actor table
//	if (DAT_004599DC <= DAT_00459EF0) FUN_0042C470(0, 0x0FA9);
//	row = block + DAT_00459EF0 * 0xA4;
//	if (row[0x0C] != param_1[0x0C]) FUN_0042C470(0, 0x0FAA);
//	for (i = 0x29; i != 0; i--) { *row++ = *param_1++; }    // 41 DWORDs
//	GlobalUnlock(DAT_004599D8);
//
// **Three things here are worth stating, and one of them corrects an expectation.**
//
// The store writes the **entire row** — 41 DWORDs, which is 164 bytes — rather than
// the single field the getter reads. So the pair is **asymmetric in what it touches**:
// `actorvisible` with no value reads the row's first DWORD, and `actorvisible` with a
// value overwrites all forty-one. A port that wrote only the first DWORD on the set
// path would leave the other forty stale, and nothing in the reference would complain.
//
// The version field is **checked but not bumped**: the test is `row[0x0C] != src[0x0C]`,
// so the caller must already have stamped a matching version into its copy. There is
// no write of a fresh version here, so the compare is a guard against a stale source
// rather than a compare-and-swap.
//
// And the geometry is a **third independent confirmation** of the actor table's stride
// and version offset: the stride is `0xA4` = 164 and the version sits at DWORD index
// `0x0C` = byte `0x30`, both of which match what `actors.go` and the write-back path
// already recorded.
const (
	// ActorRowStride is the actor table's row stride, 0xA4 = 164 bytes.
	ActorRowStride = 0xA4
	// ActorRowDwords is a row's size in DWORDs, 41, which is what the copy loop runs.
	ActorRowDwords = 0x29
	// ActorRowBytes is a row's size in bytes, 164.
	ActorRowBytes = ActorRowStride
	// ActorVersionDwordIndex is the version field's index within a row, 0x0C, which is
	// byte offset 0x30.
	ActorVersionDwordIndex = 0x0C
	// ActorStoreIndexDiag is raised when the row index is not below the count, 0x0FA9.
	ActorStoreIndexDiag uint16 = 0x0FA9
	// ActorStoreVersionDiag is raised when the source's version does not match the
	// row's, 0x0FAA.
	ActorStoreVersionDiag uint16 = 0x0FAA
)

// ActorRow is one actor's row, as the store sees it.
type ActorRow struct {
	// Visible is the first DWORD, which is the field the getter reports.
	Visible uint32
	// Version is the field at DWORD index 0x0C, byte offset 0x30.
	Version uint32
	// Tail is the remaining 39 DWORDs, which the store copies but the getter never
	// reads.
	Tail [ActorRowDwords - 2]uint32
}

// VisibleFlag extracts the boolean the getter reports: the row's first DWORD, returned
// as a type-2 result. The reference copies the DWORD whole, so the flag is whatever
// low bit the row holds rather than a normalised boolean.
func (r ActorRow) VisibleFlag() (uint16, uint32) {
	return ActorVisibleResultType, r.Visible
}

// StoreOutcome is what a row store did.
type StoreOutcome struct {
	// Written is how many DWORDs were copied, always ActorRowDwords.
	Written int
	// TableUnlocked records that the table was unlocked, which the reference does
	// unconditionally once the copy finishes.
	TableUnlocked bool
}

// StoreActorRow reproduces FUN_0040D800: it checks the index against the count, checks
// the version against the source's, and copies all 41 DWORDs.
//
// The two checks are the reference's diagnostics rather than Go errors, so a caller
// that ignores the status still sees the copy skipped — which is how a stale write is
// caught in the reference.
func StoreActorRow(table []ActorRow, index int, source ActorRow, count int) (StoreOutcome, uint16, error) {
	outcome := StoreOutcome{Written: ActorRowDwords}
	// The index test is `count <= index`, so the count itself is out of range.
	if index < 0 || count <= index {
		return outcome, ActorStoreIndexDiag, fmt.Errorf(
			"row %d is not below the count %d (FUN_0042C470(0, %#x))",
			index, count, ActorStoreIndexDiag)
	}
	if index >= len(table) {
		return outcome, ActorStoreIndexDiag, fmt.Errorf(
			"row %d is past the %d row table (FUN_0042C470(0, %#x))",
			index, len(table), ActorStoreIndexDiag)
	}
	// The version test is `row[0x0C] != src[0x0C]`, and it is a **check, not a bump**:
	// the caller must already have stamped a matching version into its copy.
	if table[index].Version != source.Version {
		return outcome, ActorStoreVersionDiag, fmt.Errorf(
			"row %d holds version %#x and the source carries %#x (FUN_0042C470(0, %#x))",
			index, table[index].Version, source.Version, ActorStoreVersionDiag)
	}
	table[index] = source
	// The table is unlocked unconditionally once the copy finishes.
	outcome.TableUnlocked = true
	return outcome, 0, nil
}

// VerifyActorRowGeometry checks the store's three numbers agree with each other and
// with what the rest of the conversion already recorded, since they are derived in
// three separate places and a disagreement would mean one of them is wrong.
func VerifyActorRowGeometry() (stride, versionOffset int, ok bool) {
	stride = ActorRowStride
	versionOffset = ActorVersionDwordIndex * 4
	// The copy loop must fill the row exactly, or it would write past it or stop short.
	ok = ActorRowDwords*4 == stride &&
		ActorVersionDwordIndex < ActorRowDwords &&
		ActorVersionOffset == versionOffset &&
		ActorVisibleResolver != "" &&
		ActorVisibleStore != ""
	return stride, versionOffset, ok
}
