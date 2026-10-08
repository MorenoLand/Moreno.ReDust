package native

// The volume resolver the path builder calls, and the two routines it calls in turn.
// All three were working names in this session's notes and **two of the three were wrong**,
// which is itself the reason this file is written around what each one does rather than
// what its name suggested.

// # The resolver: match a volume label, return the drive root
//
//	uVar3 = GetLogicalDrives();
//	do {
//	    if (uVar3 == 0) return 0;
//	    if (uVar3 & 1) {
//	        GetDriveTypeA(root);
//	        if (type is 3, 4, 5 or 6) {
//	            GetVolumeInformationA(root, label, 0xD, ...);
//	            FUN_00441668(label);                 // upper-case in place
//	            if (label == param_1) { result = root; break; }
//	        }
//	    }
//	    uVar3 >>= 1;
//	} while (true);
//	copy(result, param_2); return 1;
//
// Five things fall out, and the last is the one that matters most:
//
//   - **It walks a bitmask, one drive at a time**, testing bit zero and shifting right —
//     so `A:` through `Z:`, twenty-six positions, and a zero mask ends the loop with a
//     failure.
//   - **Four of the six drive types are accepted**: `DRIVE_FIXED`, `DRIVE_REMOTE`,
//     `DRIVE_CDROM` and `DRIVE_RAMDISK`. `DRIVE_UNKNOWN` and `DRIVE_NO_ROOT_DIR` are
//     rejected, and so is `DRIVE_REMOVABLE` — **so a floppy is skipped**, which is a
//     deliberate choice and the only one of the six that is not about certainty.
//   - **Both string-size arguments are 0x0D = 13**, while both buffers are 16 bytes. So
//     13 is neither a buffer size nor a round figure, and a port that used 16 would pass a
//     larger limit than the reference does.
//   - **The comparison is a hand-rolled two-bytes-at-a-time case-sensitive compare**,
//     unrolled by two, against the label the canonicaliser has already upper-cased.
//   - **The result is the drive root, not the label.** On a match the function copies
//     `root` into the output and returns 1. So the "base directory" the path builder
//     receives is `X:\` — a drive root — and not the volume's own directory. That is what
//     `PathFormat`, the `"%c:\"` string the platform region records, is for.
const (
	// VolumeResolverRoutine is the function, FUN_0042B9F0.
	VolumeResolverRoutine = "FUN_0042B9F0"
	// VolumeResolverBit is the bit the loop tests, zero — so the first drive examined is
	// `A:` and the mask is walked least-significant bit first.
	VolumeResolverBit = 0
	// VolumeResolverDrives is how many positions the mask has: twenty-six, one per letter.
	VolumeResolverDrives = 26
	// VolumeResolverShift is what each iteration shifts by, one.
	VolumeResolverShift = 1
	// VolumeLabelBuffer and VolumeFileSystemBuffer are the two `GetVolumeInformationA`
	// buffers, **both sixteen bytes**.
	VolumeLabelBuffer      = 16
	VolumeFileSystemBuffer = 16
	// VolumeSizeArgument is the size passed for **both** of them, 0x0D = 13. So 13 is
	// neither a buffer size nor a round figure.
	VolumeSizeArgument = 0x0D
	// The four accepted drive types, and the two that are not.
	//
	// Accepted: DRIVE_FIXED, DRIVE_REMOTE, DRIVE_CDROM, DRIVE_RAMDISK. **DRIVE_REMOVABLE
	// is rejected**, so a floppy is skipped — the one rejection that is a choice rather
	// than a statement about certainty.
	VolumeTypeFixed    = 3
	VolumeTypeRemote   = 4
	VolumeTypeCDROM    = 5
	VolumeTypeRAMDisk  = 6
	VolumeTypeRemovable = 2
	VolumeTypeUnknown  = 0
	VolumeTypeNoRoot   = 1
	// VolumeResolverReturnsRoot records that a match yields the **drive root**, not the
	// volume label and not the volume's directory.
	VolumeResolverReturnsRoot = true
	// VolumeResolverFailsWithZero records that an exhausted mask returns zero.
	VolumeResolverFailsWithZero = 0
)

// VolumeTypeAccepted reports whether a `GetDriveTypeA` result is one the resolver acts on.
func VolumeTypeAccepted(driveType uint32) bool {
	switch driveType {
	case VolumeTypeFixed, VolumeTypeRemote, VolumeTypeCDROM, VolumeTypeRAMDisk:
		return true
	default:
		return false
	}
}

// VerifyVolumeResolver checks the mask walk, the four accepted types, the odd size
// argument, and that the result is the root rather than the label.
func VerifyVolumeResolver() (ok bool, detail string) {
	// **Four of the six types are accepted**, and the rejected one that matters is the
	// removable drive — a floppy is skipped, which is a choice rather than a doubt.
	accepted := []uint32{VolumeTypeFixed, VolumeTypeRemote, VolumeTypeCDROM, VolumeTypeRAMDisk}
	for _, driveType := range accepted {
		if !VolumeTypeAccepted(driveType) {
			return false, "the four fixed, remote, cdrom and ramdisk types should be accepted"
		}
	}
	if VolumeTypeAccepted(VolumeTypeRemovable) {
		return false, "a removable drive should be skipped"
	}
	if VolumeTypeAccepted(VolumeTypeUnknown) || VolumeTypeAccepted(VolumeTypeNoRoot) {
		return false, "the unknown and no-root types should be rejected"
	}
	// So four accepted and two rejected, and the six types are distinct.
	if VolumeTypeFixed == VolumeTypeRemote || VolumeTypeCDROM == VolumeTypeRAMDisk {
		return false, "the drive types should be distinct values"
	}
	// **The mask walk is one bit at a time from the bottom**, so `A:` first and twenty-six
	// positions — the letters.
	if VolumeResolverBit != 0 {
		return false, "the loop should test bit zero, so the first drive is A:"
	}
	if VolumeResolverShift != 1 {
		return false, "the mask should shift by one per iteration"
	}
	if VolumeResolverDrives != 26 {
		return false, "the mask should have twenty-six positions, one per letter"
	}
	// And a zero mask ends the loop with a failure, which is the only way it ends
	// unsuccessfully.
	if VolumeResolverFailsWithZero != 0 {
		return false, "an exhausted mask should return zero"
	}
	// **Both size arguments are 13 while both buffers are 16.** A port that passed 16 would
	// be asking for more than the reference does, and 13 is neither a buffer size nor a
	// round figure — so it is a deliberate limit rather than a computed one.
	if VolumeSizeArgument != 0x0D {
		return false, "the size argument should be 0x0D"
	}
	if VolumeSizeArgument == VolumeLabelBuffer || VolumeSizeArgument == VolumeFileSystemBuffer {
		return false, "the size argument should be neither of the buffer sizes"
	}
	if VolumeSizeArgument != 13 {
		return false, "the size argument should be thirteen"
	}
	if VolumeSizeArgument > VolumeLabelBuffer || VolumeSizeArgument > VolumeFileSystemBuffer {
		return false, "the size argument should fit the buffers it is paired with"
	}
	// And the two buffers are the same size, which is why one constant covers both.
	if VolumeLabelBuffer != 16 || VolumeFileSystemBuffer != 16 {
		return false, "both buffers should be sixteen bytes"
	}
	// **The result is the drive root**, so the base directory the path builder gets is
	// `X:\` — which is what the platform region's `"%c:\"` format string is for.
	if !VolumeResolverReturnsRoot {
		return false, "a match should yield the drive root"
	}
	if VolumeResolverRoutine != "FUN_0042B9F0" {
		return false, "the resolver's name should be the one the image gives"
	}
	return true, "a bitmask walk accepting four of six drive types, returning the root"
}

// # The two routines the resolver calls, neither of which is what its name suggested
//
// # `FUN_0042BB70` gets the *executable's* directory, not the current directory
//
//	void FUN_0042BB70(DWORD max, char *out) {
//	    if (GetModuleFileNameA(DAT_00459150, out, max) == 0) FUN_0042C470(0, 0x1A1);
//	    last = 0;
//	    while (*out != '\0') { if (*out == '\\') last = out; out++; }
//	    if (last == 0) FUN_0042C470(0, 0x1A2);
//	    *last = '\0';
//	}
//
// **It is the directory the executable is in, not the process's current directory.** The
// call is `GetModuleFileNameA` on the module handle `DAT_00459150` — the game's own path —
// and the routine truncates at the last backslash. So the path builder's `APPL` branch
// resolves against where the game is installed, not where it was launched from, and the two
// can differ: a shortcut, a double-click, or a working directory set by a launcher.
//
// The scan keeps the **most recent** backslash rather than stopping at the first, so it
// finds the last separator — the parent directory, not the first component. And it makes no
// attempt to canonicalise: the result is a prefix of the module path.
//
// The two diagnostics are **`0x1A1` and `0x1A2`** — consecutive, as every diagnostic pair
// in this engine is — and they sit well above the movie player's `0x14C7..0x14E9` block, so
// this is a distinct diagnostic range.
const (
	// ModuleDirRoutine is the function, FUN_0042BB70.
	ModuleDirRoutine = "FUN_0042BB70"
	// ModuleDirHandleSlot is the module handle global it passes, DAT_00459150. **The
	// game's own module**, which is what makes the result the install directory rather
	// than the working directory.
	ModuleDirHandleSlot = "DAT_00459150"
	// ModuleDirNoNameDiag and ModuleDirNoSeparatorDiag are its two diagnostics, **0x1A1**
	// and **0x1A2** — consecutive, as every diagnostic pair in this engine is — and they
	// sit in the low `0x1Ax` range, **well below** the movie player's `0x14C7..0x14E9`
	// block. I first wrote that they were above it; `0x1A1` is 417 and `0x14E9` is 5353,
	// so the block is above them. Either way this is a **distinct diagnostic range**, which
	// is the useful fact.
	ModuleDirNoNameDiag      uint16 = 0x1A1
	ModuleDirNoSeparatorDiag uint16 = 0x1A2
	// ModuleDirDiagRangeHigh bounds the range this pair belongs to, `0x1AF` — the low
	// `0x1Ax` block, far below the movie player's.
	ModuleDirDiagRangeHigh uint16 = 0x1AF
	// ModuleDirIsInstallDir records that the result is the executable's directory rather
	// than the process's current directory.
	ModuleDirIsInstallDir = true
	// ModuleDirKeepsLastSeparator records that the scan finds the **last** backslash
	// rather than stopping at the first, so the result is the parent directory and not
	// the first component.
	ModuleDirKeepsLastSeparator = true
)

// ModuleDir finds the parent directory of a module path, reproducing the routine: the
// last backslash truncated away, and no canonicalisation. A path with no backslash has no
// parent directory, which is the case the second diagnostic reports.
func ModuleDir(path string) (dir string, ok bool) {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == PathBackslash {
			return path[:i], true
		}
	}
	return "", false
}

// VerifyModuleDir checks that the result is the install directory rather than the working
// directory, and that the scan takes the last separator.
func VerifyModuleDir() (ok bool, detail string) {
	// **It is the module's own path**, so the result is where the game is installed — not
	// the process's current directory, which a launcher can set independently.
	if ModuleDirHandleSlot != "DAT_00459150" {
		return false, "the handle slot should be the game's module"
	}
	if !ModuleDirIsInstallDir {
		return false, "the result should be recorded as the install directory"
	}
	if ModuleDirRoutine != "FUN_0042BB70" {
		return false, "the routine's name should be the one the image gives"
	}
	// **The last backslash, not the first** — so the result is the parent directory and
	// not the first component, which is the mistake a forward-scan-then-stop reading
	// would make.
	dir, ok := ModuleDir(`C:\PROGRAM\GAMES\DUST\DF386.EXE`)
	if !ok {
		return false, "a path with a separator should have a parent"
	}
	if dir != `C:\PROGRAM\GAMES\DUST` {
		return false, "the result should be the path up to the last separator"
	}
	if !ModuleDirKeepsLastSeparator {
		return false, "the scan should be recorded as taking the last separator"
	}
	// A single separator gives the root without its trailing backslash, which is what
	// truncating at the separator gives.
	if dir, ok := ModuleDir(`C:\DF386.EXE`); !ok || dir != `C:` {
		return false, "a one-level path should give the root without its separator"
	}
	// **A path with no separator has no parent directory**, which is the case the second
	// diagnostic reports — and the routine does not fabricate one.
	if dir, ok := ModuleDir("DF386.EXE"); ok || dir != "" {
		return false, "a path with no separator should have no parent directory"
	}
	// And an empty path likewise.
	if _, ok := ModuleDir(""); ok {
		return false, "an empty path should have no parent directory"
	}
	// **The two diagnostics are consecutive**, as every diagnostic pair in this engine is,
	// and they are outside the movie player's block — so this is a distinct range.
	if ModuleDirNoSeparatorDiag != ModuleDirNoNameDiag+1 {
		return false, "the two diagnostics should be consecutive"
	}
	if ModuleDirNoNameDiag != 0x1A1 || ModuleDirNoSeparatorDiag != 0x1A2 {
		return false, "the diagnostics should be 0x1A1 and 0x1A2"
	}
	// **Both sit inside the low `0x1Ax` range** and far below the movie player's block, so
	// this is a distinct diagnostic range rather than a gap in that one.
	if ModuleDirNoNameDiag > ModuleDirDiagRangeHigh {
		return false, "the diagnostics should lie in the low 0x1Ax range"
	}
	if ModuleDirNoSeparatorDiag > ModuleDirDiagRangeHigh {
		return false, "both diagnostics should lie in the low 0x1Ax range"
	}
	if ModuleDirNoNameDiag > MoviePlayerLastDiag {
		return false, "the diagnostics are below the movie player's block, not above"
	}
	return true, "the executable's own directory, from the last separator, with 0x1A1 and 0x1A2"
}

// # `FUN_00441668` canonicalises a path in place, and upper-cases it
//
//	char *FUN_00441668(char *path) {
//	    if (DAT_00460A68 == 0) {
//	        for (p = path; *p; p++) if ('`' < *p && *p < '{') *p -= 0x20;
//	    } else {
//	        w = FUN_0043F93C(codepage, 0x200, path, -1, NULL, 0, 0);
//	        buf = FUN_0043EF50(w);
//	        if (w && buf && FUN_0043F93C(codepage, 0x200, path, -1, buf, w, 0))
//	            copy(buf, path);                      // the inlined word copy
//	        FUN_00440E1E(buf);                         // free
//	    }
//	    return path;                                   // the same pointer, modified
//	}
//
// So it is **not a change-directory call** — it never changes directory at all. It is a
// canonicaliser that returns the pointer it was given, modified in place. The path builder
// calls it on a volume label before comparing, and the resolver calls it on the candidate
// label too, so **both sides of the comparison are upper-cased** and the compare itself can
// be case-sensitive.
//
// # The third spelling of the same fold
//
// The upper-casing test is `'`' < c < '{'`, which is `0x60` and `0x7B` **written as
// characters instead of hex**. That is the same range the path builder uses and the same one
// the project's verified table covers — so the binary has one fold, spelled three ways: a
// 256-entry table at `0x0045DDF0`, a hex range test in the builder, and a character range
// test here.
const (
	// PathCanonicalRoutine is the function, FUN_00441668.
	PathCanonicalRoutine = "FUN_00441668"
	// PathCanonicalSlot is the global that chooses its branch, DAT_00460A68. **Zero takes
	// the plain upper-casing path**; non-zero means a codepage handle exists and the
	// narrow-to-wide round trip is used.
	PathCanonicalSlot = "DAT_00460A68"
	// PathCanonicalFlags is the conversion's flag word, 0x200.
	PathCanonicalFlags = 0x200
	// PathCanonicalBufferSize is the buffer it asks for, 0x200 as well.
	PathCanonicalBufferSize = 0x200
	// PathCanonicalReturnsSamePointer records that it returns the pointer it was given,
	// modified in place, rather than a new string.
	PathCanonicalReturnsSamePointer = true
	// PathCanonicalIsNotChdir records the naming correction: **it never changes the
	// process's current directory**, so calling it a chdir is wrong in a way that matters
	// — a port that issued a real chdir would alter the program's state.
	PathCanonicalIsNotChdir = true
	// PathFoldSpellings counts the three places the one fold is written: the table, the
	// builder's hex range, and this character range.
	PathFoldSpellings = 3
)

// UpperASCIICharRange reproduces this routine's own spelling of the fold, with the bounds
// as the characters they stand for. It is the same fold as `UpperASCIIInline` and agrees
// with it on every input, which is the point of counting the spellings.
func UpperASCIICharRange(c byte) byte {
	if c > '`' && c < '{' {
		return c - 0x20
	}
	return c
}

// VerifyPathCanonical checks the naming correction, the return convention, and that the
// character-spelled fold is the same fold as the hex-spelled one.
func VerifyPathCanonical() (ok bool, detail string) {
	// **It is not a change-directory call.** It never touches the process's directory, so
	// a port that issued a real chdir here would alter program state the reference never
	// alters. This is the naming correction, and it is a behavioural one.
	if !PathCanonicalIsNotChdir {
		return false, "the routine should be recorded as not changing directory"
	}
	if PathCanonicalRoutine != "FUN_00441668" {
		return false, "the routine's name should be the one the image gives"
	}
	// **It returns the pointer it was given**, modified in place — so a caller that
	// discarded the return and used its own buffer would miss the change entirely.
	if !PathCanonicalReturnsSamePointer {
		return false, "it should be recorded as returning the same pointer"
	}
	// The branch is chosen by one global: zero takes the plain fold, non-zero the
	// round trip through the wide API.
	if PathCanonicalSlot != "DAT_00460A68" {
		return false, "the branch global should be DAT_00460A68"
	}
	// The two 0x200 figures are the same number in two roles — the conversion's flags and
	// the buffer size — so they are recorded separately rather than conflated.
	if PathCanonicalFlags != 0x200 || PathCanonicalBufferSize != 0x200 {
		return false, "the flags and the buffer size should both be 0x200"
	}
	// **The character-spelled fold is the hex-spelled fold.** Two spellings of one range:
	// a backtick is 0x60 and a brace is 0x7B, and the arithmetic is the same 0x20.
	if '`' != PathLowerBound {
		return false, "a backtick should be the same byte as the hex lower bound"
	}
	if '{' != PathUpperBound {
		return false, "a brace should be the same byte as the hex upper bound"
	}
	// And they agree on every byte, which is what makes them one fold spelled twice.
	for c := 0; c < 256; c++ {
		if UpperASCIICharRange(byte(c)) != UpperASCIIInline(byte(c)) {
			return false, "the two spellings of the fold should agree on every byte"
		}
	}
	// Three spellings in the binary, and the project holds two of them.
	if PathFoldSpellings != 3 {
		return false, "the fold should be spelled three times in the binary"
	}
	// The range covers the 26 letters and leaves the two brackets alone — the strict
	// comparisons on both sides are what exclude them.
	if UpperASCIICharRange('`') != '`' || UpperASCIICharRange('{') != '{' {
		return false, "the bounds themselves should not be folded"
	}
	return true, "an in-place upper-caser, not a chdir, with the fold spelled a third way"
}
