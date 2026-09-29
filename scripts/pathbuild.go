package scripts

import "strings"

// The path builder `FUN_0042BBD0`, **partially mapped**. Its verified prefix is
// transcribed below; the remainder — what it does after resolving the current directory
// — is **not** decompiled here and is flagged as such rather than guessed.
//
// The prefix is worth having on its own because it identifies the function's purpose,
// which was not obvious from the name. It is a **Macintosh volume compatibility shim**.
//
// Verified, from `FUN_0042BBD0`'s opening:
//
//	length = *param_1;                        // a Pascal string: a length byte, then text
//	remaining = length;
//	while (remaining != 0) {
//	    c = *next++; remaining--;
//	    if (0x60 < c && c < 0x7B) c -= 0x20;  // upper-case a..z
//	    if (c == 0x3A) break;                 // stop at the colon
//	    store(c);
//	}
//	if (remaining != 0 && (length - remaining) != 2) {
//	    // Not a bare two-character drive: take the rest as a second component,
//	    // replacing any colon with a backslash.
//	    ...
//	    compare the first five bytes of the first component against &DAT_0045DD94
//	    if equal {
//	        FUN_0042BB70(0xFF, buffer);
//	        FUN_00441668(buffer);              // the current directory
//	        ... scan backwards for &DAT_0045DD90 ...
//	    }
//	}
//
// **The colon must sit at index exactly 2 to be treated as a drive.** The test is
// `length - remaining != 2`, where `remaining` counts down to the colon — so a drive
// prefix is one letter and one separator character, then the colon. Anything else is read
// as a second path component instead.
//
// **And the special-cased volume is `APPL`.** `DAT_0045DD94` holds the four bytes
// `41 50 50 4C` — `APPL` — followed by a NUL, and the comparison is **five bytes**, so
// the first component must be *exactly* `APPL` and nothing else. `DAT_0045DD90` is a
// single backslash, which is the terminator the backward scan looks for. The same region
// holds the format string `%c:\` at `0x0045DD88`, which is how a one-letter drive is
// spelled.
//
// So a script that writes `APPL:SOMEFILE` is translated onto a Windows path, and any
// other volume prefix is not. That is the Macintosh edition's path convention surviving
// into the Windows build, and it is the reason the function exists at all.
const (
	// PathColon is the character the prefix scan stops at, 0x3A.
	PathColon = 0x3A
	// PathBackslash is 0x5C, which a colon becomes inside a path component.
	PathBackslash = 0x5C
	// PathLowerBound is the exclusive lower bound of the upper-casing range, 0x60.
	// It is one below 'a', so the test is `0x60 < c`.
	PathLowerBound = 0x60
	// PathUpperBound is the exclusive upper bound, 0x7B, one above 'z'.
	PathUpperBound = 0x7B
	// PathCaseFoldDelta is what a lower-case letter is reduced by, 0x20, which is the
	// ASCII difference between a letter and its upper-case form.
	PathCaseFoldDelta = 0x20
	// PathDriveIndex is the index the colon must sit at for a drive prefix, 2. The
	// reference computes it as `length - remaining`.
	PathDriveIndex = 2
	// PathAppleVolume is the volume the function special-cases, "APPL".
	PathAppleVolume = "APPL"
	// PathAppleVolumeAddress is where those four bytes live.
	PathAppleVolumeAddress uint32 = 0x0045DD94
	// PathAppleCompareLength is how many bytes the comparison covers, **five** — the
	// four letters and the NUL terminator. That is what makes the match exact rather
	// than a prefix: a volume called `APPLE` would fail on the fifth byte.
	PathAppleCompareLength = 5
	// PathBackslashAddress is the single-backslash string the backward scan uses.
	PathBackslashAddress uint32 = 0x0045DD90
	// PathDriveFormatAddress is the format string "%c:\", which spells a one-letter
	// drive.
	PathDriveFormatAddress uint32 = 0x0045DD88
	// PathBufferMax is the scan buffer the function uses, 258. It is one more than
	// MAX_PATH's 260 is not; the reference declares a 258-byte C string beside a
	// 256-byte one, so the two are deliberately different sizes.
	PathBufferMax = 258
	// PathSecondaryBuffer is the second buffer, 256.
	PathSecondaryBuffer = 256
)

// PathPrefix is what the verified prefix of the function determines about an input.
//
// **The two flags are mutually exclusive, and that is the reference's own structure.**
// The colon test is `length - remaining != 2`, and the `APPL` comparison sits *inside*
// that branch. So a colon at the second character is a native Windows drive and skips
// the whole Macintosh path, while any other colon position enters the branch and is
// tested against `APPL`. An input can therefore be one or the other, never both.
type PathPrefix struct {
	// Volume is the text before the colon, upper-cased. It is empty when the input has
	// no colon at all.
	Volume string
	// Rest is the text after the colon.
	Rest string
	// IsDrive is true only when the colon is the **second character**, so a bare
	// one-letter volume such as `C:`. This is the reference's `length - remaining == 2`.
	IsDrive bool
	// IsAppleVolume is true when the colon is *not* at that position and the volume is
	// exactly "APPL", which is the Macintosh form the function exists to translate.
	IsAppleVolume bool
}

// SplitPathPrefix reproduces the verified prefix of FUN_0042BBD0: it upper-cases the
// text, splits at the first colon, and decides which of the two forms it is.
//
// The colon index is the load-bearing part, and it is **1-based**: the reference
// computes `length - remaining`, and `remaining` has already been decremented past the
// colon, so the result is the one-based position of the colon. A colon at zero-based
// index 1 — the second character — is the drive form.
func SplitPathPrefix(input string) PathPrefix {
	// The reference reads a Pascal string, so the text is already length-delimited; a
	// Go string is treated the same way and an empty one has no prefix at all.
	if input == "" {
		return PathPrefix{}
	}
	// Upper-case as the reference does, over the exact range it uses, and stop at the
	// first colon.
	var volume strings.Builder
	colonIndex := -1
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c > PathLowerBound && c < PathUpperBound {
			c -= PathCaseFoldDelta
		}
		if c == PathColon {
			colonIndex = i
			break
		}
		volume.WriteByte(c)
	}
	if colonIndex < 0 {
		// No colon: the whole thing is one component and there is no volume.
		return PathPrefix{Rest: UpperPathCase(input)}
	}
	prefix := PathPrefix{
		Volume: volume.String(),
		Rest:   UpperPathCase(input[colonIndex+1:]),
	}
	// **The colon must be the second character.** The reference's index is one-based,
	// because the count is decremented past the colon before the test runs.
	prefix.IsDrive = colonIndex+1 == PathDriveIndex
	// And the Macintosh case is the *other* branch: not a drive, and the volume is
	// exactly the four letters `APPL`.
	prefix.IsAppleVolume = !prefix.IsDrive && IsAppleVolume(prefix.Volume)
	return prefix
}

// UpperPathCase reproduces the reference's fold exactly: it upper-cases only bytes in
// the open range `0x60 < c < 0x7B`, which covers `a`..`z` and nothing else. Digits,
// punctuation and the already-upper-case letters pass through untouched, and so does the
// NUL.
//
// The range is one wider than the letters on each side, so `0x60` (the backtick) and
// `0x7B` (the brace) are **excluded** by the strict comparisons. That is the reference's
// test transcribed rather than a call to a general fold function, and a general fold
// would differ on exactly those two bytes.
func UpperPathCase(input string) string {
	out := make([]byte, len(input))
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c > PathLowerBound && c < PathUpperBound {
			c -= PathCaseFoldDelta
		}
		out[i] = c
	}
	return string(out)
}

// ConvertComponentColons reproduces the second half of the prefix scan: inside a path
// component, a colon becomes a backslash. The reference does this only on the
// non-drive branch, and it does it unconditionally rather than only at the first colon.
func ConvertComponentColons(input string) string {
	out := make([]byte, len(input))
	for i := 0; i < len(input); i++ {
		if input[i] == PathColon {
			out[i] = PathBackslash
		} else {
			out[i] = input[i]
		}
	}
	return string(out)
}

// IsAppleVolume reports whether a volume name is the special-cased one, using the
// reference's **exact** five-byte comparison rather than a prefix test. A volume called
// `APPLE` or `AP` is not `APPL`, and the fifth byte is what says so.
func IsAppleVolume(volume string) bool {
	if len(volume) != len(PathAppleVolume) {
		return false
	}
	// The reference compares five bytes: the four letters and the NUL that terminates
	// them. A Go string of length four has no such byte, so the length check stands in
	// for it — which is the same test, since a longer volume would compare unequal at
	// its fifth byte.
	return volume == PathAppleVolume
}

// PathBuildUnmapped records what is **not** converted, so a reader does not assume the
// whole function is here. The prefix scan is transcribed; the rest — what the reference
// does with the resolved current directory, how it assembles the final path, and what
// it returns — is not.
const PathBuildUnmapped = "FUN_0042BBD0's current-directory resolution, path assembly and " +
	"return value are not decompiled here; only its prefix scan is transcribed"
