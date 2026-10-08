package scripts

import "fmt"

// Stage file name validation, transcribed from FUN_004178C0, the resolver
// openstagefile 12060 calls before it installs a stage. The function validates
// the name, then searches for it under up to eight prefixes, so the validation
// half is a complete, self-contained contract and is what is converted here.
//
// Verified, in the order the reference performs it:
//
//	length := *name
//	if length < 3 || length > 0x0C          -> 0x34
//	if name[1] < 0x41 || name[1] > 0x7A     -> 0x34   // must be 'A'..'z'
//	if name[1] > 0x5A && name[1] < 0x61     -> 0x34   // and not '['..'`'
//	scan i in 1..length:
//	    if name[i] == '.':
//	        if extensionSeen                  -> 0x34   // at most one dot
//	        if i > 9                          -> 0x34   // dot index <= 9
//	        ext := length - i
//	        if ext > 3                        -> 0x34   // extension 1..3 bytes
//	        if ext < 1                        -> 0x34
//	        extensionSeen = true
//
// Note the first-character test is a single range A..z with the five characters
// '[', '\', ']', '^', '_' and '`' carved out of the middle of it, which is
// exactly the set of characters DOS disallows in a stem. It is reproduced as
// written rather than as a "is alphanumeric" test, because the accepted range
// also includes the six punctuation characters between 'Z' and 'a' other than
// those five: '[', '\', ']', '^', '_' and '`' are excluded, so the survivors in
// that gap are none, but the range itself admits nothing below 'A' or above 'z'.
const (
	// StageNameStatusMalformed is native status 0x34, returned by every
	// validation failure. It is the most common status in the function.
	StageNameStatusMalformed uint16 = 0x34
	// StageNameStatusExhausted is native status 0x2A, returned when all eight
	// prefix candidates have been tried without a hit.
	StageNameStatusExhausted uint16 = 0x2A
	// StageNameStatusNotAFile is native status 0x31, returned when a directory
	// entry matched by name is not a regular file.
	StageNameStatusNotAFile uint16 = 0x31
	// StageNameStatusNoExtension is native status 0x34's sibling for a name
	// with no dot: the reference keeps searching the extensionless branch, so a
	// name with no extension is not rejected here.
	StageNameStatusNoExtension uint16 = 0
)

// Stage file name bounds, verified.
const (
	StageNameMinLength = 3
	StageNameMaxLength = 0x0C
	// StageNameMaxDotIndex is the largest index a '.' may appear at.
	StageNameMaxDotIndex = 9
	// StageNameMinExtension and StageNameMaxExtension bound the bytes after
	// the dot.
	StageNameMinExtension = 1
	StageNameMaxExtension = 3
	// StageNamePrefixCandidates is the number of prefixes FUN_004178C0 tries
	// before giving up with 0x2A.
	StageNamePrefixCandidates = 8
	// StageNameFirstLo and StageNameFirstHi bound the first character.
	StageNameFirstLo = 0x41 // 'A'
	StageNameFirstHi = 0x7A // 'z'
	// StageNameGapLo and StageNameGapHi bound the characters carved out of
	// the middle of that range: '[' '\' ']' '^' '_' '`'.
	StageNameGapLo = 0x5B
	StageNameGapHi = 0x60
)

// StageNameRejection explains why a stage name was refused, so a caller can
// report something better than a bare status.
type StageNameRejection uint8

const (
	// StageNameAccepted means the name passed validation.
	StageNameAccepted StageNameRejection = iota
	// StageNameRejectLength covers a length outside 3..12.
	StageNameRejectLength
	// StageNameRejectFirstChar covers a first character outside the accepted
	// range, including the carved-out gap.
	StageNameRejectFirstChar
	// StageNameRejectMultipleDots covers a second '.'.
	StageNameRejectMultipleDots
	// StageNameRejectDotTooLate covers a '.' past index 9.
	StageNameRejectDotTooLate
	// StageNameRejectExtension covers an extension outside 1..3 bytes.
	StageNameRejectExtension
)

// ValidateStageName reproduces the validation half of FUN_004178C0. It reports
// the native status the reference would return and, separately, which rule
// refused the name. The two are returned together because the reference
// collapses every validation failure onto the single status 0x34, so the status
// alone cannot tell a caller what was wrong with a name.
func ValidateStageName(name string) (status uint16, reason StageNameRejection, err error) {
	// The reference reads the length byte of a Pascal string, so a name longer
	// than 255 could never arrive; anything longer here is over the maximum
	// anyway.
	length := len(name)
	if length < StageNameMinLength || length > StageNameMaxLength {
		return StageNameStatusMalformed, StageNameRejectLength,
			fmt.Errorf("stage name %q is %d bytes, want %d..%d", name, length, StageNameMinLength, StageNameMaxLength)
	}
	first := name[0]
	if first < StageNameFirstLo || first > StageNameFirstHi {
		return StageNameStatusMalformed, StageNameRejectFirstChar,
			fmt.Errorf("stage name %q starts with %q, want %q..%q", name, first, rune(StageNameFirstLo), rune(StageNameFirstHi))
	}
	if first > StageNameGapLo-1 && first < StageNameGapHi+1 {
		// This is the `0x5A < c && c < 0x61` carve-out, which excludes
		// '[' '\' ']' '^' '_' '`' from inside the A..z range.
		return StageNameStatusMalformed, StageNameRejectFirstChar,
			fmt.Errorf("stage name %q starts with %q, which is excluded from the accepted range", name, rune(first))
	}
	extensionSeen := false
	// The reference indexes param_1[sVar3] with sVar3 starting at 1, because
	// param_1[0] is the length byte. Its "dot index" and its extension length
	// are therefore both measured in 1-based character positions, so the
	// extension is length - dotPosition and not length - zeroBasedIndex. Getting
	// this off by one silently accepts a trailing dot and rejects a 3 byte
	// extension, so the position is computed explicitly rather than inlined.
	for index := 1; index < length; index++ {
		if name[index] != '.' {
			continue
		}
		// param_1[sVar3] with sVar3 starting at 1 addresses the first character,
		// because param_1[0] holds the length. So the reference's dot position
		// is the zero-based character index plus one.
		dotPosition := index + 1
		if extensionSeen {
			return StageNameStatusMalformed, StageNameRejectMultipleDots,
				fmt.Errorf("stage name %q has more than one dot", name)
		}
		if dotPosition > StageNameMaxDotIndex {
			return StageNameStatusMalformed, StageNameRejectDotTooLate,
				fmt.Errorf("stage name %q has its dot at index %d, past the limit of %d", name, dotPosition, StageNameMaxDotIndex)
		}
		extension := length - dotPosition
		if extension > StageNameMaxExtension {
			return StageNameStatusMalformed, StageNameRejectExtension,
				fmt.Errorf("stage name %q has a %d byte extension, want %d..%d",
					name, extension, StageNameMinExtension, StageNameMaxExtension)
		}
		if extension < StageNameMinExtension {
			return StageNameStatusMalformed, StageNameRejectExtension,
				fmt.Errorf("stage name %q has a %d byte extension, want %d..%d",
					name, extension, StageNameMinExtension, StageNameMaxExtension)
		}
		extensionSeen = true
	}
	return 0, StageNameAccepted, nil
}

// HasStageExtension reports whether a validated name carries a dot. The
// reference branches on this: an extensionless name is searched directly,
// while a name with an extension is tried against each of the eight prefixes.
func HasStageExtension(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			return true
		}
	}
	return false
}

// Stage resolution outcome, mirroring the tail of FUN_004178C0.
type StageResolution uint8

const (
	// StageResolved means the name validated and a candidate matched a file.
	StageResolved StageResolution = iota
	// StageUnvalidated means the name was never searched because validation
	// failed.
	StageUnvalidated
	// StagePrefixesExhausted means all eight candidates were tried with no hit.
	StagePrefixesExhausted
	// StageNotAFile means a directory entry matched but was not a regular file.
	StageNotAFile
)

// StageSearch describes the search the reference performs after validation.
type StageSearch struct {
	// Extension reports whether the name carried a dot, which selects the two
	// branches: a name with an extension is prefixed and tried up to eight
	// times, a name without one is looked up directly.
	Extension bool
	// Candidates is how many prefixed names to try. It is exactly
	// StageNamePrefixCandidates for a name with an extension and 1 otherwise.
	Candidates int
	// PrefixOf returns the prefix candidate to prepend for a given 1-based
	// index, or ok=false when the index is out of range. The reference builds
	// each candidate with FUN_00417820, so the prefix text is not a constant
	// and is supplied by the caller.
	PrefixOf func(index int) (prefix string, ok bool)
	// Exists reports whether a fully qualified candidate names an existing
	// entry, and whether that entry is a regular file. A hit on a directory or
	// other non-file is the 0x31 case, which the reference distinguishes from
	// a miss.
	Exists func(candidate string) (isRegularFile bool, found bool)
}

// ResolveStageFile reproduces the search half of FUN_004178C0: validate, then
// try each candidate, distinguishing a miss, an exhausted search and a
// non-regular file.
func ResolveStageFile(name string, search StageSearch) (StageResolution, uint16, error) {
	status, _, err := ValidateStageName(name)
	if err != nil {
		return StageUnvalidated, status, err
	}
	candidates := search.Candidates
	if search.Extension {
		if candidates == 0 {
			// The reference always tries exactly eight, so an unset count
			// means the verified eight rather than none.
			candidates = StageNamePrefixCandidates
		}
	} else {
		// The extensionless branch looks the name up directly.
		candidates = 1
	}
	if search.PrefixOf == nil || search.Exists == nil {
		return StageUnvalidated, 0, fmt.Errorf("stage name %q validated but no search was supplied", name)
	}
	var lastNotAFile uint16
	for i := 1; i <= candidates; i++ {
		prefix, ok := search.PrefixOf(i)
		if !ok {
			break
		}
		candidate := prefix + name
		isFile, found := search.Exists(candidate)
		if !found {
			continue
		}
		if !isFile {
			// The reference keeps the entry but reports 0x31; it does not try
			// the remaining candidates, so this ends the search.
			lastNotAFile = StageNameStatusNotAFile
			break
		}
		return StageResolved, 0, nil
	}
	if lastNotAFile != 0 {
		return StageNotAFile, lastNotAFile,
			fmt.Errorf("stage name %q matched an entry that is not a regular file", name)
	}
	if search.Extension && candidates >= StageNamePrefixCandidates {
		return StagePrefixesExhausted, StageNameStatusExhausted,
			fmt.Errorf("stage name %q matched none of the %d candidate prefixes", name, StageNamePrefixCandidates)
	}
	return StagePrefixesExhausted, StageNameStatusExhausted,
		fmt.Errorf("stage name %q was not found", name)
}

// Stage header geometry, verified from FUN_00411900, the routine that installs
// an opened stage. It reads the loaded stage header and refuses anything that
// is not exactly the expected size:
//
//	*(short *)(header + 0x1C) != 0x200 -> FUN_0042C470(0, 0x1132)
//	*(short *)(header + 0x1E) != 0x180 -> FUN_0042C470(0, 0x1133)
//	name = PascalCopy(header + 0x824)
//
// So a stage's playfield is fixed at 0x200 by 0x180, 512 by 384, and these are
// hardcoded expectations rather than values read from the file. A stage of any
// other size is a native diagnostic, not a tolerated variant, so the Go port
// rejects it too rather than accepting a different geometry.
const (
	// StageHeaderWidth is the required width word at header offset 0x1C.
	StageHeaderWidth uint16 = 0x200
	// StageHeaderHeight is the required height word at header offset 0x1E.
	StageHeaderHeight uint16 = 0x180
	// StageHeaderWidthOffset and StageHeaderHeightOffset are where those words
	// are read from.
	StageHeaderWidthOffset  = 0x1C
	StageHeaderHeightOffset = 0x1E
	// StageHeaderDataOffset is the word copied into the stage record at +8; the
	// reference takes the DWORD at header offset 0x20.
	StageHeaderDataOffset = 0x20
	// StageHeaderNameOffset is where the stage's Pascal name record lives.
	StageHeaderNameOffset = 0x824
	// The two diagnostic arguments raised on a geometry mismatch.
	StageHeaderWidthDiag  uint16 = 0x1132
	StageHeaderHeightDiag uint16 = 0x1133
	// The diagnostic raised when the stage file cannot be loaded, and when its
	// header cannot be fetched.
	StageLoadFailedDiag    uint16 = 0x1130
	StageHeaderFailedDiag  uint16 = 0x1131
)

// StagePlayfieldWidth and StagePlayfieldHeight are the fixed playfield
// dimensions in pixels.
const (
	StagePlayfieldWidth  = int(StageHeaderWidth)
	StagePlayfieldHeight = int(StageHeaderHeight)
)

// CheckStageHeaderGeometry validates the two size words of a loaded stage
// header, reproducing the two checks in FUN_00411900 including their distinct
// diagnostic arguments. offset is the byte offset within the header, so a
// caller passes the loaded header and the field it read.
func CheckStageHeaderGeometry(widthWord, heightWord uint16) error {
	if widthWord != StageHeaderWidth {
		return fmt.Errorf("stage header width word %#x at +%#x, want %#x (FUN_0042C470(0, %#x))",
			widthWord, StageHeaderWidthOffset, StageHeaderWidth, StageHeaderWidthDiag)
	}
	if heightWord != StageHeaderHeight {
		return fmt.Errorf("stage header height word %#x at +%#x, want %#x (FUN_0042C470(0, %#x))",
			heightWord, StageHeaderHeightOffset, StageHeaderHeight, StageHeaderHeightDiag)
	}
	return nil
}

// StageNameFromHeader extracts the stage's Pascal name from a loaded header at
// the verified offset, reusing the FUN_0042E6C0 copy the reference performs.
func StageNameFromHeader(header []byte) (string, error) {
	if len(header) < StageHeaderNameOffset+1 {
		return "", fmt.Errorf("stage header is %d bytes, too short for a name at +%#x", len(header), StageHeaderNameOffset)
	}
	return PascalTextOf(PascalCopy(header[StageHeaderNameOffset:])), nil
}

// PascalTextOf renders a Pascal buffer as text, tolerating a missing length
// byte. It is the read side used by the header name extraction.
func PascalTextOf(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	length := int(value[0])
	if 1+length > len(value) {
		length = len(value) - 1
	}
	if length <= 0 {
		return ""
	}
	return string(value[1 : 1+length])
}
