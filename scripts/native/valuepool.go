package native

import (
	"fmt"
)

// The value pool, which is what a block body's names are interned into and compared
// through. It is allocated per block-list run by `FUN_0041C9A0`, passed to the
// executor as its pool argument, and freed when the run ends. Three functions define
// it completely: the allocator, the interning call and the comparison.

// The allocator, `FUN_00426D80`, verified in full:
//
//	hMem = GlobalAlloc(GMEM_MOVEABLE, 0x294);
//	p = GlobalLock(hMem);
//	*(short *)p      = 0;                          // the count
//	*(short *)(p+1)  = 0x14;                       // the capacity, 20
//	*(int   *)(p+2)  = 0;
//	*(int   *)(p+3)  = 0;
//	*(int   *)(p+4)  = 0x800;                      // 2048
//	*(HGLOBAL *)(p+8) = GlobalAlloc(GMEM_MOVEABLE, 0x800);
//	GlobalUnlock;
//	return hMem;
//
// **The header size and the slot geometry fit exactly, which is what confirms the
// layout.** The allocation is `0x294` = 660 bytes, the header fields occupy the first
// 20, and the remaining 640 bytes are 20 slots of 32. So the pool is a *fixed* initial
// block with its slots inline, and the reallocation only matters once the count
// outgrows twenty.
//
// The `0x800` in the header is **the same constant as the stack headroom threshold** the
// frame runner and the expression evaluator both test against, and it is also the
// size of the second block, so the threshold and the thing being measured agree.
const (
	// ValuePoolHeaderSize is the pool's initial allocation, 0x294.
	ValuePoolHeaderSize = 0x294
	// ValuePoolHeaderBytes is how many bytes the header's own fields occupy, 20,
	// which is the first slot's byte offset.
	ValuePoolHeaderBytes = 20
	// ValuePoolInitialCapacity is the initial slot count, 0x14.
	ValuePoolInitialCapacity = 0x14
	// ValuePoolGrowth is how many slots the capacity rises by when the count
	// overtakes it, also 0x14. The same grow-by-twenty rule the per-file
	// sub-resource cache uses, so it is a house idiom rather than a coincidence.
	ValuePoolGrowth = 0x14
	// ValuePoolSlotStride is a slot's size in bytes, 32, which is eight DWORDs and
	// matches the copy loop's eight iterations.
	ValuePoolSlotStride = 32
	// ValuePoolSlotDwords is that in DWORDs, eight.
	ValuePoolSlotDwords = 8
	// ValuePoolStackBytes is the second block's size, 0x800, and also the headroom
	// threshold the frame runner and the evaluator test against.
	ValuePoolStackBytes = 0x800
	// ValuePoolMaxSlots is the largest count the pool accepts, 32000. Beyond it the
	// interning call returns the range status rather than growing.
	ValuePoolMaxSlots = 32000
	// ValuePoolNameLimit is the longest name that can be interned, 0x0F = 15
	// characters. A longer one is refused with the length status, which is why
	// script names in the pool's contents are all short.
	ValuePoolNameLimit = 0x0F
	// ValuePoolEvictAbove is the use count above which an eviction pass runs,
	// 0x7FF = 2047. The test is strict, so a count of 2048 triggers it.
	ValuePoolEvictAbove = 0x7FF
	// ValuePoolAllocFlag is GMEM_MOVEABLE, the flag both allocations use.
	ValuePoolAllocFlag uint32 = 0x0002
)

// The two statuses the pool raises, verified from both callers. They are distinct
// from every status the rest of the pool entry path uses, and the range status in
// particular appears in both the interning and the comparison.
const (
	// StatusPoolNameTooLong is returned when a name exceeds ValuePoolNameLimit.
	// It is 1, which is also the dispatcher's default status for an unknown opcode,
	// so the two are indistinguishable from the code alone.
	StatusPoolNameTooLong uint16 = 1
	// StatusPoolSlotRange is returned when a slot index is out of range, 9.
	StatusPoolSlotRange uint16 = 9
)

// The capacity-growth guard, transcribed verbatim because it appears in **two**
// functions with **identical** arithmetic. From the interning call:
//
//	if (capacity < count) {
//	    capacity += 0x14;
//	    if (capacity * 0x20 == -0x14) bytes = 1;
//	    else bytes = capacity * 0x20 + 0x14;
//	    pool = GlobalReAlloc(pool, bytes, GMEM_MOVEABLE);
//	}
//
// and the sub-resource cache in `FUN_004014F0` has the same shape.
//
// **The guard is unreachable, and that is provable rather than merely likely.** It
// asks whether `capacity * 32` equals `-20`. Over the integers there is no solution,
// since 32 does not divide 20. It is still unreachable if the multiplication wraps at
// 32 bits: the congruence `32c ≡ -20 (mod 2^32)` has no solution either, because
// `gcd(32, 2^32) = 32` and 32 does not divide 20. So no capacity, however large,
// makes the guard fire, and `RequiredPoolSize` always takes its ordinary path.
//
// It is kept rather than deleted for two reasons. It is in the reference twice, so
// removing it is second-guessing two separate functions; and "this guard is dead" is
// itself a verified claim about the engine rather than an assumption, which a port
// reading the same code will otherwise have to re-derive.
const (
	// ValuePoolSlotBytesDivisor is the multiplier the size arithmetic uses, 0x20.
	ValuePoolSlotBytesDivisor = 0x20
	// ValuePoolSizeHeaderAdd is the constant added for the header, 0x14.
	ValuePoolSizeHeaderAdd = 0x14
	// ValuePoolOverflowGuardValue is the sentinel the guard compares against, -0x14.
	ValuePoolOverflowGuardValue = -0x14
	// ValuePoolMinimumSize is what the guard would fall back to if it could fire.
	ValuePoolMinimumSize = 1
)

// ValuePoolGuardIsUnreachable reports whether the overflow guard can ever fire. It
// cannot, and the reason is a divisibility fact rather than a search: the guard asks
// for `capacity * 32 == -20`, which has an integer solution only if 32 divides 20.
// It does not. Nor does 32 divide 20 modulo 2^32, since the gcd of 32 and 2^32 is
// 32, so wrapping the multiplication at 32 bits does not produce a solution either.
func ValuePoolGuardIsUnreachable() bool {
	divisor := ValuePoolSlotBytesDivisor
	return ValuePoolOverflowGuardValue%divisor != 0
}

// RequiredPoolSize reproduces the growth arithmetic. newCapacity is the capacity
// after the grow-by-twenty step, and the result is the byte size to reallocate to.
// The overflow guard is present but unreachable, so the ordinary path always runs.
func RequiredPoolSize(newCapacity int) int {
	sized := newCapacity * ValuePoolSlotBytesDivisor
	if sized == ValuePoolOverflowGuardValue {
		return ValuePoolMinimumSize
	}
	return sized + ValuePoolSizeHeaderAdd
}

// ValuePoolLayout is the pool's geometry, derived from the allocator and the two
// users. Slot indices are **1-based**: the interning call stores count+1 and the
// comparison rejects anything below one, and the first slot's byte offset is 20.
type ValuePoolLayout struct {
	// Capacity is the current slot capacity.
	Capacity int
	// Count is how many slots are in use.
	Count int
}

// NewValuePoolLayout returns the layout of a freshly allocated pool.
func NewValuePoolLayout() ValuePoolLayout {
	return ValuePoolLayout{Capacity: ValuePoolInitialCapacity, Count: 0}
}

// SlotOffset returns a slot's byte offset from the start of the pool. Index 0 is
// invalid and reported as such, because the reference rejects it.
func (l ValuePoolLayout) SlotOffset(index int) (int, error) {
	if index < 1 {
		return 0, fmt.Errorf("slot %d is invalid; the pool's indices are 1-based", index)
	}
	return ValuePoolHeaderBytes + (index-1)*ValuePoolSlotStride, nil
}

// Grow applies the growth rule and returns the new layout and the byte size to
// reallocate to. It reports whether a reallocation is needed at all, so a caller can
// skip the call entirely on the common in-capacity path.
//
// The reference performs a **single** twenty-slot step, which is sufficient because a
// miss raises the count by exactly one and so can only ever exceed the capacity by
// one. A loop is used here instead, matching the sub-resource cache's helper, so that
// a pool handed an inconsistent count cannot be made to under-allocate. The test
// asserts the growth lands on whole steps either way.
func (l ValuePoolLayout) Grow() (layout ValuePoolLayout, realloc bool, size int) {
	count := l.Count + 1
	capacity := l.Capacity
	for capacity < count {
		capacity += ValuePoolGrowth
	}
	if capacity == l.Capacity {
		return ValuePoolLayout{Capacity: capacity, Count: count}, false, 0
	}
	return ValuePoolLayout{Capacity: capacity, Count: count}, true, RequiredPoolSize(capacity)
}

// InternNames validates and stores one name, returning the 1-based slot index it was
// given. This reproduces the checks the interning call `FUN_00427020` makes before it
// touches the pool: the length first, then the slot limit, then the growth.
//
// A name of ValuePoolNameLimit characters or fewer is interned. The returned value
// is the slot index, and the reference's caller stores it as a **type-4 numeric**
// record, which is why a block body's values are numbers rather than strings.
func (l ValuePoolLayout) InternNames(names []string) (layout ValuePoolLayout, slots []int, status uint16, err error) {
	layout = l
	slots = make([]int, 0, len(names))
	for _, name := range names {
		if len(name) > ValuePoolNameLimit {
			return layout, nil, StatusPoolNameTooLong, fmt.Errorf(
				"the name %q is %d characters, over the %d character limit (status %#x)",
				name, len(name), ValuePoolNameLimit, StatusPoolNameTooLong)
		}
		count := layout.Count + 1
		// The slot limit is checked against the count *before* the increment, so a
		// pool holding 32000 slots refuses the next name rather than growing.
		if layout.Count >= ValuePoolMaxSlots {
			return layout, nil, StatusPoolSlotRange, fmt.Errorf(
				"the pool already holds %d slots, the limit (status %#x)",
				layout.Count, StatusPoolSlotRange)
		}
		next, _, _ := layout.Grow()
		layout = next
		slots = append(slots, count)
	}
	return layout, slots, 0, nil
}

// ResolvePoolSlot checks a slot index the way the comparison `FUN_00426F20` does,
// which is the only validation on that path. The verified test is
// `param_2 < *pool && -1 < param_2`: the index must be positive and below the count,
// so it is **1-based and exclusive of the count itself**.
func (l ValuePoolLayout) ResolvePoolSlot(index int) error {
	if index < 1 {
		return fmt.Errorf("slot %d is not positive; the pool's indices start at one (status %#x)",
			index, StatusPoolSlotRange)
	}
	if index >= l.Count {
		return fmt.Errorf("slot %d is not below the pool's count %d (status %#x)",
			index, l.Count, StatusPoolSlotRange)
	}
	return nil
}

// ValuePoolSlot is one slot's contents. The comparison copies all eight DWORDs of a
// slot, so the type and the value are the two leading DWORDs and the rest is opaque
// here.
type ValuePoolSlot struct {
	// Kind is the record kind stored at the slot. The interning call writes 4, a
	// numeric, and the comparison special-cases 3, a string.
	Kind uint16
	// Value is the slot's value word. For an interned name this is the index of the
	// interned text within the second block.
	Value int32
	// Uses is the use counter at slot offset +4, which triggers an eviction pass
	// once it exceeds ValuePoolEvictAbove.
	Uses int32
	// Name is the interned text, which the comparison resolves through the second
	// block. It is not stored inline.
	Name string
}

// EvictionNeeded reports whether a slot's use count has crossed the threshold. The
// test is strict: a count of exactly ValuePoolEvictAbove does not evict, and
// ValuePoolEvictAbove+1 does.
func (s ValuePoolSlot) EvictionNeeded() bool {
	return s.Uses > ValuePoolEvictAbove
}

// ResolvePoolSlotForComparison reproduces the comparison's per-slot work, which is
// where the use counter lives and where eviction is decided. isString reports whether
// the slot holds a type-3 record, since only strings resolve their text and bump the
// counter.
func ResolvePoolSlotForComparison(slot ValuePoolSlot, isString bool, nameLength int) (uses int32, evict bool) {
	if !isString {
		// Only a string slot resolves and counts; anything else is compared as
		// stored, which is why a numeric slot's counter never moves.
		return slot.Uses, false
	}
	uses = slot.Uses + int32(nameLength) + 1
	return uses, uses > ValuePoolEvictAbove
}
