package render

import (
	"encoding/binary"
	"fmt"
)

// clutRawSize is a CLUT in the port's raw layout: 256 entries of eight bytes,
// each an index word followed by the R, G and B words, little endian. It is the
// native 0x808-byte handle without its eight-byte header (see PaletteStateFromRaw).
const clutRawSize = 256 * 8

// MixPaletteRaw is the blend behind mixclut (command 12082, FUN_00425E90). It
// returns a copy of from in which each R, G and B word of the entries lo..hi
// moves toward the same word of to:
//
//	channel = from + (to - from) * t / 255
//
// lo, hi and t are each clamped to 0..255 first, and the division truncates
// toward zero as C integer division does. When lo is above hi after clamping,
// nothing changes. Index words and entries outside lo..hi keep from's values, and
// neither input is modified. Entries 0 and 255 take part like the others; the
// native apply step (FUN_0042F060) overrides them when the result reaches the
// screen, which the port does not do yet.
func MixPaletteRaw(from, to []byte, lo, hi, t int32) ([]byte, error) {
	if len(from) != clutRawSize || len(to) != clutRawSize {
		return nil, fmt.Errorf("CLUT blend has %d and %d bytes, want %d", len(from), len(to), clutRawSize)
	}
	out := append([]byte(nil), from...)
	first, last := clampCLUTValue(lo), clampCLUTValue(hi)
	weight := int(clampCLUTValue(t))
	for index := int(first); index <= int(last); index++ {
		for channel := 2; channel <= 6; channel += 2 {
			offset := index*8 + channel
			start := int(binary.LittleEndian.Uint16(from[offset:]))
			end := int(binary.LittleEndian.Uint16(to[offset:]))
			binary.LittleEndian.PutUint16(out[offset:], uint16(start+(end-start)*weight/255))
		}
	}
	return out, nil
}

func clampCLUTValue(value int32) int32 {
	return max(0, min(255, value))
}
