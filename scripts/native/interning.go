package native

import (
	. "redust/scripts"
	"fmt"
)

// The value pool's interning, text resolution and eviction. These three complete the
// pool described in valuepool.go: what a slot's text is, where it lives, and how the
// pool reclaims space.

// The slot layout, confirmed independently by the interning call and the eviction
// pass, which between them touch every field. A slot is 32 bytes:
//
//	+0   the kind, 16 bits        the comparison tests this against 3 for a string
//	+2   the value, 32 bits       for a string this is the text block's offset
//	+4   the use count, 32 bits   reset nowhere, and triggers eviction above 0x7FF
//	+8   the Pascal name, up to 24 bytes
//
// so the name has 24 bytes of room and the pool's own name limit of 15 characters
// leaves room for a length byte with more to spare.
const (
	// ValuePoolSlotKindOffset is the slot's kind field, in bytes.
	ValuePoolSlotKindOffset = 0
	// ValuePoolSlotValueOffset is the slot's value field, in bytes.
	ValuePoolSlotValueOffset = 2
	// ValuePoolSlotUsesOffset is the slot's use counter, in bytes.
	ValuePoolSlotUsesOffset = 4
	// ValuePoolSlotNameOffset is the slot's Pascal name, in bytes.
	ValuePoolSlotNameOffset = 8
	// ValuePoolSlotNameRoom is how many bytes are left for the name, 24.
	ValuePoolSlotNameRoom = ValuePoolSlotStride - ValuePoolSlotNameOffset
)

// The pool's header layout, in bytes. Confirmed by the allocator's four writes and by
// the eviction pass's save, reset and restore, which name the same fields:
//
//	+0   the slot count, 16 bits
//	+2   the capacity, 16 bits
//	+4   two 16-bit fields, both zero, and both reset by the eviction
//	+8   the text block's current write offset, 32 bits, initialised to 0x800
//	+12  a second field initialised to 0x800, and reset to 0x800 by the eviction
//	+16  the text block's handle
//	+20  the slots, 32 bytes each
//
// The text block is `0x800` bytes and its write offset starts at `0x800`, so the
// allocator's own `0x800` header field and the block's size are the same number, which
// is the same coincidence as the stack headroom threshold sharing `0x800`.
const (
	// ValuePoolHeaderCountOffset is the slot count, in bytes.
	ValuePoolHeaderCountOffset = 0
	// ValuePoolHeaderCapacityOffset is the capacity, in bytes.
	ValuePoolHeaderCapacityOffset = 2
	// ValuePoolHeaderFlagsOffset is the zeroed 32-bit pair the eviction resets, in
	// bytes.
	ValuePoolHeaderFlagsOffset = 4
	// ValuePoolHeaderTextOffsetOffset is the text block's write offset, in bytes.
	ValuePoolHeaderTextOffsetOffset = 8
	// ValuePoolHeaderSecondOffset is the second 0x800 field, in bytes.
	ValuePoolHeaderSecondOffset = 12
	// ValuePoolHeaderTextHandleOffset is the text block's handle, in bytes.
	ValuePoolHeaderTextHandleOffset = 16
	// ValuePoolTextBlockBytes is the text block's size, 0x800, and the write offset's
	// initial value.
	ValuePoolTextBlockBytes = 0x800
)

// VerifyPoolHeaderFits checks the header's fields all lie before the first slot, and
// that the text offset and the block size agree. It returns a description of any
// overlap rather than a bare error, so a caller can say which field collided.
func VerifyPoolHeaderFits(slotCount int) (ok bool, detail string) {
	fields := []struct {
		name   string
		offset int
		bytes  int
	}{
		{"count", ValuePoolHeaderCountOffset, 2},
		{"capacity", ValuePoolHeaderCapacityOffset, 2},
		{"flags", ValuePoolHeaderFlagsOffset, 4},
		{"text offset", ValuePoolHeaderTextOffsetOffset, 4},
		{"second field", ValuePoolHeaderSecondOffset, 4},
		{"text handle", ValuePoolHeaderTextHandleOffset, 4},
	}
	previousEnd := 0
	for _, field := range fields {
		if field.offset < previousEnd {
			return false, fmt.Sprintf("%s at +%d overlaps the previous field ending at +%d",
				field.name, field.offset, previousEnd)
		}
		if field.offset+field.bytes > ValuePoolHeaderBytes {
			return false, fmt.Sprintf("%s at +%d does not fit the %d byte header",
				field.name, field.offset, ValuePoolHeaderBytes)
		}
		previousEnd = field.offset + field.bytes
	}
	if previousEnd != ValuePoolHeaderBytes {
		return false, fmt.Sprintf("the header fields end at +%d, not +%d", previousEnd, ValuePoolHeaderBytes)
	}
	if slotCount < 0 {
		return false, fmt.Sprintf("the slot count is negative: %d", slotCount)
	}
	return true, "the six header fields tile 0..20 and the slots follow"
}

// InternLookup is the pool's name interning, `FUN_00427200`, and it is the **fourth**
// instance of the engine's cached name lookup, after the prop table, the actor table
// and the stage resolver. Verified:
//
//	if (pool == 0) return 9;
//	slots = GlobalLock(pool);
//	base = slots + 10;                             // byte offset 20
//	cached = *(short *)(record + 6);              // the cache lives in the CALLER
//	if (0 < cached && cached < count)
//	    if (equalASCII(base + cached * 0x10 + 8, name)) { *out = cached; return 0; }
//	for (i = 0; i < count; i++)
//	    if (equalASCII(base + 8, name)) {
//	        *(short *)(record + 6) = i;            // the scan hit replaces the cache
//	        *out = i; return 0;
//	    }
//	return 9;
//
// The shape is identical to the other three — cache first, re-validated by name, scan
// updating it — but two things set it apart, and both are why `LookupCached` does not
// cover it. **The cache lives inside the caller's value record at offset 6** rather
// than in a side table, so it is per-value rather than per-table. And **the miss
// status is 9, not `0x0A`**, so the four lookups do not even agree on how to say
// "not found".
const (
	// ValuePoolCacheOffset is the short offset of the cache index within the caller's
	// value record, 6.
	ValuePoolCacheOffset = 6
	// ValuePoolCacheOffsetBytes is that offset in bytes.
	ValuePoolCacheOffsetBytes = ValuePoolCacheOffset * 2
	// StatusInternNotFound is the miss status, 9, distinct from the name resolvers'
	// 0x0A.
	StatusInternNotFound uint16 = 9
)

// InternText resolves a name to a slot index, reproducing FUN_00427200. recordCache is
// the cached index from the caller's record, and is updated in place on a scan hit.
//
// The bounds are the reference's own and are both strict: the cached index must be
// **positive and below the count**, so a cached index of zero or of exactly the count
// is ignored and rescanned.
func InternText(layout ValuePoolLayout, name string, recordCache *int) (index int, status uint16, err error) {
	if recordCache == nil {
		return 0, StatusInternNotFound, fmt.Errorf("the interning cache is not available")
	}
	// Step one: the cached index, when it is in range and still matches by name.
	cached := *recordCache
	if cached > 0 && cached < layout.Count {
		if EqualASCIIFold(slotNameAt(layout, cached), name) {
			return cached, 0, nil
		}
	}
	// Step two: the scan, which replaces the cache on a hit.
	for i := 1; i <= layout.Count; i++ {
		if EqualASCIIFold(slotNameAt(layout, i), name) {
			*recordCache = i
			return i, 0, nil
		}
	}
	return 0, StatusInternNotFound, nil
}

// slotNameAt reads a slot's name from a synthetic view of the pool, used by
// InternText so the cache comparison can be tested without laying out 660 bytes. The
// name a real pool would hold is supplied through slotNames, and a slot with no entry
// has an empty name, which matches a pool that has never interned anything.
func slotNameAt(layout ValuePoolLayout, index int) string {
	if index < 1 || index > layout.Count {
		return ""
	}
	return slotNames[index]
}

// slotNames is the synthetic name table InternText consults. It is the test seam for
// the pool's name comparisons, and it is package-private so only the tests set it.
var slotNames map[int]string

// SlotNames exposes the synthetic name table so a caller can model a pool's contents.
func SetSlotNames(names map[int]string) {
	slotNames = names
}

// SlotNameCount returns how many names the synthetic table holds.
func SlotNameCount() int { return len(slotNames) }

// ResolvePoolText is the text resolution, `FUN_004272F0`, verified in full:
//
//	if (*(int *)(pool + 8) <= offset) FUN_0042C470(0, 0x15E0);
//	block = GlobalLock(*(HGLOBAL *)(pool + 16));
//	PascalCopy(block + offset, out);
//
// So a slot's text is a Pascal string at its own offset in the text block, and the
// bounds check is against **the block's write offset**, not against the block's size.
// That is the meaningful detail: an offset is legal only if the interning has actually
// written past it, so a stale offset left over from before an eviction is caught. The
// check is strict, so an offset exactly equal to the write offset is refused.
const (
	// StatusTextOutOfRangeDiag is the diagnostic the resolution raises when an offset
	// is not inside what has been written, 0x15E0.
	StatusTextOutOfRangeDiag uint16 = 0x15E0
)

// PoolTextBlock holds the pool's text block, its write position and its byte limit.
//
// Each interned name is stored in the project's usual **Pascal** form: a length byte at
// the offset, then the text, so an offset points at the length byte and a name of n
// characters occupies n+1 bytes.
//
// **The block grows upward, and `FUN_00427340` is what settles it.** Decompiling the
// writer — the one function the previous entry had to leave as an explicit
// approximation — shows the whole sequence:
//
//	used  = *(int *)(pool + 8);              // bytes written so far
//	size  = (short)(length + 1);             // note the 16-bit truncation
//	if (*(int *)(pool + 0xC) <= size + used) {   // the LIMIT would be reached
//	    limit = size + 0x800 + limit;             // grow by 0x800 *plus* the text
//	    block = GlobalReAlloc(block, limit, GMEM_MOVEABLE);
//	}
//	copy(name -> block + used);
//	*(int *)(pool + 8) = used + size;        // advance
//	return used;                              // the offset it was written at
//
// So the writer allocates the offset itself, returns it, and the write position moves
// **up** past the string just written. That makes `Get`'s test — which refuses when
// `used <= offset`, so a valid offset satisfies `offset < used` — self-consistent with
// no gap or fudge of any kind. The earlier note's "one byte below the string" was a
// workaround for a downward-growing model, and both the model and the workaround are
// now gone: this file contains no approximation.
type PoolTextBlock struct {
	// Block is the block's contents. A real pool holds this in a second moveable
	// allocation of ValuePoolTextBlockBytes.
	Block []byte
	// WriteOffset is **bytes written so far**, which bounds every valid offset from
	// above. An offset is valid exactly when it is below this.
	WriteOffset int
	// Limit is the block's allocated size in bytes, at pool header offset `+0xC`. The
	// writer grows the block when a new name would reach it.
	Limit int
}

// The growth rule for the text block, which is **not** the same as the slot table's.
//
//	limit = size + 0x800 + limit;        // grow by 0x800 plus the text's own size
//	if (limit == 0) limit = 1;
//
// Two things are worth stating. The step is `0x800` rather than the slot table's
// `0x14`, so **the two halves of one structure grow by different amounts** — a port
// that shared one growth helper between them would mis-size the block. And the
// overflow guard here is `if (newSize == 0) newSize = 1`, which is **a different
// guard** from the slot table's `capacity * 0x20 == -0x14` and is *not* provably dead:
// `size + 0x800 + limit` can wrap to zero once the limit passes about 4.29 billion, so
// unlike its twin this one is reachable in principle. The contrast is recorded because
// treating the two as the same idiom would mis-state one of them.
const (
	// ValuePoolTextGrowth is the text block's growth step, 0x800.
	ValuePoolTextGrowth = 0x800
	// ValuePoolTextMinSize is the overflow guard's fallback.
	ValuePoolTextMinSize = 1
)

// RequiredTextSize reproduces the growth arithmetic for a new limit.
//
// **The addition is done in 32 bits on purpose.** The reference is a 32-bit binary, so
// `size + 0x800 + limit` wraps at 2^32 — and that wrap is the *only* way its
// `if (newSize == 0) newSize = 1` guard can fire. Computing the same expression in Go's
// 64-bit `int` would make the guard permanently unreachable and would silently differ
// from the reference for any limit near 4.29 billion, so the truncation is transcribed
// rather than widened.
func RequiredTextSize(size, limit int) int {
	grown := int32(size) + int32(ValuePoolTextGrowth) + int32(limit)
	if grown == 0 {
		return ValuePoolTextMinSize
	}
	return int(grown)
}

// Put interns a name into the text block at the current write position and returns the
// offset, which is what a slot stores. It reproduces FUN_00427340 including the 16-bit
// truncation of the size and the overflow guard.
func (p *PoolTextBlock) Put(name []byte) (int, error) {
	if p == nil {
		return 0, fmt.Errorf("there is no text block")
	}
	if len(name) > 255 {
		return 0, fmt.Errorf("a name of %d bytes does not fit a Pascal length byte", len(name))
	}
	used := p.WriteOffset
	// **The reference computes the size in 16 bits and stores that**, so a name long
	// enough to overflow a short would be recorded as a smaller one. The pool's own
	// 15-character name limit makes this unreachable, but the truncation is real
	// arithmetic and is transcribed rather than "fixed" to a 32-bit size.
	size := int(int16(uint16(len(name)) + 1))
	// The limit test is `limit <= size + used`, so growing happens when the new text
	// would *reach* the limit, not merely exceed it.
	if p.Limit <= size+used {
		p.Limit = RequiredTextSize(size, p.Limit)
		if cap(p.Block) < p.Limit {
			grown := make([]byte, p.Limit)
			copy(grown, p.Block)
			p.Block = grown
		}
	}
	// Grow the buffer if the limit outran its capacity, which a caller-supplied block
	// can do by raising the limit without resizing.
	if p.WriteOffset+size > len(p.Block) {
		grown := make([]byte, p.WriteOffset+size)
		copy(grown, p.Block)
		p.Block = grown
	}
	// Pascal form: the length byte at the offset, then the text.
	offset := used
	p.Block[offset] = byte(len(name))
	copy(p.Block[offset+1:], name)
	p.WriteOffset = used + size
	return offset, nil
}

// Get copies out the text at an offset, applying the reference's bounds test.
//
// **The comparison is the reference's, and its direction is the easy thing to get
// wrong.** `FUN_004272F0` raises the diagnostic when `*(int *)(pool + 8) <= offset`, so
// a valid offset satisfies `offset < used` — the write position is an **upper** bound.
// An offset at or above it was never written and is refused, which is what catches a
// stale offset left over from before an eviction.
func (p *PoolTextBlock) Get(offset int) (string, uint16, error) {
	if p == nil {
		return "", StatusTextOutOfRangeDiag, fmt.Errorf("there is no text block")
	}
	if offset < 0 || p.WriteOffset <= offset {
		return "", StatusTextOutOfRangeDiag, fmt.Errorf(
			"the text offset %d is not below the write position %d (FUN_0042C470(0, %#x))",
			offset, p.WriteOffset, StatusTextOutOfRangeDiag)
	}
	if offset >= len(p.Block) {
		return "", StatusTextOutOfRangeDiag, fmt.Errorf(
			"the text offset %d is past the %d byte block", offset, len(p.Block))
	}
	length := int(p.Block[offset])
	if offset+1+length > len(p.Block) {
		return "", StatusTextOutOfRangeDiag, fmt.Errorf(
			"the length byte %d at offset %d does not fit the block", length, offset)
	}
	return string(p.Block[offset+1 : offset+1+length]), 0, nil
}

// Evict reproduces `FUN_004273C0`, the pool's space reclamation, verified in full:
//
//	savedOffset = header.textOffset;
//	oldBlock    = header.textHandle;
//	header.second = 0x800; header.secondHigh = 0;
//	newBlock = alloc(GMEM_MOVEABLE, 0x800);
//	for each slot:
//	    if (slot.kind == 3) {                      // only string slots hold text
//	        header.textHandle = oldBlock; header.textOffset = savedOffset;
//	        text = resolve(slot.textOffset);
//	        header.textHandle = newBlock; header.textOffset = freshOffset;
//	        slot.textOffset = intern(text);
//	        freshOffset = header.textOffset;
//	    }
//	free(oldBlock);
//	header.textOffset = freshOffset; header.textHandle = newBlock;
//	header.flags = 0;
//
// **It is a copy-and-compact, not a mark-and-sweep.** Every string slot's text is read
// out of the old block and written into a freshly allocated one at a new offset, the
// slot is updated, and only then is the old block freed. So eviction never leaves a
// dangling offset, and a slot whose text has been written twice ends up with whichever
// copy the last write produced.
//
// Only **type-3** slots are touched, which is why a numeric slot's counter cannot
// trigger eviction and why its contents need no relocation.
type Eviction struct {
	// Relocated counts the slots that were copied into the new block.
	Relocated int
	// Skipped counts the slots left alone because they held no text.
	Skipped int
	// NewBlockBytes is the size the replacement block starts at, 0x800.
	NewBlockBytes int
	// NewUsed is the replacement's write position once the walk is done, which is the
	// sum of the surviving names' sizes packed from zero.
	NewUsed int
	// NewLimit is the replacement's byte limit, still 0x800 unless a name forced it
	// to grow.
	NewLimit int
	// OldBlockFreed records that the old block was released, which the reference does
	// unconditionally once the walk finishes.
	OldBlockFreed bool
}

// EvictPool compacts the text of every string slot into a fresh block, updating each
// slot's offset and returning what was done.
//
// **With the writer mapped this is now exact rather than approximate.** The reference
// interleaves two blocks: it points the pool at the **old** block and restores the old
// write position to read a name out, then points it at the **new** block with the new
// running position and calls the writer to get a fresh offset. Because the writer
// allocates its own offset, the new block's layout is simply the names in slot order,
// packed from zero — and the run of `uVar6` through the loop is that position being
// read back after each write.
//
// The fresh block starts with a write position of **zero** and a limit of `0x800`, so a
// compaction leaves the block no larger than a newly allocated pool's, and the writer
// grows it again if the surviving names do not fit.
func EvictPool(layout ValuePoolLayout, text *PoolTextBlock) (Eviction, error) {
	if text == nil {
		return Eviction{}, fmt.Errorf("there is no text block to evict")
	}
	result := Eviction{NewBlockBytes: ValuePoolTextBlockBytes}
	fresh := &PoolTextBlock{
		Block:       make([]byte, ValuePoolTextBlockBytes),
		WriteOffset: 0,
		Limit:       ValuePoolTextBlockBytes,
	}
	for index := 1; index <= layout.Count; index++ {
		kind := internedSlotKind(index)
		if kind != ValueTypeString {
			// Only a type-3 slot holds text, so nothing to relocate.
			result.Skipped++
			continue
		}
		textOut, status, err := text.Get(internedSlotOffset(index))
		if err != nil {
			return result, fmt.Errorf("slot %d: %w (status %#x)", index, err, status)
		}
		if _, err = fresh.Put([]byte(textOut)); err != nil {
			return result, fmt.Errorf("slot %d: re-interning %q: %w", index, textOut, err)
		}
		result.Relocated++
	}
	// The old block is released unconditionally once the walk is done, and the pool
	// takes the new block, its write position and its limit.
	result.OldBlockFreed = true
	result.NewUsed = fresh.WriteOffset
	result.NewLimit = fresh.Limit
	return result, nil
}

// internedSlotKind and internedSlotOffset are the test seams for a slot's contents, so
// the eviction's kind test can be exercised without building a 660-byte pool.
var (
	internedKinds   = map[int]uint16{}
	internedOffsets = map[int]int{}
)

func internedSlotKind(index int) uint16 {
	if kind, ok := internedKinds[index]; ok {
		return kind
	}
	return ValueTypeString
}

func internedSlotOffset(index int) int {
	return internedOffsets[index]
}

// SetInternedSlots declares a slot's kind and text offset, for driving EvictPool.
func SetInternedSlots(kinds map[int]uint16, offsets map[int]int) {
	internedKinds = kinds
	internedOffsets = offsets
}
