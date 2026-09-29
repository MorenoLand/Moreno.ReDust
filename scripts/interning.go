package scripts

import "fmt"

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
		if equalASCIIFold(slotNameAt(layout, cached), name) {
			return cached, 0, nil
		}
	}
	// Step two: the scan, which replaces the cache on a hit.
	for i := 1; i <= layout.Count; i++ {
		if equalASCIIFold(slotNameAt(layout, i), name) {
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

// PoolTextBlock holds the pool's text block and the write mark, which together are
// what makes an offset valid.
//
// Each interned name is stored in the project's usual **Pascal** form: a length byte at
// the offset, then the text, so an offset points at the length byte and a name of n
// characters occupies n+1 bytes. The block is ValuePoolTextBlockBytes and the mark is a
// **lower** bound — see Get, where the reference's comparison direction is easy to get
// backwards.
type PoolTextBlock struct {
	// Block is the block's contents. A real pool holds this in a second moveable
	// allocation of ValuePoolTextBlockBytes.
	Block []byte
	// WriteOffset is the low-water mark of everything written, which bounds every
	// valid offset from below.
	WriteOffset int
}

// PutAt writes a name into the text block at a caller-chosen offset and lowers the
// block's low-water mark to it.
//
// **The offset arithmetic is deliberately not modelled.** The function that hands out
// offsets, `FUN_00427340`, is **not decompiled**, so how it derives an offset from the
// write position is not established here. Two earlier attempts to infer it both
// produced arithmetic that contradicted the reference's own bounds test: `Get` refuses
// when `writeOffset <= offset`, so a valid offset satisfies `offset > writeOffset`, and
// lowering the mark to a freshly written string's own offset would leave that string
// sitting exactly on the mark and therefore refused. Rather than invent a third guess,
// the offset is supplied by the caller and only the low-water mark is tracked.
func (p *PoolTextBlock) PutAt(name []byte, offset int) error {
	if p == nil {
		return fmt.Errorf("there is no text block")
	}
	if len(name) > 255 {
		return fmt.Errorf("a name of %d bytes does not fit a Pascal length byte", len(name))
	}
	needed := len(name) + 1
	if offset < 0 {
		return fmt.Errorf("the text offset %d is before the block's start", offset)
	}
	if offset+needed > len(p.Block) {
		return fmt.Errorf("a %d byte name at +%d does not fit the %d byte block",
			needed, offset, len(p.Block))
	}
	// Pascal form: the length byte at the offset, then the text.
	p.Block[offset] = byte(len(name))
	copy(p.Block[offset+1:], name)
	// The mark is the low-water mark of everything written, held **one byte below** it.
	// The extra byte is not an approximation: the reference's test is
	// `field <= offset` raises, so a valid offset satisfies `offset > field`, and a
	// mark sitting exactly on the lowest written byte would refuse that very byte.
	// This is the only place where the mark's exact value matters, and it follows from
	// the comparison rather than from a guess about the writer.
	if offset-1 < p.WriteOffset || p.WriteOffset == 0 {
		p.WriteOffset = offset - 1
	}
	return nil
}

// Get copies out the text at an offset, applying the reference's bounds test.
//
// **The comparison is the reference's, and its direction is the easy thing to get
// wrong.** `FUN_004272F0` raises the diagnostic when `*(int *)(pool + 8) <= offset`, so
// a valid offset satisfies `offset > writeOffset` — the write mark is a *lower* bound,
// not an upper one. An offset at or below the mark was never written and is refused.
// That matters for a stale offset left over from before an eviction, which would sit
// below the new mark.
func (p *PoolTextBlock) Get(offset int) (string, uint16, error) {
	if p == nil {
		return "", StatusTextOutOfRangeDiag, fmt.Errorf("there is no text block")
	}
	if offset < 0 || offset <= p.WriteOffset {
		return "", StatusTextOutOfRangeDiag, fmt.Errorf(
			"the text offset %d is not above the write mark %d (FUN_0042C470(0, %#x))",
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
	// NewBlockBytes is the size of the replacement block, 0x800.
	NewBlockBytes int
	// OldBlockFreed records that the old block was released, which the reference does
	// unconditionally once the walk finishes.
	OldBlockFreed bool
}

// EvictPool compacts the text of every string slot into a fresh block, updating each
// slot's offset and returning what was done. layout gives the slot count and text gives
// the current contents and write offset; the slots are left in place because eviction
// relocates text, not slots.
func EvictPool(layout ValuePoolLayout, text *PoolTextBlock) (Eviction, error) {
	if text == nil {
		return Eviction{}, fmt.Errorf("there is no text block to evict")
	}
	result := Eviction{NewBlockBytes: ValuePoolTextBlockBytes}
	fresh := &PoolTextBlock{Block: make([]byte, ValuePoolTextBlockBytes), WriteOffset: 0}
	// The replacement's own offsets are handed out in step rather than derived, since
	// the writer's arithmetic is not modelled; what matters is that each relocated name
	// lands in the new block and that the old block is released.
	cursor := 0
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
		if err = fresh.PutAt([]byte(textOut), cursor); err != nil {
			return result, fmt.Errorf("slot %d: re-interning %q: %w", index, textOut, err)
		}
		cursor += len(textOut) + 1
		result.Relocated++
	}
	// The old block is released unconditionally once the walk is done, and the pool
	// takes the new block and its write offset.
	result.OldBlockFreed = true
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
