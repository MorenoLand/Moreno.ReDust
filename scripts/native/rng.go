package native

import (
	"fmt"
)

// The raw random source, `FUN_0042E740`, mapped in full:
//
//	index = (short)(index + 1) % 0x37;
//	a = table[(index + 0x36) % 0x37];      // 0x36 = 54, which is -1 mod 55
//	b = table[(index + 0x17) % 0x37];      // 0x17 = 23
//	table[index] = (a + b) & 0x7FFFFFFF;
//	return a + b;
//
// So it is a **55-slot circular table** with the recurrence
// `table[i] = (table[i-1] + table[i+23]) & 0x7FFFFFFF`, using the stored values. The
// `+0x36` is a way of writing `-1 mod 55` in positive arithmetic, and it is the clearest
// sign the author's own comment on the constants would read "lag 1 and lag 23".
//
// **The return value is unmasked while the stored value is masked.** That asymmetry is
// the load-bearing detail: a caller can receive a sum as large as `2^32 - 2`, well
// above the `0x7FFFFFFF` any single stored entry holds. A port that masked the return
// as well would halve the range the engine actually produces, and the one-based wrapper
// `FUN_0042E7A0` — already converted — would then be built on a range the reference
// never has.
const (
	// RawRandomSlots is the table's length, 0x37 = 55.
	RawRandomSlots = 0x37
	// RawRandomLag1 is the first lag, 1, written by the reference as 0x36.
	RawRandomLag1 = 1
	// RawRandomLag1Written is how the reference writes the first lag: 0x36, which is
	// 54, and equals -1 modulo 55. It is recorded so the transcription can be checked
	// against the disassembly rather than silently "simplified" to a negative offset.
	RawRandomLag1Written = 0x36
	// RawRandomLag2 is the second lag, 23, 0x17.
	RawRandomLag2 = 0x17
	// RawRandomMask is the mask applied to the **stored** value, 0x7FFFFFFF. It is a
	// 31-bit mask, so every stored entry is a non-negative 31-bit number.
	RawRandomMask uint32 = 0x7FFFFFFF
	// RawRandomMaxStored is the largest value a slot can hold.
	RawRandomMaxStored = RawRandomMask
	// RawRandomMaxReturned is the largest value the function can **return**, which is
	// the sum of two maximal stored entries and so exceeds the mask.
	RawRandomMaxReturned uint32 = 2 * RawRandomMask
)

// RawRandom is the generator's state: the circular table and the index it advances.
type RawRandom struct {
	// Table holds the 55 stored entries, each already masked.
	Table [RawRandomSlots]uint32
	// Index is the slot the next write lands in, in 0..54.
	Index int32
}

// NewRawRandom returns a generator with the given seed values already in the table. The
// reference's own seeding is not decompiled, so this takes the table as given rather
// than inventing a seeding routine.
func NewRawRandom(seed [RawRandomSlots]uint32) *RawRandom {
	r := &RawRandom{}
	for i, v := range seed {
		// The table only ever holds masked values, so a seed above the mask would not
		// be a state the reference could produce.
		r.Table[i] = v & RawRandomMask
	}
	return r
}

// Next advances the generator one step and returns the **unmasked** sum, reproducing
// FUN_0042E740.
//
// The index is advanced **first** and then the two lags are read relative to the new
// position, so the first call after seeding reads the table at its initial state and
// only then writes. The stored value is masked and the returned one is not, which is
// the reference's asymmetry.
func (r *RawRandom) Next() uint32 {
	if r == nil {
		return 0
	}
	// The reference stores the index in a 16-bit field and truncates the increment
	// through it, so a state whose index has been set beyond a short wraps. Doing the
	// same keeps the step identical for every reachable state.
	r.Index = int32(int16(r.Index + 1))
	if r.Index < 0 {
		r.Index += RawRandomSlots
	}
	r.Index %= RawRandomSlots
	// Lag 1 is written 0x36 in the reference, which is -1 modulo 55; lag 2 is 0x17.
	lagged1 := r.Table[(r.Index+RawRandomLag1Written)%RawRandomSlots]
	lagged2 := r.Table[(r.Index+RawRandomLag2)%RawRandomSlots]
	sum := lagged1 + lagged2
	r.Table[r.Index] = sum & RawRandomMask
	// **The return is the unmasked sum.**
	return sum
}

// Value is an alias for Next, named for what the engine's wrappers actually want: the
// next raw draw.
func (r *RawRandom) Value() uint32 { return r.Next() }

// RawRandomState describes a generator's state for a test or a diagnostic.
type RawRandomState struct {
	// Index is the next slot to be written.
	Index int32
	// NonZero counts the slots holding a non-zero value, which is how a caller can
	// tell a seeded generator from an all-zero one without comparing every entry.
	NonZero int
	// Max is the largest stored value.
	Max uint32
}

// State summarises the generator without exposing the whole table.
func (r *RawRandom) State() RawRandomState {
	if r == nil {
		return RawRandomState{}
	}
	state := RawRandomState{Index: r.Index}
	for _, v := range r.Table {
		if v != 0 {
			state.NonZero++
		}
		if v > state.Max {
			state.Max = v
		}
	}
	return state
}

// VerifyRawRandomLags checks the three ways the lags are written all agree, since the
// reference's `+0x36` is a disguised `-1` and a port that "simplified" it to `-1` would
// still be right — but only if the simplification is checked rather than assumed.
func VerifyRawRandomLags() error {
	if RawRandomLag1Written%RawRandomSlots != (RawRandomSlots - RawRandomLag1) {
		return fmt.Errorf("the written lag %d is not %d modulo %d",
			RawRandomLag1Written, RawRandomSlots-RawRandomLag1, RawRandomSlots)
	}
	if RawRandomLag1Written >= RawRandomSlots {
		return fmt.Errorf("the written lag %d is outside the %d slot table",
			RawRandomLag1Written, RawRandomSlots)
	}
	if RawRandomLag2 >= RawRandomSlots {
		return fmt.Errorf("the second lag %d is outside the %d slot table",
			RawRandomLag2, RawRandomSlots)
	}
	// The two lags must differ, or the recurrence would degenerate.
	if RawRandomLag1 == RawRandomLag2 {
		return fmt.Errorf("both lags are %d, so the recurrence degenerates", RawRandomLag1)
	}
	// And the returned maximum must exceed the stored maximum, which is the whole
	// reason the return is not masked.
	if RawRandomMaxReturned <= RawRandomMaxStored {
		return fmt.Errorf("the maximum return %#x does not exceed the maximum stored %#x",
			RawRandomMaxReturned, RawRandomMaxStored)
	}
	return nil
}
