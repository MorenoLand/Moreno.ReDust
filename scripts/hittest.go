package scripts

import "fmt"

// The coordinate and hit-test primitives behind the boot-critical value
// builtins: `mouse` 20006, `hittest` 20070, `pointx` 20002, `pointy` 20003,
// `pointinset` 20041 and `pointinprop` 20040. All four functions below are
// decompiled from DF386.EXE and each is small enough to convert exactly.

// Point is the engine's 2D point, a packed dword of two signed 16-bit
// coordinates. The high word is x and the low word is y: pointx (FUN_00415CB0)
// reads the high word as x and pointy (FUN_00415D10) the low word as y, each
// sign-extended, and FUN_0042E340 splits the point the same way.
type Point struct {
	X int16
	Y int16
}

// Rect is the engine's rectangle, four signed 16-bit edges. FUN_0042E340 takes
// one as a pointer to four consecutive shorts, rect[0..3], and the verified order
// is Top, Left, Bottom, Right: rect[0] is the top, rect[1] the left, rect[2] the
// bottom and rect[3] the right. The Go fields are named by meaning, so their
// declaration order does not matter.
type Rect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

// ContainsPoint reproduces FUN_0042E340, the hit test the pointinset and
// pointinprop builtins resolve to. It is a half-open axis-aligned box test, in
// the native's order of checks:
//
//	if (point.x < rect.left)    return false    // high word against rect[1]
//	if (rect.right <= point.x)  return false    // high word against rect[3]
//	if (point.y < rect.top)     return false    // low word against rect[0]
//	return point.y < rect.bottom                // low word against rect[2]
//
// The bounds are deliberately asymmetric: the left and top edges are inclusive
// and the right and bottom edges are exclusive. The point exactly on the right or
// bottom edge is therefore outside, which is what makes adjacent rectangles
// non-overlapping. Rewriting this as an inclusive test would make two props
// sharing an edge both claim a click.
func ContainsPoint(point Point, rect Rect) bool {
	if point.X < rect.Left {
		return false
	}
	if rect.Right <= point.X {
		return false
	}
	if point.Y < rect.Top {
		return false
	}
	return point.Y < rect.Bottom
}

// The view state offsets read by FUN_0042C820, the function behind the `mouse`
// builtin. It calls GetCursorPos, converts the result to client coordinates for
// the view's window, and then adds the view's own origin:
//
//	ScreenToClient(window, &pt);
//	out.y = (short)view[0x16] + pt.y;      // view[0x16] is byte offset 0x58
//	out.x = (short)view[0x17] + pt.x;      // view[0x17] is byte offset 0x5C
//
// Ghidra renders the DWORD array indexes, so view[0x16] is byte offset 0x16*4 =
// 0x58 and view[0x17] is 0x5C. The mouse position the scripts see is therefore
// in view space, not screen space and not client space, and getting that wrong
// shifts every click by the scroll offset.
const (
	// ViewOriginXOffset and ViewOriginYOffset are the byte offsets of the view's
	// origin within its state block, read as signed 16-bit values.
	ViewOriginXOffset = 0x16 * 4
	ViewOriginYOffset = 0x17 * 4
	// ViewContextPointerOffset is where a caller-supplied context keeps its own
	// view pointer, overriding the global. FUN_0042C820 reads `*(u32 **)(context
	// + 0x18)`.
	ViewContextPointerOffset = 0x18
	// MouseNoViewDiag is the second argument of the FUN_0042C470(0x6E, 0x198)
	// raised when there is no view to ask, which is a diagnostic rather than a
	// recoverable status.
	MouseNoViewDiag uint16 = 0x198
)

// MouseView is the view state the mouse position is expressed in. Only the
// origin is needed to convert a client point into view space, so that is what is
// carried; the native reads it from a larger block at the offsets above.
type MouseView struct {
	// OriginX and OriginY are the signed 16-bit values read from
	// ViewOriginXOffset and ViewOriginYOffset.
	OriginX int16
	OriginY int16
}

// ViewOriginOffsetsAreAdjacent asserts the two origins are read from consecutive
// fields, which is what makes the native's two indexed reads a single logical
// pair. It is a compile-time-shaped fact worth pinning rather than assuming.
func (v MouseView) OriginAt(offset int) (int16, bool) {
	switch offset {
	case ViewOriginXOffset:
		return v.OriginX, true
	case ViewOriginYOffset:
		return v.OriginY, true
	}
	return 0, false
}

// ClientToView converts a client-space cursor position into the view space the
// scripts use, reproducing the two additions FUN_0042C820 performs after
// ScreenToClient. Both additions are on signed 16-bit values, so the result
// wraps the same way the native's shorts do.
func ClientToView(view MouseView, client Point) Point {
	return Point{
		X: int16(uint16(client.X) + uint16(view.OriginX)),
		Y: int16(uint16(client.Y) + uint16(view.OriginY)),
	}
}

// ViewToClient is the inverse of ClientToView, needed to turn a script's view
// point back into the client coordinates ScreenToClient's result is expressed in.
func ViewToClient(view MouseView, viewPoint Point) Point {
	return Point{
		X: int16(uint16(viewPoint.X) - uint16(view.OriginX)),
		Y: int16(uint16(viewPoint.Y) - uint16(view.OriginY)),
	}
}

// PointerStateSnapshotWords is the number of DWORDs FUN_00406F70 copies, and
// PointerStateSnapshotHeaderWords the two it copies separately.
//
//	FUN_00406F70(&DAT_00442350)  -- 0x19 = 25 DWORDs
//	FUN_00406F70(_DAT_00442340)  -- 2 DWORDs
//
// The function is the "remember where the pointer is" step the hit-test builtins
// take before walking the world, and the two regions are adjacent, so together
// they are a twenty-seven DWORD state block. The count is verified rather than
// assumed: getting it short would silently capture a partial state.
const (
	PointerStateSnapshotWords       = 0x19
	PointerStateSnapshotHeaderWords  = 2
	PointerStateSnapshotTotalWords   = PointerStateSnapshotWords + PointerStateSnapshotHeaderWords
	PointerStateSnapshotTotalByteLen = PointerStateSnapshotTotalWords * 4
)

// PointerState is the snapshot FUN_00406F70 produces: two header DWORDs followed
// by twenty-five body DWORDs. The field names are generic because the reference
// gives no meaning to the individual words; what matters is that all twenty-seven
// are captured and restored together.
type PointerState struct {
	// Header holds the two DWORDs read from DAT_00442340.
	Header [PointerStateSnapshotHeaderWords]uint32
	// Body holds the twenty-five DWORDs read from DAT_00442350.
	Body [PointerStateSnapshotWords]uint32
}

// PointerStateSize is the snapshot's byte size, 108.
func PointerStateSize() int { return PointerStateSnapshotTotalByteLen }

// ValidatePointerState checks a snapshot captured from a buffer is the right
// size, so a short buffer is refused rather than half-captured.
func ValidatePointerState(data []byte) error {
	if len(data) < PointerStateSize() {
		return fmt.Errorf("pointer state source is %d bytes, want the verified %d",
			len(data), PointerStateSize())
	}
	return nil
}

// StageObjectTable describes the id-keyed table FUN_0040E0C0 walks, which is a
// third table with different geometry from the two name tables. Unlike those, it
// is matched on a numeric id rather than a name:
//
//	count in DAT_004599DC, rows in DAT_004599D8
//	if (*(int *)(row + 0x30) == id) FUN_0042E6C0(row + 0x54, out)
//	row += 0xA4
//	not found -> 0x0A
//
// So the stride is 0xA4, 164 bytes, the id is a 32-bit field at offset 0x30 and
// the name a Pascal record at offset 0x54.
var StageObjectTable = NameTable{
	Kind:        "stage object",
	CountOffset: -1, // the count lives in a separate global, not before the rows
	RowsOffset:  0,
	RowSize:     0xA4,
	NameOffset:  0x54,
}

// StageObjectIDOffset is where a row's numeric id sits, 0x30.
const StageObjectIDOffset = 0x30

// StageObjectCount is the number of rows FUN_0040E0C0 iterates, read from
// DAT_004599DC. The loop is `for (i = 0; i < count; i++)` with a
// `if (0 < count)` guard, so a non-positive count means no rows.
type StageObjectCount int

// LookupByID finds the row whose id field matches, returning its index and the
// whole row so a caller can read the name. The miss status is the same 0x0A the
// name resolvers use, since this is the same "not found" condition.
func LookupByID(data []byte, count StageObjectCount, id int32) (index int, row []byte, status uint16, err error) {
	table := StageObjectTable
	if err := table.validate(); err != nil {
		return 0, nil, 0, err
	}
	if count <= 0 {
		return 0, nil, StatusNameNotFound, nil
	}
	for i := 0; i < int(count); i++ {
		start := table.RowsOffset + i*table.RowSize
		if start+table.RowSize > len(data) {
			return 0, nil, 0, fmt.Errorf("stage object row %d spans +%#x..%#x, past the %d byte table",
				i, start, start+table.RowSize, len(data))
		}
		candidate := data[start : start+table.RowSize]
		if readInt32(candidate[StageObjectIDOffset:]) == id {
			return i, candidate, 0, nil
		}
	}
	return 0, nil, StatusNameNotFound, nil
}

// The pointer rectangle behind pointinset 20041, and the stage view behind
// pointinstage 20042.
//
// FUN_004195F0 loads four words from the set metadata into the set block
// DAT_00459A50..A56 when a set opens. FUN_004057D0 then builds the pointer
// rectangle DAT_00442340..46 from them, in the verified rect order Top, Left,
// Bottom, Right (see Rect):
//
//	DAT_00442340 (Top)    = DAT_00459A56 = metadata +0x16
//	DAT_00442342 (Left)   = DAT_00459A54 = metadata +0x14
//	DAT_00442344 (Bottom) = Top  + DAT_00459A52  (height = metadata +0x2C)
//	DAT_00442346 (Right)  = Left + DAT_00459A50  (width  = metadata +0x2A)
//
// So the width is DAT_00459A50 and the height DAT_00459A52. The shipped set
// metadata fits the 512 by 384 stage only with that assignment.
//
// The stage view rectangle DAT_00459700..07 is set by FUN_0041C6E0 at init as
// (Top 0, Left 0, Bottom 0x180, Right 0x200), so the stage is 512 wide and 384 high.

// Limits and diagnostic codes FUN_004057D0 checks for a set's pointer rectangle.
// Both limit checks are signed comparisons (INT_SLESS in the decompile). A
// diagnostic is reported through FUN_0042C470, which shows a message box; the
// rectangle is still installed, since the rebuild has no early return.
const (
	// SetPointerMaxWidth is the largest width FUN_004057D0 accepts, 0x200, which
	// it tests on DAT_00459A50 (metadata +0x2A).
	SetPointerMaxWidth int16 = 0x200
	// SetPointerMaxHeight is the largest height FUN_004057D0 accepts, 0x180, which
	// it tests on DAT_00459A52 (metadata +0x2C).
	SetPointerMaxHeight int16 = 0x180
	// SetPointerWidthDiag is raised when width > 0x200.
	SetPointerWidthDiag uint16 = 0x13EC
	// SetPointerHeightDiag is raised when height > 0x180.
	SetPointerHeightDiag uint16 = 0x13ED
	// SetPointerOutsideStageDiag is raised when the pointer rectangle is not
	// inside the stage view rectangle.
	SetPointerOutsideStageDiag uint16 = 0x13EE
)

// StageViewRect is DAT_00459700..07, the stage view rectangle FUN_0041C6E0 sets:
// Top 0, Left 0, Bottom 0x180 (384) and Right 0x200 (512).
var StageViewRect = Rect{Top: 0, Left: 0, Bottom: 0x180, Right: 0x200}

// SetPointerRect builds the set's pointer rectangle from its four metadata inputs
// (top +0x16, left +0x14, width +0x2A, height +0x2C), with the same 16-bit wrap
// the native stores use.
func SetPointerRect(top, left, width, height int16) Rect {
	return Rect{Top: top, Left: left, Bottom: top + height, Right: left + width}
}

// CheckSetPointerRect returns the diagnostics FUN_004057D0 raises for a set's
// pointer rectangle, in the order it raises them: width over 0x200 (0x13EC),
// height over 0x180 (0x13ED), then containment (0x13EE). The containment test is
// the native's: FUN_0042E380 takes the bounding union of the set rectangle and the
// stage view, and FUN_0042E2F0 requires that union to equal the stage view. That
// is the same as the set rectangle lying inside the stage view.
func CheckSetPointerRect(width, height int16, rect Rect) []uint16 {
	var codes []uint16
	if width > SetPointerMaxWidth {
		codes = append(codes, SetPointerWidthDiag)
	}
	if height > SetPointerMaxHeight {
		codes = append(codes, SetPointerHeightDiag)
	}
	if unionRect(rect, StageViewRect) != StageViewRect {
		codes = append(codes, SetPointerOutsideStageDiag)
	}
	return codes
}

// unionRect reproduces FUN_0042E380 on two rectangles: the top and left edges
// take the smaller value and the bottom and right edges the larger.
func unionRect(a, b Rect) Rect {
	out := b
	if a.Top < b.Top {
		out.Top = a.Top
	}
	if a.Left < b.Left {
		out.Left = a.Left
	}
	if b.Bottom < a.Bottom {
		out.Bottom = a.Bottom
	}
	if b.Right < a.Right {
		out.Right = a.Right
	}
	return out
}

// PointFromPacked splits a packed 32-bit point the way the point builtins read
// it: the high word is x (pointx, FUN_00415CB0) and the low word is y (pointy,
// FUN_00415D10). FUN_0042E340 compares the high word with the left and right
// edges and the low word with the top and bottom edges.
func PointFromPacked(packed uint32) Point {
	return Point{X: int16(uint16(packed >> 16)), Y: int16(uint16(packed))}
}

// PointInSetValue reproduces pointinset 20041, FUN_0041B330, once its argument
// has been evaluated. An argument that is not numeric (type 4) gives status 0x0E
// with no result. Otherwise the result is a type-2 boolean. The box test runs only
// when DAT_00459A24 and DAT_00459A26 are both set, against the set's pointer
// rectangle, and is false otherwise.
func PointInSetValue(argType uint16, packed uint32, state SetState, rect Rect) (ValueResult, uint16) {
	if argType != ValueTypeNumeric {
		return ValueResult{}, StatusWrongOperandType
	}
	if !PointInSetWillTest(state) {
		return BoolResult(false), 0
	}
	return BoolResult(ContainsPoint(PointFromPacked(packed), rect)), 0
}

// PointInStageValue reproduces pointinstage 20042, FUN_00412690. The argument rule
// is the same as PointInSetValue. The box test runs when the stage is open
// (DAT_00459A00) and DAT_00459A02 is set, against the stage view rectangle. The
// stage's visible flag is the one stagevisible 16008 writes, so this is false
// whenever that flag is clear.
func PointInStageValue(argType uint16, packed uint32, stageOpen, stageVisible bool) (ValueResult, uint16) {
	if argType != ValueTypeNumeric {
		return ValueResult{}, StatusWrongOperandType
	}
	if !stageOpen || !stageVisible {
		return BoolResult(false), 0
	}
	return BoolResult(ContainsPoint(PointFromPacked(packed), StageViewRect)), 0
}

// readInt32 reads a little-endian signed 32-bit value, which is how every 32-bit
// field in the reference is stored.
func readInt32(data []byte) int32 {
	if len(data) < 4 {
		return 0
	}
	return int32(uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24)
}
