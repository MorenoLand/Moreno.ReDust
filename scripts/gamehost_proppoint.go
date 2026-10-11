package scripts

import (
	"encoding/binary"
	"errors"
	"fmt"

	"redust/assets"
)

// PropFrame is the frame FUN_00421B70 selects for a prop record: the variant row it
// copies (Info) and the frame resource that row names (Data).
type PropFrame struct {
	Info assets.PropFrameInfo
	Data []byte
}

// PropPointRecord is what pointinprop reads from one prop record, and the frame its
// cursor selects. Frame is nil when FUN_00421B70 selects no variant.
type PropPointRecord struct {
	// Visible is record word 0, the flag FUN_00421820 tests first.
	Visible bool
	// Projected is record word 9 being non-zero, which selects path B.
	Projected bool
	// Anchor is record words 10 and 11 as one dword, with the low word y and the
	// high word x, the packed point convention of the rest of the point builtins.
	Anchor uint32
	// Walker is record word 20. A negative value runs the unscaled walker
	// FUN_004360C0; a non-negative one runs the scaled walker FUN_004396F0.
	Walker int16
	// Frame is the variant FUN_00421B70 selects at the record's cursor (word 18)
	// and angle (word 12), with the frame resource's bytes.
	Frame *PropFrame
}

// propFrameHeaderBytes is the frame resource's header: word 0 the column count,
// word 1 the row count, and words 2 and 3 the origin's low and high words. Column
// records start at byte 8, as FUN_004360C0 reads them.
const propFrameHeaderBytes = 8

// Native walk faults. FUN_004360C0 and FUN_004361E0 report these through
// FUN_0042C470 and return a zero result. For a point inside the frame box they cannot
// be reached, so they are errors here rather than false results.
var (
	errPropRunPastColumn   = errors.New("prop run walk ran past its column height (native 0x201)")
	errPropColumnNotFound  = errors.New("prop column walk ran past the frame's columns (native 0x200)")
	errPropColumnTruncated = errors.New("prop column record is truncated")
)

// propRunHit is FUN_004361E0, the run walk over one column record. The runs of the
// column cover the rows in order, and the native walk answers the 0-based row r with
// the first run whose running total S satisfies r <= S. So the first run covers rows
// 0..S(0), not 0..S(0)-1, and a row on a run boundary belongs to the earlier run.
// That is one row more inclusive than the drawn partition, which is half-open.
//
// The run type is the low two bits of the header byte and its count is the rest:
// types 0 and 2 answer opaque, type 1 answers clear, and type 3 is an opaque literal
// of count bytes. Type 2 has a second header byte, which is skipped.
func propRunHit(frame []byte, pos int, height, row int16) (bool, error) {
	remaining := height
	for {
		if pos < 0 || pos >= len(frame) {
			return false, errPropRunPastColumn
		}
		head := frame[pos]
		count := int16(head >> 2)
		row -= count
		next := pos + 1
		switch head & 3 {
		case 0:
			if row < 1 {
				return true, nil
			}
		case 1:
			if row < 1 {
				return false, nil
			}
		case 2:
			next = pos + 2
			if row < 1 {
				return true, nil
			}
		case 3:
			next = pos + 1 + int(count)
			if row < 1 {
				return true, nil
			}
		}
		remaining -= count
		if remaining < 1 {
			return false, errPropRunPastColumn
		}
		pos = next
	}
}

// propFrameHit is path A's unscaled hit test, FUN_004360C0 with flip 0. The frame's
// box is the anchor less the frame's own origin, with the frame's column count along
// the low word and its row count along the high word. A point outside that box is
// false. Otherwise the column is the point's low word less the box's top, and the
// row is its high word less the box's left, both 0-based. The column record for the
// column is walked by propRunHit, and its result is the answer.
//
// Only the unscaled walker is ported. Path A reads it whenever record word 20 is
// negative, which every path A record is (see PropPointRecord.Walker).
func propFrameHit(frame []byte, anchor, point uint32) (bool, error) {
	if len(frame) < propFrameHeaderBytes {
		return false, fmt.Errorf("prop frame resource is %d bytes, shorter than its %d byte header", len(frame), propFrameHeaderBytes)
	}
	columns := int16(binary.LittleEndian.Uint16(frame[0:2]))
	rows := int16(binary.LittleEndian.Uint16(frame[2:4]))
	originLow := int16(binary.LittleEndian.Uint16(frame[4:6]))
	originHigh := int16(binary.LittleEndian.Uint16(frame[6:8]))
	top := int16(uint16(anchor)) - originLow
	left := int16(uint16(anchor>>16)) - originHigh
	box := Rect{Top: top, Left: left, Bottom: top + columns, Right: left + rows}
	p := PointFromPacked(point)
	if !ContainsPoint(p, box) {
		return false, nil
	}
	column := p.Y - top
	pos := propFrameHeaderBytes
	for index := int16(0); index < columns; index++ {
		if column < 1 {
			return propRunHit(frame, pos+2, rows, p.X-left)
		}
		column--
		if pos+2 > len(frame) {
			return false, errPropColumnTruncated
		}
		length := int16(binary.LittleEndian.Uint16(frame[pos : pos+2]))
		if length < 0 {
			return false, errPropColumnTruncated
		}
		pos += 2 + int(length)
	}
	return false, errPropColumnNotFound
}

// pointInProp is FUN_0041FA40, pointinprop 20040, on path A. The reference runs these
// steps in order, and the dispatcher has already required "(" and, on status 0, ")":
//
//	FUN_00417070: two comma separated arguments; a missing comma is 0x1C
//	FUN_00421FC0(arg1): the name must be a string, else 0x0E
//	FUN_004213C0(arg1): the name is searched in the prop table; a miss is 0x0A
//	if (arg2 is not numeric) return 0x0E;     // checked after the lookup
//	result: a type-2 boolean, false unless FUN_00421820 projects the record
//	FUN_00421820, path A (word 9 == 0):
//	    word 0 == 0                -> false
//	    FUN_00421B70 selects none  -> false
//	    the frame box clipped to the stage view (FUN_0042E230); empty -> false
//	FUN_0042E340(arg2, clipped box) -> false outside
//	FUN_004360C0 or FUN_004396F0 by the sign of word 20; the result is its low word
//
// The handler's status is 0 on every path past the lookup, because the value it
// writes is either a false or the walker's low word. So no status code is raised
// inside the projection.
func (h *GameHost) pointInProp(call *ScriptCall) (Record, int, uint16, error) {
	if h.Env.PropPoint == nil {
		return Record{}, 0, 0, fmt.Errorf("%w: pointinprop", ErrHostOpcodeUnimplemented)
	}
	args, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	switch {
	case len(args) < 2:
		return Record{}, 0, ScriptStatusMissingComma, nil
	case len(args) > 2:
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	if args[0].Kind != 3 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	prop, found, err := h.Env.PropPoint(args[0].Text)
	if err != nil {
		return Record{}, 0, 0, err
	}
	if !found {
		return Record{}, 0, StatusNameNotFound, nil
	}
	if args[1].Kind != 4 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	hit, err := h.propPointHit(prop, uint32(args[1].Int))
	if err != nil {
		return Record{}, 0, 0, err
	}
	return Record{Kind: 2, Data: boolWord(hit)}, consumed, 0, nil
}

// propPointHit is FUN_00421820 followed by the hit test of FUN_0041FA40, for path A.
// Path B (word 9 non-zero) is not ported. Its visible flag is tested first, as
// the reference does. Past that, its two set gates are verified to give false, which
// is returned. With the set open, the path B projection is not verified, so it is a
// documented gap rather than a value. No port record has word 9 set unless propset,
// propstar, propxyz or propis3d placed it, and the set gate covers those as well.
func (h *GameHost) propPointHit(prop PropPointRecord, point uint32) (bool, error) {
	if !prop.Visible {
		return false, nil
	}
	if prop.Projected {
		_, open := h.setRect()
		if !PointInSetWillTest(SetState{Active: open, Visible: h.setVisible}) {
			return false, nil
		}
		return false, fmt.Errorf("%w: pointinprop of a projected prop (word 9 != 0) with a set open", ErrHostOpcodeUnimplemented)
	}
	if prop.Frame == nil {
		return false, nil
	}
	info := prop.Frame.Info
	top := int16(uint16(prop.Anchor)) - info.OriginY
	left := int16(uint16(prop.Anchor>>16)) - info.OriginX
	frameBox := Rect{Top: top, Left: left, Bottom: top + (info.Bottom - info.Top), Right: left + (info.Right - info.Left)}
	clipped, inside := ClipRect(frameBox, StageViewRect)
	if !inside || !ContainsPoint(PointFromPacked(point), clipped) {
		return false, nil
	}
	if prop.Walker >= 0 {
		return false, fmt.Errorf("%w: pointinprop scaled walk (FUN_004396F0, word 20 >= 0) is not ported", ErrHostOpcodeUnimplemented)
	}
	return propFrameHit(prop.Frame.Data, prop.Anchor, point)
}
