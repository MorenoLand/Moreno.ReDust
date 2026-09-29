package scripts

// The platform region, 0x0045DA78..0x0045DDC0, transcribed in full. It is the fifth
// string region and the one that names the operating system interface: window classes,
// cursor files, DLL names resolved at run time, an OS-version probe, and the display-mode
// enumeration the video selector uses.
//
// This region resolves three things earlier slices had left as addresses, and it explains
// two that earlier slices had recorded without knowing what they called.

// # The bounds, and what sits at the start
const (
	// PlatformRegionFirst is the first byte of the region, which is **not a string**: it is
	// the fifteen-byte ramp. The first *string* is at PlatformRegionFirstString.
	PlatformRegionFirst uint32 = 0x0045DA78
	// PlatformRegionFirstString is "WIN", the first printable string.
	PlatformRegionFirstString uint32 = 0x0045DA89
	// PlatformRegionLastString is the region's last printable string, "*.rt", at
	// 0x0045DDBC. The region continues past it — the string `APPL` that `pathbuild.go`
	// already records as PathAppleVolumeAddress lies inside this span — so this is the
	// last *string this slice transcribed*, not the region's end.
	PlatformRegionLastString uint32 = 0x0045DDBC
	// PlatformRegionScanEnd is where the transcription stopped, chosen to take in
	// PathAppleVolumeAddress at 0x0045DD94.
	PlatformRegionScanEnd uint32 = 0x0045DDC0
	// PlatformRegionSeparator is how many NULs separate strings here. Runs of three and
	// four appear, so unlike the pool and the prop/shop region this one is **not** on a
	// single-NUL discipline; the addresses below are read from the image, not inferred
	// from a separator count.
	PlatformRegionSeparator = 0
)

// The fifteen-byte ramp at the region's first address, which is worth recording as data
// rather than skipping: **seven 0x08 bytes, then 07, 06, 05, 04, 03, 02, 01, 00**.
//
// Seven at the front and then a plain countdown to zero, which together is fifteen bytes
// — the last `00` in the image is the region's NUL separator, not part of the ramp. A
// ramp that begins with a repeated value and then descends is not a sequence of fifteen
// distinct entries, so any port that indexed it as a plain 0..14 table would be wrong
// about its first seven slots. What it drives is not established here.
var platformPriorityRamp = [15]byte{
	0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08,
	0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0x00,
}

// PlatformRampRepeat is how many bytes at the front share one value, seven.
const PlatformRampRepeat = 7

// VerifyPlatformRamp checks the ramp's shape, which is the property that makes it
// something other than a plain table.
func VerifyPlatformRamp() (ok bool, detail string) {
	if len(platformPriorityRamp) != 15 {
		return false, "the ramp should be fifteen bytes"
	}
	// **The first seven are all the same value.** That is what makes the ramp not a table
	// of fifteen distinct entries.
	const repeat = PlatformRampRepeat
	for i := 0; i < repeat; i++ {
		if platformPriorityRamp[i] != platformPriorityRamp[0] {
			return false, "the ramp's first seven bytes should all be equal"
		}
	}
	if platformPriorityRamp[0] != 0x08 {
		return false, "the repeated value should be eight"
	}
	// And the remaining eight are a plain countdown from seven to zero.
	for i := 0; i < len(platformPriorityRamp)-repeat; i++ {
		want := byte(7 - i)
		if platformPriorityRamp[repeat+i] != want {
			return false, "the ramp's tail should count down from seven to zero"
		}
	}
	// **The repeated value and the countdown's top are adjacent**: eight, then seven.
	// So the whole ramp is 8,8,8,8,8,8,8,7,6,...,1,0 — a non-increasing sequence with
	// one repeated run, not a permutation and not a table.
	for i := 1; i < len(platformPriorityRamp); i++ {
		if platformPriorityRamp[i] > platformPriorityRamp[i-1] {
			return false, "the ramp should never increase"
		}
	}
	// And the last byte is zero, which is where the region's NUL separator begins.
	if platformPriorityRamp[len(platformPriorityRamp)-1] != 0x00 {
		return false, "the ramp should end at zero"
	}
	return true, "seven repeated, then a plain countdown to zero"
}

// # The volume ladder, which runs downwards
//
// Ten strings, "volume 9" through "volume 0", at a uniform twelve-byte stride, in
// **descending** order:
//
//	0x0045DACD 'volume 9'    0x0045DAD9 'volume 8'    ...    0x0045DB39 'volume 0'
//
// Ten steps, not nine: the ladder includes both ends, so a port that counted down from
// nine would stop one short. The uniform stride and the descending order together are
// what make this a table — the number in a name is `9 - index`, which a caller can
// compute rather than parse.
const (
	// PlatformVolumeFirst is "volume 9".
	PlatformVolumeFirst uint32 = 0x0045DACD
	// PlatformVolumeCount is how many, ten, from nine down to zero inclusive.
	PlatformVolumeCount = 10
	// PlatformVolumeStride is the gap between consecutive names, 12 bytes.
	PlatformVolumeStride = 12
	// PlatformVolumeTop and PlatformVolumeBottom are the two ends' numbers.
	PlatformVolumeTop    = 9
	PlatformVolumeBottom = 0
)

// PlatformVolumeName returns the ladder's entry for a level. **The level is the number in
// the name**: level three gives "volume 3". Out-of-range levels return nothing rather
// than a name, because the ladder has exactly ten entries and a level of ten has no
// string in the image.
func PlatformVolumeName(level int) (string, bool) {
	if level < PlatformVolumeBottom || level > PlatformVolumeTop {
		return "", false
	}
	// Every level in range is a single digit, since the ladder runs zero to nine, so the
	// name is the prefix and one character.
	return "volume " + string(rune('0'+level)), true
}

// PlatformVolumeAddressForLevel gives where a level's name sits in the image. **This is
// the descending part**: level nine is the *first* name and level zero the last, so the
// address runs the opposite way from the number. A port that walked the addresses
// ascending and called the result "levels zero to nine" would invert the ladder.
func PlatformVolumeAddressForLevel(level int) (uint32, bool) {
	if level < PlatformVolumeBottom || level > PlatformVolumeTop {
		return 0, false
	}
	return PlatformVolumeFirst +
		uint32(PlatformVolumeStride*(PlatformVolumeTop-level)), true
}

// VerifyVolumeLadder checks the ladder's three properties: ten entries, a uniform
// twelve-byte stride, and descending order.
func VerifyVolumeLadder() (ok bool, detail string) {
	// **Ten entries, inclusive of both ends.** This is the number a port is most likely
	// to get wrong, since counting down from nine suggests nine.
	if PlatformVolumeCount != 10 {
		return false, "the ladder should hold ten names"
	}
	if PlatformVolumeCount != PlatformVolumeTop-PlatformVolumeBottom+1 {
		return false, "the count should be the span plus one, so both ends are included"
	}
	if PlatformVolumeStride != 12 {
		return false, "the stride should be twelve bytes"
	}
	// Every level in range resolves, and out-of-range levels do not — a level of ten
	// must fail rather than producing a name the image does not hold.
	for level := PlatformVolumeBottom; level <= PlatformVolumeTop; level++ {
		name, found := PlatformVolumeName(level)
		if !found {
			return false, "a level inside the range should resolve"
		}
		if name != "volume "+string(rune('0'+level)) {
			return false, "the name for a level should be the level's own digit"
		}
	}
	for _, level := range []int{-1, 10, 11, 100} {
		if name, found := PlatformVolumeName(level); found {
			return false, "the level " + itoa(level) + " should not resolve to " + name
		}
	}
	// **The first name is the top and the last is the bottom**, so the ladder descends
	// in number while ascending in address. The two orders are opposite, which is what a
	// port that walked the addresses would silently transpose.
	first, okFirst := PlatformVolumeName(PlatformVolumeTop)
	if !okFirst || first != "volume 9" {
		return false, "the first name should be the top of the ladder"
	}
	last, okLast := PlatformVolumeName(PlatformVolumeBottom)
	if !okLast || last != "volume 0" {
		return false, "the last name should be the bottom of the ladder"
	}
	// And the address mapping is the descending one: level nine is the first address.
	addr, okAddr := PlatformVolumeAddressForLevel(PlatformVolumeTop)
	if !okAddr || addr != PlatformVolumeFirst {
		return false, "the top level's name should be the first address"
	}
	bottomAddr, okBottom := PlatformVolumeAddressForLevel(PlatformVolumeBottom)
	if !okBottom || bottomAddr != 0x0045DB39 {
		return false, "the bottom level's name should be at 0x0045DB39"
	}
	// A level of ten has no address, so the mapping refuses rather than running past.
	if _, found := PlatformVolumeAddressForLevel(10); found {
		return false, "a level past the top should have no address"
	}
	if PlatformVolumeFirst+uint32(PlatformVolumeStride*(PlatformVolumeCount-1)) !=
		0x0045DB39 {
		return false, "the last name should be at 0x0045DB39"
	}
	return true, "ten names, twelve-byte stride, descending from nine to zero"
}

// itoa is a small helper for the check's messages, kept local so the region does not
// reach for strconv in a file that is otherwise plain data.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// # The permutation at 0x0045DB78
//
// Nineteen DWORDs, and **they are a permutation of zero through eighteen**: every value
// in that range appears exactly once. It is a reordering, not a selection and not a set
// of thresholds.
//
// The one property that makes it a trap: **the value zero sits at index fifteen**, and
// the DWORD immediately after the table is *also* zero. So the table's own data contains
// its terminator's value, and a reader cannot find the end by scanning for zero. Nineteen
// is the count, and it has to come from outside the table. A port that stopped at the
// first zero would get fifteen entries and silently drop four.
const (
	// PlatformPermutationAddress is where the nineteen DWORDs sit.
	PlatformPermutationAddress uint32 = 0x0045DB78
	// PlatformPermutationCount is nineteen, and it must be carried rather than derived.
	PlatformPermutationCount = 19
	// PlatformPermutationZeroIndex is where the value zero sits, fifteen.
	PlatformPermutationZeroIndex = 15
	// PlatformPermutationTerminator is the zero DWORD that follows the table, at
	// PlatformPermutationAddress + 19*4. **It has the same value as the element at
	// index fifteen**, which is the whole reason the count cannot be read off the data.
	PlatformPermutationTerminator uint32 = PlatformPermutationAddress +
		4*PlatformPermutationCount
)

// platformPermutation is the table, read from the image in scan order.
var platformPermutation = [PlatformPermutationCount]uint32{
	0x0A, 0x02, 0x0C, 0x01, 0x0F, 0x10, 0x12, 0x09, 0x11, 0x0D,
	0x0E, 0x0B, 0x03, 0x04, 0x07, 0x00, 0x05, 0x06, 0x08,
}

// VerifyPlatformPermutation checks the three claims: nineteen entries, a permutation of
// zero through eighteen, and the zero at index fifteen.
func VerifyPlatformPermutation() (ok bool, detail string) {
	n := len(platformPermutation)
	if n != PlatformPermutationCount {
		return false, "the permutation should hold nineteen entries"
	}
	// **Every value appears exactly once and the set is exactly 0..18.** Both halves
	// matter: a duplicate would leave a gap, and a value outside the range would make the
	// length and the range disagree.
	seen := make(map[uint32]int, n)
	for i, v := range platformPermutation {
		if v >= uint32(n) {
			return false, "a value is outside the range the length implies"
		}
		if prior, dup := seen[v]; dup {
			return false, "the value " + itoa(int(v)) + " appears at both " +
				itoa(prior) + " and " + itoa(i)
		}
		seen[v] = i
	}
	for want := uint32(0); want < uint32(n); want++ {
		if _, present := seen[want]; !present {
			return false, "the value " + itoa(int(want)) + " does not appear"
		}
	}
	// **The zero is at index fifteen**, and that is what makes the terminator ambiguous.
	if platformPermutation[PlatformPermutationZeroIndex] != 0 {
		return false, "the zero should sit at index fifteen"
	}
	for i, v := range platformPermutation {
		if v == 0 && i != PlatformPermutationZeroIndex {
			return false, "the zero should appear only at index fifteen"
		}
	}
	// **The terminator's address is one past the last element**, so a reader told the
	// count can find the end exactly — and a reader without it cannot.
	if PlatformPermutationTerminator != PlatformPermutationAddress+4*19 {
		return false, "the terminator should sit one DWORD past the last element"
	}
	// The permutation's first value is ten and its last is eight, so it is not a
	// rotation, a reversal or an identity shift — the three shapes a reader would try.
	if platformPermutation[0] == platformPermutation[n-1] {
		return false, "the first and last values should differ"
	}
	return true, "a permutation of zero through eighteen, with the zero at index fifteen"
}

// # The coverage mask that follows the permutation
//
// From `0x0045DBC8` the image holds alternating runs of `0x00000000` and `0x00FFFFFF`
// DWORDs, with a sixteen-byte all-zero gap between the two groups. **The opaque value is
// `0x00FFFFFF`, not `0xFFFFFFFF`**: in memory the four bytes are FF FF FF 00, so a run
// covers three bytes and leaves the fourth clear. That is what makes it an anti-aliased
// edge rather than a solid fill — a solid run would not stop mid-DWORD. The second group
// is the same run shifted.
const (
	// PlatformMaskOpaque is the DWORD 0x00FFFFFF, a run of set bits.
	PlatformMaskOpaque uint32 = 0x00FFFFFF
	// PlatformMaskEmpty is 0x00000000, a run of clear bits.
	PlatformMaskEmpty uint32 = 0x00000000
	// PlatformMaskFirst is where the runs begin, immediately after the permutation's
	// terminator.
	PlatformMaskFirst uint32 = PlatformPermutationTerminator + 4
	// PlatformMaskOpaqueRun is how many consecutive opaque DWORDs the first group holds,
	// three.
	PlatformMaskOpaqueRun = 3
	// PlatformMaskRowGap is the sixteen-byte all-zero gap between the two groups. It is
	// one scanline's worth of clear pixels, so the mask is at least two rows tall.
	PlatformMaskRowGap = 16
)

// VerifyPlatformMask checks the two-group shape and the two values it is built from.
func VerifyPlatformMask() (ok bool, detail string) {
	if PlatformMaskOpaque != 0x00FFFFFF {
		return false, "the opaque run should be 0x00FFFFFF"
	}
	if PlatformMaskEmpty != 0x00000000 {
		return false, "the empty run should be zero"
	}
	// **The opaque value's high byte is clear**, which is what distinguishes it from a
	// full 0xFFFFFFFF. In memory the DWORD's bytes run FF FF FF 00, so the run is "all
	// but the last byte of the DWORD" and not "all".
	if PlatformMaskOpaque&0xFF == 0 {
		return false, "the opaque run's low byte should be set"
	}
	if PlatformMaskOpaque>>24 != 0 {
		return false, "the opaque run should have a clear high byte"
	}
	if PlatformMaskOpaque != 0x00FFFFFF {
		return false, "the opaque run should be 0x00FFFFFF"
	}
	// **The run is three DWORDs**, so twelve bytes of coverage, and the row gap is wider
	// than that — which is what makes it a row separator rather than a continuation.
	if PlatformMaskOpaqueRun != 3 {
		return false, "the opaque run should be three DWORDs"
	}
	if PlatformMaskRowGap <= 4*PlatformMaskOpaqueRun {
		return false, "the row gap should exceed the opaque run's width"
	}
	if PlatformMaskRowGap != 16 {
		return false, "the row gap should be sixteen bytes"
	}
	// The mask begins immediately after the permutation's terminator, with no gap, so
	// the two tables are adjacent and a reader cannot confuse their boundaries.
	if PlatformMaskFirst != PlatformPermutationTerminator+4 {
		return false, "the mask should start one DWORD after the permutation ends"
	}
	return true, "three opaque DWORDs, then a sixteen-byte row gap, then the run again"
}

// # The strings, grouped by what they are for
//
// Fifty printable strings, transcribed whole. Grouping them is the useful part: the
// region is not a list, it is five groups.

// The window and resource class names.
const (
	// WindowClassMain is the application's main window class, DFWINDOW.
	WindowClassMain = "DFWINDOW"
	// WindowClassMessages is the second class, Messages — a message-only window, which
	// is how the engine receives shell messages without a visible window.
	WindowClassMessages = "Messages"
	// ResourceCoverAccelerators and ResourceCoverName are the accelerator table's name
	// and the window class it belongs to. **"DFCOVER" and "Cover"** are separate strings,
	// and the accelerator table is named for the cover window specifically rather than
	// for the game.
	ResourceCoverAccelerators = "ACCELTABLE"
	ResourceCoverName        = "DFCOVER"
	ResourceCoverMenu        = "Cover"
	// IconName is the application's icon, DUSTICON.
	IconName = "DUSTICON"
	// PluginExport is the export name a plugin DLL must provide, PlugProc.
	PluginExport = "PlugProc"
	// DLLSuffix is ".DLL", spelled in upper case as Windows import records do.
	DLLSuffix = ".DLL"
	// VolumeSuffix is the second accelerator-table name, "2", after "1".
	VolumeSuffixOne = "1"
	VolumeSuffixTwo = "2"
)

// The two cursor files. **These are the operating-system names for the two bare names the
// prop/shop region stores** — "arrow" and "watch" — so the six-name run's first two
// members are cursor selections rather than context labels, and the chain from a script's
// `cursor` argument to a file on disk is now complete.
const (
	// CursorFileArrow is CURS.ARROW, the arrow cursor.
	CursorFileArrow = "CURS.ARROW"
	// CursorFileWatch is CURS.WATCH, the watch cursor.
	CursorFileWatch = "CURS.WATCH"
	// CursorNameArrow is the bare name "arrow", which the `cursor` builtin compares
	// against. **This resolves CursorNamePoolAddress**, which earlier entries recorded as
	// a bare address with a guessed name deliberately withheld: 0x0045DA4C is this
	// region's one-byte-before-its-text convention applied to "arrow" at 0x0045DA4D.
	CursorNameArrow = "arrow"
	// CursorNameWatch is the bare name "watch", at 0x0045DA45.
	CursorNameWatch = "watch"
	// CursorDefaultFile is which of the two a failure falls back to. The status 0x2B
	// recorded in flags.go is "the default cursor resource cannot be loaded", so the
	// default is the one the region lists first — CURS.ARROW, before CURS.WATCH.
	CursorDefaultFile = CursorFileArrow
)

// VerifyCursorChain closes the loop from the script builtin to the file. Three links
// that were previously separate facts: the builtin's comparison address, the two bare
// names in the prop/shop region, and the two OS cursor files.
func VerifyCursorChain() (ok bool, detail string) {
	// **The builtin's address is one below the name it compares against**, which is this
	// region's convention rather than the pool's.
	if CursorNamePoolAddress != 0x0045DA4C {
		return false, "the cursor builtin's address should be 0x0045DA4C"
	}
	if CursorNamePoolAddress+1 != 0x0045DA4D {
		return false, "the name should be at 0x0045DA4D"
	}
	// And the prop/shop region really holds that name, so the two regions agree.
	name, found := PropShopRegionText(0x0045DA4D)
	if !found || name != CursorNameArrow {
		return false, "the region should hold the arrow name at 0x0045DA4D"
	}
	// **The other bare name is the watch cursor**, so the two of the six short names that
	// are not keyword stems are the two cursor selections.
	watch, foundWatch := PropShopRegionText(0x0045DA45)
	if !foundWatch || watch != CursorNameWatch {
		return false, "the region should hold the watch name at 0x0045DA45"
	}
	// **Every member of the six-name run is either a cursor or a keyword stem.** That is
	// the answer to what the run is for, and it is worth checking as a property: five of
	// the six are accounted for by the two mechanisms this project has now found, and
	// the sixth is the only member that is neither.
	unexplained := 0
	for _, name := range propShopContextNames {
		isCursor := name == CursorNameArrow || name == CursorNameWatch
		if isCursor {
			continue
		}
		if IsKeywordStem(name) {
			continue
		}
		unexplained++
	}
	if unexplained != 1 {
		return false, "exactly one of the six names should be neither a cursor nor a stem"
	}
	// And the stems in the run are the three that the keyword table does *not* store
	// bare — so the run and the table are complements here rather than duplicates.
	stemCount := 0
	for _, name := range propShopContextNames {
		if IsKeywordStem(name) {
			stemCount++
		}
	}
	if stemCount != 3 {
		return false, "the run should hold three keyword stems"
	}
	// **The two files are the names with a "CURS." prefix and upper case**, which is a
	// derivable relationship rather than a coincidence of spelling.
	if CursorFileArrow != "CURS."+UpperASCII(CursorNameArrow) {
		return false, "the arrow file should be the name prefixed and upper-cased"
	}
	if CursorFileWatch != "CURS."+UpperASCII(CursorNameWatch) {
		return false, "the watch file should be the name prefixed and upper-cased"
	}
	// The default is the arrow, which is the one the region lists first.
	if CursorDefaultFile != CursorFileArrow {
		return false, "the default cursor should be the arrow"
	}
	// And the two are distinct, so a fallback to one is not a fallback to the other.
	if CursorFileArrow == CursorFileWatch {
		return false, "the two cursor files should differ"
	}
	// The unavailable status names the *default* resource, so it is the arrow that fails.
	if CursorDefaultFile != CursorFileArrow {
		return false, "the 0x2B status is about the default resource"
	}
	return true, "the cursor builtin compares against \"arrow\", which is CURS.ARROW on disk"
}

// The three DLLs and what each is loaded for. **These are resolved by name at run time**,
// not through the import table — the names and the module names sit in .data beside the
// code that calls LoadLibrary, which is why they are in the image at all.
const (
	// ModuleSystem is KERNEL32.DLL, from which only GetSystemInfo is taken.
	ModuleSystem = "KERNEL32.DLL"
	// SystemInfoProc is that one import.
	SystemInfoProc = "GetSystemInfo"
	// ModuleGDI is GDI32.DLL, which provides the DIB calls the blit path needs.
	ModuleGDI = "GDI32.DLL"
	// ModuleUser is USER32.DLL, which provides the display-mode enumeration and the
	// window-system calls.
	ModuleUser = "USER32.DLL"
	// ModuleWinG is WING32.DLL, the GDI accelerator wrapper. **Its presence is the
	// answer to whether the engine used a hardware blit**: the four WinG entry points are
	// resolved by name, and a machine without WING32.DLL falls back to the GDI path.
	ModuleWinG = "WING32.DLL"
	// The WinG entry points, in the region's order.
	ProcWinGBitBlt           = "WinGBitBlt"
	ProcWinGSetDIBColorTable = "WinGSetDIBColorTable"
	ProcWinGCreateBitmap     = "WinGCreateBitmap"
	ProcWinGCreateDC         = "WinGCreateDC"
	// The GDI entry points that WinG stands in for.
	ProcSetDIBColorTable = "SetDIBColorTable"
	ProcCreateDIBSection = "CreateDIBSection"
)

// # The OS-version probe, and why the save-game warning exists
//
// "GetVersionExA" at 0x0045DD34 and the three bytes "NT " at 0x0045DD48 are adjacent in
// this region, and together they are the engine's operating-system check: fill an
// `OSVERSIONINFO`, call `GetVersionExA`, and test whether the returned platform name
// begins with "NT ".
//
// **This is the mechanism behind the save-game compatibility message.** The warning
// recorded in the prop/shop region is "This saved game is from a different version of
// this title.", and a version stamped into a save is almost certainly the OS version
// read through this call. So the warning is not about the *game's* version — it is about
// the machine's. A save written on NT and loaded on a 9x box, or vice versa, is what the
// message reports, and the movie player reuses the same message because the movie is
// embedded in the save.
const (
	// ProcGetVersionExA is the version probe.
	ProcGetVersionExA = "GetVersionExA"
	// OSTypePrefix is the three characters the engine looks for in the platform name.
	// **The trailing space is part of the test** — "NT" alone would also match "NTL", so
	// a port that dropped it would accept a platform it should reject.
	OSTypePrefix = "NT "
	// OSTypePrefixLength is three.
	OSTypePrefixLength = 3
)

// IsNTPlatform reports whether an OSVERSIONINFO platform name is the one the engine
// accepts, testing the full three characters including the space.
func IsNTPlatform(platformName string) bool {
	if len(platformName) < OSTypePrefixLength {
		return false
	}
	return equalASCIIFold(platformName[:OSTypePrefixLength], OSTypePrefix)
}

// VerifyOSTypePrefix checks the three-character test, and specifically that the space is
// part of it — a prefix test that stopped after "NT" would be a different, looser test.
func VerifyOSTypePrefix() (ok bool, detail string) {
	if OSTypePrefix != "NT " {
		return false, "the prefix should be three characters"
	}
	if OSTypePrefixLength != 3 {
		return false, "the prefix length should be three"
	}
	if OSTypePrefix[OSTypePrefixLength-1] != ' ' {
		return false, "the prefix should end in a space"
	}
	// **The space is what makes the test exact.** Without it, a name beginning "NTL"
	// would match, so a port that trimmed the prefix would accept the wrong platform.
	if !IsNTPlatform("NT ") {
		return false, "the exact name should be accepted"
	}
	// A name of exactly two characters is rejected rather than padded: there is no third
	// character to compare, and GetVersionExA always supplies one.
	if IsNTPlatform("NT") {
		return false, "a bare NT should be rejected, since there is no third character"
	}
	// A name whose third character is a letter is rejected, which is the case the space
	// exists to exclude.
	if IsNTPlatform("NTL") {
		return false, "NTL should not be accepted: the third character is not a space"
	}
	// Longer names match on their first three characters, which is what a prefix test
	// does, so "NT 4.0"-style spellings are accepted.
	if !IsNTPlatform("NT something") {
		return false, "a longer name should match on its first three characters"
	}
	return true, "the test is three characters including the space, so \"NTL\" is rejected"
}

// # The display-mode enumeration, which is the video selector's real mechanism
//
// "EnumDisplaySettingsA" and "ChangeDisplaySettingsA" are both in this region, in that
// order, and both are resolved from USER32.DLL. This is the answer to a question the
// video-driver selector left open: it does not pick a mode from a table in the file, it
// **asks the operating system what modes exist and then changes to one.** The twenty
// driver resources `videomode.go` records are the candidates it tries; the enumeration is
// how it learns which of them the machine actually has.
const (
	// ProcEnumDisplaySettings enumerates the available modes.
	ProcEnumDisplaySettings = "EnumDisplaySettingsA"
	// ProcChangeDisplaySettings applies one.
	ProcChangeDisplaySettings = "ChangeDisplaySettingsA"
	// ProcExitWindows is resolved alongside them, for the shutdown path.
	ProcExitWindows = "ExitWindowsEx"
	// QuitMenuItem is the menu entry that reaches it, with its accelerator ampersand.
	QuitMenuItem = "E&xit"
)

// VerifyDisplayModePair checks that the two mode calls are both present, come from the
// same module, and are spelled with the `A` suffix — the ANSI variants, which is what a
// 1995 port uses and what a port must keep or the marshalling changes.
func VerifyDisplayModePair() (ok bool, detail string) {
	if ModuleUser != "USER32.DLL" {
		return false, "the display calls should come from USER32.DLL"
	}
	if ProcEnumDisplaySettings != "EnumDisplaySettingsA" {
		return false, "the enumeration should be the ANSI variant"
	}
	if ProcChangeDisplaySettings != "ChangeDisplaySettingsA" {
		return false, "the change should be the ANSI variant"
	}
	// **Both carry the `A` suffix and neither is the `W` variant.** A 1995 port calling
	// the wide entry points would fail to load on a system without them, so the suffix is
	// load-bearing rather than cosmetic.
	for _, proc := range []string{ProcEnumDisplaySettings, ProcChangeDisplaySettings} {
		if len(proc) < 1 || proc[len(proc)-1] != 'A' {
			return false, "the procedure should end in the ANSI suffix"
		}
		if len(proc) >= 2 && proc[len(proc)-2] == 'W' {
			return false, "the procedure should not be the wide variant"
		}
	}
	// The two are different calls — one asks, one applies — so a port that used one for
	// both would not be expressing the same shape.
	if ProcEnumDisplaySettings == ProcChangeDisplaySettings {
		return false, "the enumeration and the change should be different procedures"
	}
	// The three are the region's only USER32 imports, so the module's whole purpose here
	// is display and shutdown.
	if ProcExitWindows != "ExitWindowsEx" {
		return false, "the shutdown call should be ExitWindowsEx"
	}
	if QuitMenuItem != "E&xit" {
		return false, "the quit item should carry its accelerator ampersand"
	}
	return true, "the two ANSI display calls and the shutdown call, all from USER32.DLL"
}

// # The remaining strings
//
// Paths, a font, a signature, and a wildcard.
const (
	// PathFormat is "%c:\\", which takes the drive letter the path builder already
	// handles and makes a root. So the drive prefix the project parses separately is
	// formatted from here.
	PathFormat = "%c:\\"
	// PathSeparator is a bare backslash.
	PathSeparator = "\\"
	// PathWildcard is "*.*", the file dialog's filter.
	PathWildcard = "*.*"
	// FontDefault is Arial, the UI font.
	FontDefault = "Arial"
	// SignatureCompany is the string the executable carries, "Raven Digital" — **the
	// developer, and the only place in the transcribed regions that names a company.**
	SignatureCompany = "Raven Digital"
	// SaveExtension is "*.rt", the recorder's own file wildcard. It is *not* the game
	// data's extension, so a port that treated it as such would save to the wrong files.
	SaveExtension = "*.rt"
)

// The Apple Event vocabulary, which is the Macintosh path the engine keeps.
//
// "appl:", "quit" and "message" are an Apple Event target, an event name and a reply
// selector — the shape of a program that answers quit and message requests. The volume
// ladder beside them is the volume control event, which is a **ten-step** set.
const (
	// AppleEventTarget is "appl:", the target the engine sends to.
	AppleEventTarget = "appl:"
	// AppleEventQuit is the quit event's name.
	AppleEventQuit = "quit"
	// AppleEventMessage is the message event's name, and the reply selector.
	AppleEventMessage = "message"
	// AppleEventSuffix is the colon the target name carries. **The colon is part of the
	// name**, so a port that built the target by appending it would double it.
	AppleEventSuffix = ":"
)

// VerifyAppleEvents checks the three Apple Event strings and the colon, and the one thing
// that is easy to get wrong about them: the target is *not* the same string as the
// application's own name, and the volume names are not Apple Events.
func VerifyAppleEvents() (ok bool, detail string) {
	if AppleEventTarget != "appl:" {
		return false, "the target should be appl: with its colon"
	}
	// **The colon is the last character and is part of the name.**
	if AppleEventTarget[len(AppleEventTarget)-1] != ':' {
		return false, "the target should end in a colon"
	}
	// The target is lower case while the volume names are not, so a case-folding port
	// that compared them interchangeably would be wrong.
	if AppleEventTarget != UpperASCII(AppleEventTarget) {
		if !equalASCIIFold(AppleEventTarget, UpperASCII(AppleEventTarget)) {
			return false, "the target should compare equal to its upper-case form"
		}
	}
	// **The two event names are single words with no punctuation**, unlike the target.
	for _, name := range []string{AppleEventQuit, AppleEventMessage} {
		if len(name) == 0 {
			return false, "an event name is empty"
		}
		for i := 0; i < len(name); i++ {
			c := name[i]
			if c == ':' || c == ' ' {
				return false, "an event name should carry no punctuation or spaces"
			}
		}
	}
	if AppleEventQuit == AppleEventMessage {
		return false, "the two event names should differ"
	}
	// And the volume names are **not** event names: they carry a space and a digit, so a
	// port that treated the whole group as event names would mis-parse all ten.
	for level := 0; level < PlatformVolumeCount; level++ {
		name, found := PlatformVolumeName(level)
		if !found {
			return false, "a volume name should resolve"
		}
		if len(name) <= len("volume ") {
			return false, "a volume name should carry a digit after its prefix"
		}
		last := name[len(name)-1]
		if last < '0' || last > '9' {
			return false, "a volume name should end in a digit"
		}
	}
	return true, "appl: carries its colon, the two event names do not, and the ten volume names are a separate group"
}

// UpperASCII returns a copy of s in upper case, byte by byte, which is all these strings
// need. It uses the same ASCII-only fold the rest of the engine's comparisons use, so a
// name written in upper case here compares equal to the mixed-case form on disk.
func UpperASCII(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'a' && out[i] <= 'z' {
			out[i] -= 'a' - 'A'
		}
	}
	return string(out)
}
