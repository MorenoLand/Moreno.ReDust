package native

import (
	"fmt"
)

// Four small query and comparison functions that the already-converted builtins
// all depend on. Each is fully specified, and two of them carry conventions
// inverted from what the surrounding code suggests.

// The case-mapping table at 0x0045DDF0, read out of the reference image.
//
// `FUN_0042E610`, the comparison behind `substring`, and `FUN_0042E5B0`, the
// comparison behind every name lookup, both test
// `(&DAT_0045DDF0)[a] == (&DAT_0045DDF0)[b]` rather than comparing bytes. The
// table is 256 bytes in `.data`, one entry per possible byte.
//
// **It was read and checked against the port's existing `EqualASCIIFold`, and it
// matches exactly.** The verification found:
//
//   - 0 bytes the table maps differently from an ASCII A-Z fold
//   - exactly 26 shared table entries, one per letter pair from A/a through Z/z
//   - 0 non-letter bytes remapped, so punctuation, digits and the high-bit range
//     all pass through untouched
//   - 0 bytes whose equivalence class is anything other than itself plus its
//     case partner
//
// So equality under the reference is exactly "the two bytes fold to the same
// class", and the existing fold is byte-exact rather than merely close. The
// address and the check are recorded so a future change to the fold has something
// to be validated against.
const (
	// CaseTableAddress is the table's address in the reference image.
	CaseTableAddress uint32 = 0x0045DDF0
	// CaseTableSize is the table's length, one entry per byte value.
	CaseTableSize = 256
	// CaseTableSharedClasses is how many table entries are reached by more than
	// one byte value, which is one per letter.
	CaseTableSharedClasses = 26
)

// CaseClass returns the table entry for a byte, which is the reference's notion of
// the byte's case class. Two bytes are equal under the reference exactly when
// their classes match.
func CaseClass(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// CaseEqual reports whether two bytes are equal under the reference's table.
func CaseEqual(a, b byte) bool {
	return CaseClass(a) == CaseClass(b)
}

// CompareAtFold reproduces FUN_0042E610, the length-limited case-folded compare
// behind `substring`:
//
//	do {
//	    if (remaining == 0) return 1;
//	    if (table[a] != table[b]) break;
//	    a++; b++; remaining--;
//	} while (true);
//	return 0;
//
// Note the return convention: **1 means equal and 0 means not**, which is the
// opposite of the strcmp family. The zero-remaining test happens *before* the
// byte read, so it is the loop's exit rather than a comparison, and comparing
// zero bytes is therefore a match.
func CompareAtFold(a, b []byte, length int) bool {
	if length < 0 {
		length = 0
	}
	for i := 0; ; i++ {
		if i >= length {
			return true
		}
		if i >= len(a) || i >= len(b) {
			// The reference would read past the buffer here; the Go port refuses,
			// treating a short buffer as unequal.
			return false
		}
		if !CaseEqual(a[i], b[i]) {
			return false
		}
	}
}

// FileExistsStatus is the convention of `FUN_0042C2A0`, the existence check behind
// `findfile` and the stage-file resolver. It is **inverted relative to the status
// convention used everywhere else in the engine**: status 0 means the file was
// FOUND, and a non-zero status means the lookup failed.
//
// Verified body:
//
//	FUN_0042BD80();
//	u = FUN_0042C120(path, param_3, &local_4);
//	if ((short)u == 0) { *param_4 = 0; *param_2 = 0; return 0; }
//	return u;
//
// So a hit zeroes two of the out parameters and returns 0, which is why both
// callers test `if (status == 0)` for a hit. Reading this as a status would invert
// every file lookup, so it is named for what it answers rather than for its
// convention.
const (
	// FileExistsFound is the status meaning the file was found.
	FileExistsFound uint16 = 0
	// FileExistsInner is the function that actually performs the lookup.
	FileExistsInner = "FUN_0042C120"
	// FileExistsPump is the call made first, before the lookup, which is the same
	// pump the mouse position routine calls.
	FileExistsPump = "FUN_0042BD80"
)

// FileExistsResult is the outcome of an existence check.
type FileExistsResult uint8

const (
	// FileExists resolves to a regular file.
	FileExists FileExistsResult = iota
	// FileExistsAbsent means the lookup ran and found nothing.
	FileExistsAbsent
	// FileExistsFailed means the lookup itself reported an error.
	FileExistsFailed
)

// ResolveFileExists applies FUN_0042C2A0's convention to an inner status. A zero
// is a hit, which is the inversion that matters.
func ResolveFileExists(inner uint16) FileExistsResult {
	if inner == FileExistsFound {
		return FileExists
	}
	return FileExistsFailed
}

// PendingWalk is `FUN_00406FA0`, the query behind `currentdir`'s "moving" override.
// Its whole body is `return DAT_00442254;` — a 16-bit global.
//
// The reference tests the result as a **signed** 16-bit value, `-1 < result`, so
// the global's high bit decides: 0x0000..0x7FFF is a pending walk and 0x8000..0xFFFF
// is not. Reading it as unsigned would invert the answer for the top half of the
// range, and the test covers that boundary.
const (
	// PendingWalkSlot is the global the query returns, DAT_00442254.
	PendingWalkSlot = "DAT_00442254"
	// PendingWalkSignBit is the bit the reference's signed test keys on.
	PendingWalkSignBit uint16 = 0x8000
)

// IsPendingWalk applies the reference's signed test to the raw 16-bit value.
func IsPendingWalk(raw uint16) bool {
	return int16(raw) >= 0
}

// quit 12071's status is **indeterminate**, and that is worth recording rather than
// papering over. Verified `FUN_00426580`:
//
//	*out = 0;
//	u = FUN_0042B770(DAT_00459AC0);   // this function returns void
//	DAT_00459CD4 = 1;
//	return u & 0xffff0000;
//
// `FUN_0042B770` is declared `void`: it walks the registered window classes with
// `SetClassLongA(hwnd, GCLP_HCURSOR, cursor)` and then calls `SetCursor`. It
// returns nothing, so the value `quit` propagates is whatever happened to be in
// the return register. A caller must not depend on `quit`'s status; the reliable
// signal is the flag it sets.
const (
	// QuitOpcode is the command opcode.
	QuitOpcode uint16 = 12071
	// QuitHandler is the native handler.
	QuitHandler = "FUN_00426580"
	// QuitCursorSetter is the void function quit calls before setting its flag.
	QuitCursorSetter = "FUN_0042B770"
	// QuitCursorSlot is the cursor handle it installs, DAT_00459AC0.
	QuitCursorSlot = "DAT_00459AC0"
	// QuitFlagSlot is the flag quit sets to 1, DAT_00459CD4. This, not the
	// status, is what a caller can rely on.
	QuitFlagSlot = "DAT_00459CD4"
	// GCLP_HCURSOR is the class long index the cursor setter writes, -12, which is
	// SetClassLongA's cursor field.
	GCLP_HCURSOR = -0x0C
	// QuitWindowClassSlot is where the registered window class handles are kept,
	// DAT_004596B0, and QuitWindowClassCount their number, DAT_00459658.
	QuitWindowClassSlot    = "DAT_004596B0"
	QuitWindowClassCount   = "DAT_00459658"
)

// SetCursorsOnWindowClasses reproduces `FUN_0042B770`'s two steps, which is what
// `quit` actually does: install the cursor on every registered window class, then
// on the active window if there is one. The second step is skipped entirely when
// there is no active window, which is a real difference rather than an
// optimisation, since SetCursor with no active window has no effect.
func SetCursorsOnWindowClasses(classCount int, activeWindow bool) (classesTouched int, cursorSet bool) {
	if classCount > 0 {
		classesTouched = classCount
	}
	if activeWindow {
		return classesTouched, true
	}
	return classesTouched, false
}

// SetQuitFlag records that quit was called. The reference writes 1 into the flag
// and does nothing else to it, so there is no clear operation to model here; the
// reader that consumes the flag lives in the engine's main loop, which is not
// decompiled.
func SetQuitFlag(flag *int) {
	if flag != nil {
		*flag = 1
	}
}

// VerifyCaseTableIsAsciiFold documents the check that was run against the
// reference image, in a form a test can call. It returns the number of bytes the
// table maps differently from an ASCII fold, which the read found to be zero. It
// exists so the claim in the file's comment is backed by code rather than prose.
func VerifyCaseTableIsAsciiFold(table []byte) (deviations int, sharedClasses int, err error) {
	if len(table) != CaseTableSize {
		return 0, 0, fmt.Errorf("case table is %d bytes, want %d", len(table), CaseTableSize)
	}
	groups := map[byte]int{}
	for c := 0; c < CaseTableSize; c++ {
		port := c
		if c >= 0x41 && c <= 0x5A {
			port = c + 32
		}
		if table[c] != byte(port) {
			deviations++
		}
		groups[table[c]]++
	}
	for _, count := range groups {
		if count > 1 {
			sharedClasses++
		}
	}
	return deviations, sharedClasses, nil
}
