package scripts

// The path builder's **input convention**, its buffers, its two halves and its inlined
// copy. The fold, the drive test and the `APPL` comparison are already in `pathbuild.go`
// and are **reused here rather than restated** — that file got there first and this one
// adds only what the decompilation newly shows.

// # The input is a counted string, and this is the finding
//
//	void FUN_0042BBD0(byte *path, char *out) {
//	    bVar4 = *param_1;                        // the LENGTH
//	    pbVar9 = param_1 + 1;                    // the text starts one byte later
//	    ...
//	    memcpy(out, param_1 + 1, bVar4);         // the fall-through copies that many
//	    out[bVar4] = '\0';
//	}
//
// **Four independent places in the function agree**: the loop counter is seeded from the
// first byte, the text pointer starts at `+1`, the copy length is that first byte, and the
// terminator goes at `out[length]`. So a caller passes a byte followed by that many bytes
// of text — a Pascal-style counted string.
//
// This is worth stating plainly because the parameter is typed `byte *` and **every other
// string in this project is NUL-terminated**. A port that read `path[0]` as a drive letter
// would read a length, and the first character of every path the engine builds would be
// lost. The two callers the file layer has — the open and the seek — both pass their
// argument straight into this function, so the convention is what they hand it.
const (
	// PathLengthIsFirstByte records the convention. **The first byte is a count**, and the
	// text begins at index one.
	PathLengthIsFirstByte = true
	// PathTextOffset is where the text begins, one byte past the length.
	PathTextOffset = 1
	// PathBuilderRoutine is the function itself.
	PathBuilderRoutine = "FUN_0042BBD0"
	// The three buffer sizes, which the two-byte base offset explains. `pathbuild.go`
	// records the fold and the drive test but not these.
	//
	// PathVolumeMax is the volume buffer: six bytes, enough for a five-character volume
	// and its NUL. PathRestMax is the remainder: 256. PathBaseMax is the base directory:
	// **258, two bytes wider than the remainder**, with its text starting at offset two.
	PathVolumeMax = 6
	PathRestMax   = 256
	PathBaseMax   = 258
	// PathBaseTextOffset is where the base directory's text starts, **two** — and that is
	// why the failure path clears the buffer by writing a NUL at index *two* rather than
	// at zero. A reader looking for a `buf[0] = 0` would not find one.
	PathBaseTextOffset = 2
	// PathBaseTextMax is how much room the base's text has once the offset is taken, 256
	// — **the same as the remainder's**, which is what makes the two comparable.
	PathBaseTextMax = PathBaseMax - PathBaseTextOffset
)

// PathText returns a counted string's text and its length, reproducing what the builder
// reads. A length of zero yields an empty string, and a length that overruns the buffer is
// refused with the length reported — so a malformed path cannot read past its own data.
func PathText(counted []byte) (text string, length int, ok bool) {
	if len(counted) == 0 {
		return "", 0, false
	}
	length = int(counted[0])
	if PathTextOffset+length > len(counted) {
		return "", length, false
	}
	return string(counted[PathTextOffset : PathTextOffset+length]), length, true
}

// VerifyCountedString checks the convention as the function uses it, and the three ways a
// reader could get it wrong: reading byte zero as a character, reading the text from index
// zero, and trusting the length without a bound.
func VerifyCountedString() (ok bool, detail string) {
	if !PathLengthIsFirstByte {
		return false, "the first byte should be recorded as a length"
	}
	if PathTextOffset != 1 {
		return false, "the text should start one byte past the length"
	}
	// The accessor agrees with the convention on a well-formed input.
	text, length, ok := PathText([]byte{3, 'a', 'b', 'c'})
	if !ok || text != "abc" || length != 3 {
		return false, "a counted string should yield its text and its length"
	}
	// **The length is read from byte zero and nothing else** — a string whose first
	// character happens to be a non-zero byte would give a nonsense length if byte zero
	// were read as text instead, which is the failure this check exists to catch.
	if _, length, _ := PathText([]byte{1, 'z'}); length != 1 {
		return false, "the length should come from the first byte alone"
	}
	// A zero length is an empty path, and the accessor says so rather than reading a byte
	// that may be outside the buffer.
	if text, length, ok := PathText([]byte{0}); !ok || text != "" || length != 0 {
		return false, "a zero length should give an empty path"
	}
	// **An empty buffer has no length at all**, so it is refused rather than dereferenced.
	if _, _, ok := PathText(nil); ok {
		return false, "an empty buffer should not yield a path"
	}
	// A length that overruns is refused, **with the length still reported**, so a caller
	// can log what it was given.
	if _, length, ok := PathText([]byte{9, 'a'}); ok || length != 9 {
		return false, "an overrunning length should be refused, with the length reported"
	}
	// And the bound is exact: a length that exactly fills the rest is accepted.
	if text, _, ok := PathText([]byte{1, 'a'}); !ok || text != "a" {
		return false, "a length that exactly fits should be accepted"
	}
	// The three buffers, and the arithmetic that connects the base's width to its offset.
	if PathVolumeMax != 6 {
		return false, "the volume buffer should be six bytes"
	}
	if PathRestMax != 256 {
		return false, "the remainder buffer should be 256 bytes"
	}
	if PathBaseMax != 258 {
		return false, "the base buffer should be 258 bytes"
	}
	if PathBaseMax-PathRestMax != PathBaseTextOffset {
		return false, "the base should be two bytes wider than the remainder, by its offset"
	}
	if PathBaseTextOffset != 2 {
		return false, "the base's text should start at offset two"
	}
	// **And its text has the same room as the remainder's**, which is the reason for the
	// two extra bytes rather than an accident of the layout.
	if PathBaseTextMax != PathRestMax {
		return false, "the base's text should have the same room as the remainder's"
	}
	return true, "a length byte, then that many bytes of text"
}

// # The two halves, and the asymmetry between them
//
// After the volume, the code tests:
//
//	if (remainder != 0 && (length - consumed) != PathDriveIndex) { ... }
//
// `length - consumed` is the count of characters **before** the colon, and
// `PathDriveIndex` is 2 — the byte position the colon must sit at for a drive prefix,
// already recorded in `pathbuild.go`. So the fall-through is taken when there is no
// remainder, or when the volume part is exactly the two bytes of a drive letter. And the
// fall-through is a plain `memcpy` of the text.
//
// # The asymmetry is the finding
//
// **Whether a path gets upper-cased depends on whether it has a volume part at all.** The
// colon path folds the remainder and turns its colons into backslashes; the fall-through
// copies byte for byte with no folding anywhere on it. So a port that folded
// unconditionally would change the behaviour of every path that reaches the fall-through
// — which is the no-colon case, the simplest one, and therefore the easiest to overlook.
//
// The one-letter drive is the case `pathbuild.go` already calls out; what is new here is
// that it shares its path with the named-volume case, and both differ from the fall-through
// in the same way.
const (
	// PathFallThroughIsVerbatim records that the no-colon and drive cases copy the text
	// unchanged — no folding, no colon replacement.
	PathFallThroughIsVerbatim = true
	// PathFoldedPathsHaveVolume records the other half: **only a path with a volume part
	// is folded**, so the fold's reach is conditional.
	PathFoldedPathsHaveVolume = true
)

// PathVolumeAndRest splits a path's text at the first colon, folding the volume with the
// builder's inline fold. The remainder is returned **verbatim** — the folding of the
// remainder happens in a later pass and only on the colon path, so this function does not
// do it, and doing it here would be the error this split of behaviour is recording.
func PathVolumeAndRest(text string) (volume string, rest string, hadColon bool) {
	for i := 0; i < len(text); i++ {
		if text[i] == PathColon {
			return foldVolumeInline(text[:i]), text[i+1:], true
		}
	}
	return "", text, false
}

// foldVolumeInline upper-cases a run with the builder's own inline fold, reusing the
// bounds `pathbuild.go` records rather than restating them.
func foldVolumeInline(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c > PathLowerBound && c < PathUpperBound {
			c -= PathCaseFoldDelta
		}
		out[i] = c
	}
	return string(out)
}

// VerifyPathTwoHalves checks the split, and in particular that **the remainder comes back
// unfolded** and that a no-colon path does not split at all.
func VerifyPathTwoHalves() (ok bool, detail string) {
	// A path with a colon splits, and the volume comes back folded. **A slash is not a
	// colon** — the separator only appears after a volume, so a slash-only path has no
	// volume part and takes the fall-through.
	volume, rest, hadColon := PathVolumeAndRest("dust:data/x.snd")
	if !hadColon {
		return false, "a path with a colon should report one"
	}
	if volume != "DUST" {
		return false, "the volume should come back upper-cased"
	}
	// **The remainder comes back verbatim** — the folding happens later, on this path only.
	if rest != "data/x.snd" {
		return false, "the remainder should be returned unfolded"
	}
	if !PathFallThroughIsVerbatim {
		return false, "the fall-through should be recorded as verbatim"
	}
	// And a path with no colon does not split at all — the fall-through.
	volume, rest, hadColon = PathVolumeAndRest("data/x.snd")
	if hadColon || volume != "" || rest != "data/x.snd" {
		return false, "a path with no colon should not split"
	}
	// So the fold's reach is conditional, which is the asymmetry worth asserting: the same
	// text, with and without a volume, is treated differently.
	if !PathFoldedPathsHaveVolume {
		return false, "only a path with a volume part should be folded"
	}
	if _, folded, _ := PathVolumeAndRest("DUST:x"); folded != "x" {
		return false, "the remainder of a folded path should still come back verbatim"
	}
	// A one-letter drive is the drive case, and its volume is one character.
	volume, rest, hadColon = PathVolumeAndRest("C:\\dust\\data")
	if !hadColon || volume != "C" || rest != "\\dust\\data" {
		return false, "a one-letter drive should split with a one-character volume"
	}
	// The volume part before the colon is one character, and the code's test is on the
	// *byte position* of the colon, which is the same thing.
	if 1+1 != PathDriveIndex {
		return false, "a one-character volume plus its colon should sit at the drive index"
	}
	// The `APPL` comparison's five bytes and the volume buffer's six agree, and both come
	// from `pathbuild.go` — so the two files cannot disagree about them.
	if PathAppleCompareLength != 5 {
		return false, "the APPL comparison should read five bytes"
	}
	if PathAppleCompareLength != len(PathAppleVolume)+1 {
		return false, "the comparison should read the name and its NUL"
	}
	if PathAppleCompareLength != PathVolumeMax-1 {
		return false, "the comparison length and the volume buffer should agree"
	}
	return true, "a verbatim fall-through, and a fold that reaches only paths with a volume"
}

// # The inlined copy, which appears four times in this one function
//
// The builder finishes by appending the remainder to the base directory, using the
// compiler's inlined `memcpy`/`strcat`:
//
//	for (i = length >> 2; i != 0; i--) *(uint32 *)dst = *(uint32 *)src, src += 4, dst += 4;
//	for (i = length & 3;  i != 0; i--) *dst++ = *src++;
//
// **Words first, then the byte tail** — the same shape in all four places. It appears in
// the trailing-backslash append and again in the final assembly, so a function this small
// contains four copies of one loop pair.
//
// Two things are worth recording. The length is spent as the word loop takes the multiples
// of four and the byte loop takes `length & 3`, which is the whole of the arithmetic a port
// would reimplement. And **the copy runs through `uint32` pointers without regard for
// alignment**, so a port must not assume the source and destination are word-aligned — a
// Go `copy` would be a faithful port, and a hand-rolled word loop on `[]byte` would not be
// safe to write that way.
const (
	// PathCopyWordBytes is how much each word iteration moves, four.
	PathCopyWordBytes = 4
	// PathCopyShift is the shift that yields the word count, two.
	PathCopyShift = 2
	// PathCopySites is how many times the pattern appears in this one function.
	PathCopySites = 4
	// PathCopyIgnoresAlignment records that the reference copies words without an
	// alignment guarantee, so a port should use a byte copy.
	PathCopyIgnoresAlignment = true
)

// PathCopyPlan gives how a length is split between the two loops, which is the whole of
// the reference's arithmetic.
func PathCopyPlan(length int) (words int, tail int) {
	return length >> PathCopyShift, length & (PathCopyWordBytes - 1)
}

// VerifyInlinedCopy checks the copy's shape as a property, and that the two loops between
// them cover the length exactly and no byte twice.
func VerifyInlinedCopy() (ok bool, detail string) {
	if PathCopyWordBytes != 4 {
		return false, "the copy should move four bytes per word iteration"
	}
	if PathCopyShift != 2 {
		return false, "the word count should come from a shift of two"
	}
	// **The tail is the length modulo the word size**, so together the two loops cover the
	// length exactly. Checked across a range that crosses a word boundary twice.
	for length := 0; length <= 16; length++ {
		words, tail := PathCopyPlan(length)
		if words*PathCopyWordBytes+tail != length {
			return false, "the two loops should cover the length exactly"
		}
		if words != length/PathCopyWordBytes || tail != length%PathCopyWordBytes {
			return false, "the split should be a division and its remainder"
		}
		if tail >= PathCopyWordBytes {
			return false, "the tail should be less than a word"
		}
	}
	// The cases around a word boundary, which is where a wrong shift would show. The pairs
	// are `{length, words, tail}`, and the length is read from the slice rather than
	// indexed with a literal, because a constant index into a short literal slice is a
	// compile-time error.
	for _, want := range [][3]int{
		{0, 0, 0}, {1, 0, 1}, {4, 1, 0}, {5, 1, 1}, {7, 1, 3}, {8, 2, 0}, {9, 2, 1},
	} {
		words, tail := PathCopyPlan(want[0])
		if words != want[1] || tail != want[2] {
			return false, "the split at a word boundary should be exact"
		}
	}
	// The four sites, and the alignment note.
	if PathCopySites != 4 {
		return false, "the pattern should appear four times in the builder"
	}
	if !PathCopyIgnoresAlignment {
		return false, "the reference copies words without an alignment guarantee"
	}
	return true, "words then a byte tail, four bytes at a time, four times over"
}
