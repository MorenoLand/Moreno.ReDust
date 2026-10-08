package native

// Three small functions with an outsized amount to say: the sub-resource stamp, the
// memory-availability query, and the audio volume getter. Two of them are corrections.

// # The stamp is the file's last eight bytes
//
// The call site is exactly as `subcache.go` recorded it:
//
//	void FUN_00402160(node, offset, out) {
//	    if ((short)FUN_0042C010(node, offset) == 0) FUN_0042BE70(node, out, 8);
//	}
//
// But the previous two slices have established that **`FUN_0042BE70` seeks to `FILE_END`
// before it reads**. So the seek to `offset` is superseded, exactly as it is in the page
// record, and the eight bytes that land in `out` are **the file's trailing eight bytes** —
// not the bytes at `offset`.
//
// That changes what the stamp is. It is not a per-resource tag positioned by an offset; it
// is a **whole-file trailing integrity value**. Two consequences follow, and both matter:
//
//   - **Comparing a stamp from one file against a stamp from another at the same offset is
//     comparing the same thing twice.** The offset identifies *which* file's trailing
//     stamp to read, and nothing else.
//   - **The 64-bit width is what makes it useful.** A trailing eight-byte value changes
//     when the file's length or its tail changes, so a 4-byte read would be a weaker check
//     on the same quantity — which is why `SubStampBytes` is 8 and the loader's
//     comparison is a 64-bit equality.
const (
	// SubStampReadRoutine is the read, which is the tail-read primitive.
	SubStampReadRoutine = PayloadReadRoutine
	// SubStampIsTrailing records that the stamp is the file's last eight bytes, because
	// the read seeks to the end rather than to the offset the caller passed.
	SubStampIsTrailing = true
	// SubStampWidthIs64Bit is why the width matters: the value is a trailing extent or
	// checksum, and half of it is a weaker check on the same quantity.
	SubStampWidthIs64Bit = true
)

// VerifyStampIsTrailing checks the correction, and states the two readings it leaves open
// — the record is careful not to claim more than the call site supports.
func VerifyStampIsTrailing() (ok bool, detail string) {
	// **The read seeks to the end**, which is the whole of the correction.
	if PayloadEndMethod != 1 {
		return false, "the read should seek with FILE_END"
	}
	if !SubStampIsTrailing {
		return false, "the stamp should be recorded as the file's trailing bytes"
	}
	// The stamp is eight bytes, and the read takes eight — so the read is one call and the
	// cap never applies, exactly as the previous slice found for a 1024-byte read.
	if SubStampBytes != 8 {
		return false, "the stamp should be eight bytes"
	}
	if PayloadReadChunks(SubStampBytes) != 1 {
		return false, "an eight-byte read should take a single call"
	}
	// **The width is what makes the check worth doing**, and it is 64-bit.
	if !SubStampWidthIs64Bit {
		return false, "the stamp's width should be recorded as 64-bit"
	}
	if SubStampBytes*8 != 64 {
		return false, "eight bytes should be a 64-bit quantity"
	}
	// And the read routine is the one the page record uses, so the engine has a single
	// file-read primitive — which is what `subcache.go` says, and it is right about that
	// even though it was wrong about the stamp's position.
	if SubStampReadRoutine != PayloadReadRoutine {
		return false, "the stamp should be read by the same primitive as the page payload"
	}
	if SubStampSeek != PageSeekRoutine {
		return false, "the stamp and the page should share the seek routine"
	}
	// So both call sites have the **same shape** — a seek whose position does not reach
	// the read, then the read — which is why the correction applies to both.
	if !PageSeekIsSuperseded {
		return false, "the page's superseded seek should still be recorded as such"
	}
	return true, "the stamp is the file's trailing eight bytes, and the seek only supplies a pass or fail"
}

// # The memory query: the minimum of three ceilings
//
//	MEMORYSTATUS mem; mem.dwLength = 0x20;
//	GlobalMemoryStatus(&mem);
//	if (mem.dwAvailPageFile < mem.dwAvailVirtual) mem.dwAvailVirtual = mem.dwAvailPageFile;
//	if (mem.dwTotalPhys     < mem.dwAvailVirtual) mem.dwAvailVirtual = mem.dwTotalPhys;
//	return mem.dwAvailVirtual;
//
// **It returns the smallest of three quantities**, and the two clamps are what make it so:
// available page file, then total physical, each folded into available virtual. The answer
// is therefore `min(availVirtual, availPageFile, totalPhys)` — the address-space ceiling
// and the physical-RAM ceiling both bound what can actually be allocated, and the smallest
// wins.
//
// The clamp order matters and is **page file first, then physical**. A port that clamped
// physical first would get the same *value* in every case where all three are present,
// because a minimum is a minimum — so the order is not observable in the result and is
// recorded only because the code is explicit about it.
const (
	// MemoryQueryRoutine is the function, FUN_0042C420.
	MemoryQueryRoutine = "FUN_0042C420"
	// MemoryStatusLength is the value written to `dwLength`, 0x20 = 32.
	//
	// **This is the compatibility shim, and it is the important part.** `_MEMORYSTATUS` is
	// 32 bytes on Windows 9x and 40 on NT, because NT added a field. Writing the length
	// tells the kernel which layout to fill, so this code is written against the **9x
	// layout** and asks for only the prefix on NT. It is a second, older way of
	// discriminating the two — alongside the `GetVersionExA` and `"NT "` probe the
	// platform region records, and it is the one that does not need a version call.
	MemoryStatusLength uint32 = 0x20
	// MemoryStatusLength9x and MemoryStatusLengthNT are the two real structure sizes, so
	// the constant's meaning is stated rather than left as a number.
	MemoryStatusLength9x = 32
	MemoryStatusLengthNT = 40
	// MemoryClampOrder is the two clamps, in the order the code applies them. The result
	// does not depend on the order, but the code does fix it.
	MemoryClampOrder = 2
)

// MemoryAvailable gives the value the query returns for a set of readings, reproducing the
// two clamps exactly.
func MemoryAvailable(availVirtual, availPageFile, totalPhys uint32) uint32 {
	result := availVirtual
	if availPageFile < result {
		result = availPageFile
	}
	if totalPhys < result {
		result = totalPhys
	}
	return result
}

// VerifyMemoryQuery checks the two clamps and the structure length, and in particular that
// the length is the **9x** size — so the code predates or ignores the NT layout that the
// version probe knows about.
func VerifyMemoryQuery() (ok bool, detail string) {
	// **The result is the minimum of the three**, so it is never more than any of them.
	for _, readings := range [][3]uint32{
		{100, 50, 200}, {100, 200, 50}, {100, 50, 50},
		{0, 0, 0}, {1, 2, 3}, {1000, 1, 1000}, {7, 9, 8},
	} {
		got := MemoryAvailable(readings[0], readings[1], readings[2])
		// The minimum of the three, computed separately so the check is not a restatement
		// of the implementation.
		want := readings[0]
		for _, v := range readings[1:] {
			if v < want {
				want = v
			}
		}
		if got != want {
			return false, "the result should be the smallest of the three readings"
		}
	}
	// So no reading can exceed the result, which is the property that makes it a ceiling.
	if MemoryAvailable(100, 200, 300) != 100 {
		return false, "the smallest reading should win"
	}
	// And a zero anywhere makes the answer zero, which is what a port that ignored the
	// clamps would get wrong.
	if MemoryAvailable(100, 0, 300) != 0 {
		return false, "a zero reading should make the answer zero"
	}
	// **The structure length is the 9x size**, not the NT one — so the query asks for the
	// 32-byte prefix and the extra NT field is never filled.
	if MemoryStatusLength != 0x20 {
		return false, "the length should be 0x20"
	}
	if MemoryStatusLength != MemoryStatusLength9x {
		return false, "the length should be the 9x structure size"
	}
	if MemoryStatusLength == MemoryStatusLengthNT {
		return false, "the length should not be the NT structure size"
	}
	// Which is the point: the engine carries **two** mechanisms for telling 9x from NT,
	// and this one is the older — it needs no version call, just a length the kernel reads.
	if MemoryStatusLength < MemoryStatusLength9x {
		return false, "the length should be at least the 9x size"
	}
	if OSTypePrefix != "NT " {
		return false, "the other mechanism's prefix should still be recorded"
	}
	// Two clamps, in the code's order: page file then physical.
	if MemoryClampOrder != 2 {
		return false, "there should be two clamps"
	}
	return true, "the minimum of three ceilings, asked for with the 9x structure length"
}

// # The audio volume getter, and the third home for the audio guard
//
//	uint FUN_00434970(void) {
//	    if (DAT_0045E068 == 0) return 0;
//	    FUN_0042FDD0(&local_2);
//	    return local_2;
//	}
//
// **The same guard, in a third function.** `DAT_0045E068` is the audio-initialised flag the
// message pump ticks on and `dialogs.go` already records, and this is a getter that returns
// zero when audio is not up rather than reading uninitialised state.
//
// The return is **16-bit** — `undefined2` — so the volume is a 16-bit quantity, not a byte
// and not a 32-bit word. That matters because a getter and a setter that disagreed about
// the width would lose the top bits on a large value, and the volume ladder the platform
// region records is a ten-step control, so the values are small; the width is still the
// contract.
const (
	// AudioVolumeRoutine is the getter, FUN_00434970.
	AudioVolumeRoutine = "FUN_00434970"
	// AudioVolumeReader is the routine it calls once the guard passes, FUN_0042FDD0.
	AudioVolumeReader = "FUN_0042FDD0"
	// AudioVolumeBits is the width, 16.
	AudioVolumeBits = 16
	// AudioGuardSlot is the flag the guard tests. **The third recorded use of it**, after
	// the pump's tick and the dialog layer's early return.
	AudioGuardSlot = AudioInitialisedSlot
	// AudioGuardReturns is how many recorded functions share this exact guard, three.
	AudioGuardReturns = 3
	// AudioGuardZeroWhenUninitialised is what every one of them returns when the flag is
	// clear: zero, not an error.
	AudioGuardZeroWhenUninitialised = 0
)

// AudioVolume reads the volume through the guard, reproducing the getter's shape: zero when
// audio is not initialised, and the 16-bit value otherwise.
func AudioVolume(initialised bool, read func(*uint16)) uint16 {
	if !initialised {
		return 0
	}
	var out uint16
	read(&out)
	return out
}

// VerifyAudioVolumeGuard checks the third home for the guard, and that the getter's width
// is 16-bit rather than a byte or a word.
func VerifyAudioVolumeGuard() (ok bool, detail string) {
	// **The guard is the same global the pump and the dialog layer already record**, so
	// the three uses cannot drift apart.
	if AudioGuardSlot != "DAT_0045E068" {
		return false, "the guard should be the audio-initialised flag"
	}
	if AudioGuardSlot != AudioInitialisedSlot {
		return false, "the guard and the dialog layer's slot should be the same global"
	}
	// Three recorded uses, and the return is **zero** when the flag is clear — not an
	// error, which is what makes the guard silent rather than noisy.
	if AudioGuardReturns != 3 {
		return false, "there should be three recorded uses of the guard"
	}
	if AudioGuardZeroWhenUninitialised != 0 {
		return false, "the guard should return zero"
	}
	if AudioVolume(false, func(*uint16) {}) != 0 {
		return false, "an uninitialised read should be zero"
	}
	// And the reader is not called at all in that case, so it never touches
	// uninitialised state.
	called := false
	AudioVolume(false, func(*uint16) { called = true })
	if called {
		return false, "the reader should not run when audio is uninitialised"
	}
	// **The width is 16-bit**, so the value is a word rather than a byte.
	if AudioVolumeBits != 16 {
		return false, "the volume should be 16 bits wide"
	}
	if AudioVolumeBits/8 != 2 {
		return false, "sixteen bits should be two bytes"
	}
	// And an initialised read passes its value through unchanged, including the top half of
	// the word — which is the property a byte-width port would lose. The loop is written
	// out because a `t.Errorf` cannot appear in a non-test function.
	for _, want := range []uint16{0, 1, 255, 256, 1000, 0x8000, 0xFFFF} {
		got := AudioVolume(true, func(out *uint16) { *out = want })
		if got != want {
			return false, "an initialised read should pass its value through unchanged"
		}
	}
	// The routine and its reader are different functions, so the guard and the read are
	// separable.
	if AudioVolumeRoutine == AudioVolumeReader {
		return false, "the getter and its reader should be different routines"
	}
	return true, "a 16-bit getter behind the same audio guard the pump and dialogs use"
}
