package scripts

import "fmt"

// The path prefix table, from FUN_00417820, and the `path` builtin 16009 that
// exposes it. This closes an item left open when the stage-file resolver was
// converted: its eight candidate prefixes and the `path` builtin's eight values
// are the same table.
//
// FUN_00417820 is a bounds check and a copy:
//
//	if (DAT_00443C28 == 0 || index < 0 || 8 < index) FUN_0042C470(0, 0x1459);
//	FUN_0042E6C0((byte *)(index * 0x100 + DAT_00443C28), out);
//
// So the table is nine slots of 256 bytes each, indexed 0 through 8. Both
// callers use only 1 through 8: `path` 16009 tests `0 < value && value < 9` and
// the stage-file resolver loops to 8 before giving up with 0x2A. **Slot 0 is
// therefore allocated but never used**, which is worth knowing before anyone
// indexes the table from zero. The table is filled at startup, so its contents
// are not in the static image; the geometry is what is verified here.

const (
	// PathPrefixSlotSize is the 0x100 byte stride between slots.
	PathPrefixSlotSize = 0x100
	// PathPrefixSlots is the number of slots the table allocates, indices 0..7
	// after 0, that is nine.
	PathPrefixSlots = 9
	// PathPrefixFirst and PathPrefixLast are the indices callers actually use.
	// Note the asymmetry with PathPrefixSlots: nine are allocated, eight are used.
	PathPrefixFirst = 1
	PathPrefixLast  = 8
	// PathPrefixCount is the number of usable values, which is also the number
	// of candidate prefixes the stage-file resolver tries.
	PathPrefixCount = PathPrefixLast - PathPrefixFirst + 1
	// PathPrefixOutOfRangeDiag is the second argument of the
	// FUN_0042C470(0, 0x1459) raised for an out-of-range or unallocated index.
	// It is a diagnostic, not a recoverable status.
	PathPrefixOutOfRangeDiag uint16 = 0x1459
)

// StageNamePrefixCandidates and PathPrefixCount are the same verified eight. A
// test asserts they agree, since the two were transcribed from different
// functions and a divergence would mean the stage resolver and the `path` builtin
// were searching different places.
const _ = uint(StageNamePrefixCandidates - PathPrefixCount)

// PathPrefixRange reports whether a value is a usable path index. It deliberately
// excludes 0, which the reference's callers also exclude.
func PathPrefixRange(value int) bool {
	return value >= PathPrefixFirst && value <= PathPrefixLast
}

// PathPrefixSlot returns the index of the storage slot for a path value, which
// the reference computes as `index * 0x100` from the table base. It is separated
// from lookup so the geometry can be asserted without a populated table.
func PathPrefixSlot(value int) (int, error) {
	if !PathPrefixRange(value) {
		return 0, fmt.Errorf("path %d is outside the usable range %d..%d (FUN_0042C470(0, %#x))",
			value, PathPrefixFirst, PathPrefixLast, PathPrefixOutOfRangeDiag)
	}
	return value * PathPrefixSlotSize, nil
}

// PathPrefixTable holds the resolved prefix text. The native table is populated
// at startup, so this is the Go side of that population rather than a static
// transcription.
type PathPrefixTable struct {
	// Prefixes holds one entry per allocated slot, index 0 included even though
	// no caller uses it.
	Prefixes []string
}

// NewPathPrefixTable builds a table with the verified nine slots, filling any
// that are not supplied with the empty string so a lookup of an unused slot
// cannot read past the end.
func NewPathPrefixTable(prefixes ...string) *PathPrefixTable {
	table := &PathPrefixTable{Prefixes: make([]string, PathPrefixSlots)}
	for i := range table.Prefixes {
		if i < len(prefixes) {
			table.Prefixes[i] = prefixes[i]
		}
	}
	return table
}

// Lookup returns the prefix for a path value and the native status. An
// out-of-range value is the same 0x0A the `path` builtin returns; an unpopulated
// table is the reference's 0x1459 diagnostic surfaced as an error, since a
// missing table is a setup failure rather than a script-visible status.
func (t *PathPrefixTable) Lookup(value int) (string, uint16, error) {
	if _, err := PathPrefixSlot(value); err != nil {
		return "", StatusNameNotFound, err
	}
	if t == nil || t.Prefixes == nil {
		return "", 0, fmt.Errorf("path table is not populated (FUN_0042C470(0, %#x))", PathPrefixOutOfRangeDiag)
	}
	if value >= len(t.Prefixes) {
		return "", 0, fmt.Errorf("path %d is past the %d allocated slots", value, len(t.Prefixes))
	}
	return t.Prefixes[value], 0, nil
}

// PathValue resolves the `path` builtin, opcode 16009, whose handler
// FUN_00415B70 is exactly:
//
//	if (arg.Kind != 4) return 0x0E;
//	if (0 < arg.Value && arg.Value < 9) {
//	    FUN_00417820(arg.Value, &scratch);
//	    return FUN_00421F60(&scratch, out);   // store as a string register
//	}
//	return 0x0A;
//
// So the argument must be a number, the value must be 1 through 8, and the result
// is a type-3 string register holding that prefix. The guard order matters: a
// non-numeric argument is 0x0E and a number outside 1..8 is 0x0A, so a script
// passing a name gets a type error rather than a range error.
//
// registers is the caller's shared string register pool, which the reference
// reaches through the globals DAT_00443EE8 and DAT_00443EF0. It is injected
// rather than created here because the pool is global state in the native: a
// register stored by one builtin is the same register a later builtin loads, and
// creating a private pool per call would break that.
func PathValue(table *PathPrefixTable, argument Record, registers *StringRegisters) (Record, uint16, error) {
	if argument.Kind != ValueTypeNumeric {
		return Record{}, StatusWrongOperandType, ErrWrongOperandType
	}
	value := int(int32(argument.Data))
	if !PathPrefixRange(value) {
		return Record{}, StatusNameNotFound, nil
	}
	prefix, status, err := table.Lookup(value)
	if err != nil {
		return Record{}, 0, err
	}
	if registers == nil {
		return Record{}, 0, fmt.Errorf("path needs a string register pool")
	}
	record, storeStatus, err := registers.Store(PascalFromCString(prefix))
	if err != nil {
		return Record{}, 0, err
	}
	if storeStatus != 0 {
		return Record{}, storeStatus, nil
	}
	return record, status, nil
}

// Prop block field offsets, from FUN_00421820, which projects a prop to the
// screen. The prop is addressed as a `short *`, so the indexes Ghidra prints are
// doubled to reach byte offsets.
//
//	param_1[0x0C]  byte 0x18  an argument passed to the projector
//	param_1[0x09]  byte 0x12  mode selector: 0 takes the simple path
//	param_1[0x0A]  byte 0x14  prop screen x
//	param_1[0x0B]  byte 0x16  prop screen y
//	param_1[0x14]  byte 0x28  copied to the result's word 3
//	*(u32 *)(param_1 + 10)  bytes 0x14..0x1B  copied to the result's words 0..1
const (
	PropOffsetProjectArg = 0x0C * 2
	PropOffsetMode       = 0x09 * 2
	PropOffsetX          = 0x0A * 2
	PropOffsetY          = 0x0B * 2
	PropOffsetExtra      = 0x014 * 2
	// PropOffsetPosition is where the x and y words begin, and eight bytes are
	// copied from here to the start of the result.
	PropOffsetPosition = 0x0A * 2
	// PropBlockSize is the size the pointinprop handler reserves for a prop,
	// 80 shorts.
	PropBlockSize = 80 * 2
	// PropModeSimple is the value of PropOffsetMode that selects the
	// project-then-rect path; anything else takes the alternative branch.
	PropModeSimple uint16 = 0
)

// PropBlock is a loaded prop, as pointinprop fetches it with FUN_004213C0 into a
// PropBlockSize-byte buffer.
type PropBlock struct {
	// Block is the raw prop data, PropBlockSize bytes.
	Block []byte
	// Viewport is the visible rectangle the projection is relative to.
	Viewport Rect
}

// readPropWord reads a signed 16-bit field at a byte offset.
func (p PropBlock) readPropWord(offset int) (int16, error) {
	if offset < 0 || offset+2 > len(p.Block) {
		return 0, fmt.Errorf("prop field at +%#x is past the %d byte block", offset, len(p.Block))
	}
	return int16(uint16(p.Block[offset]) | uint16(p.Block[offset+1])<<8), nil
}

// PropMode reports the prop's mode selector, the field that decides whether the
// simple projection path is taken.
func (p PropBlock) PropMode() (uint16, error) {
	value, err := p.readPropWord(PropOffsetMode)
	if err != nil {
		return 0, err
	}
	return uint16(value), nil
}

// PropPosition reports the prop's screen position, read from the two consecutive
// words at PropOffsetX and PropOffsetY.
func (p PropBlock) PropPosition() (Point, error) {
	x, err := p.readPropWord(PropOffsetX)
	if err != nil {
		return Point{}, err
	}
	y, err := p.readPropWord(PropOffsetY)
	if err != nil {
		return Point{}, err
	}
	return Point{X: x, Y: y}, nil
}

// PropExtra reports the field copied to the result's word 3.
func (p PropBlock) PropExtra() (int16, error) {
	return p.readPropWord(PropOffsetExtra)
}

// PropScreenRect reproduces the rectangle FUN_00421820 builds before clipping:
//
//	vpWidth  = viewRight - viewLeft
//	vpHeight = viewBottom - viewTop
//	rect.left   = propX - viewLeft
//	rect.top    = propY - viewTop
//	rect.right  = rect.left + vpWidth
//	rect.bottom = rect.top + vpHeight
//
// So the rect is the prop's position expressed relative to the viewport origin,
// with the viewport's own width and height as the extent. It is then handed to
// the clipper, FUN_0042E230, which this does not model. A prop that is not loaded
// at all, that is `*param_1 == 0`, makes the reference return success with no
// rect written, which is why a zero prop yields an error here rather than a
// degenerate rectangle.
func (p PropBlock) PropScreenRect() (Rect, error) {
	if len(p.Block) == 0 {
		return Rect{}, fmt.Errorf("prop is not loaded, so the reference writes no rect")
	}
	position, err := p.PropPosition()
	if err != nil {
		return Rect{}, err
	}
	view := p.Viewport
	width := int16(uint16(view.Right) - uint16(view.Left))
	height := int16(uint16(view.Bottom) - uint16(view.Top))
	left := int16(uint16(position.X) - uint16(view.Left))
	top := int16(uint16(position.Y) - uint16(view.Top))
	return Rect{
		Left:   left,
		Top:    top,
		Right:  int16(uint16(left) + uint16(width)),
		Bottom: int16(uint16(top) + uint16(height)),
	}, nil
}
