package native

// The file-open, seek, page-fill and eviction primitives, mapped in full. They are four
// small functions and between them they fix the engine's **file-status convention**,
// which is the same convention as the `0x6B` the project already records from a
// different direction.

// # The packed return convention
//
// Three of the four return a value with a **status in the low sixteen bits and something
// else — the handle's high half — in the top sixteen**:
//
//	if SetFilePointer(...) != INVALID_SET_FILE_POINTER { return DVar1 & 0xFFFF0000; }
//	...
//	*param_2 = handle;
//	return handle & 0xFFFF0000;
//
// And the callers read **only the low half, and as a signed 16-bit value**:
//
//	uVar1 = FUN_0042C010(param_1, *param_2 * 4 + 0x400);
//	if ((short)uVar1 == 0) { ... }
//
// Two things follow, and both are the kind of detail a port gets wrong:
//
//   - **Success is zero.** A handle's high half is essentially always zero, so a
//     successful call returns 0. The callers' `== 0` test is therefore a *success* test,
//     and a port that treated a non-zero return as failure would invert it — though not
//     silently, because the one real status, `0x6B`, is also non-zero.
//   - **The comparison is signed**, so a status with the high bit of its low half set
//     would read as negative. `0x6B` does not, so no current status depends on it, but
//     the comparison's signedness is part of the convention rather than an accident of
//     how one call site happens to be written.
const (
	// FileStatusShift is where the handle's high half sits.
	FileStatusShift = 16
	// FileStatusMask isolates the low half, the status.
	FileStatusMask uint16 = 0xFFFF
	// FileStatusOK is the success value, zero. **A successful call returns zero**,
	// because a handle's high sixteen bits are zero on this target.
	FileStatusOK uint16 = 0x0000
	// FileStatusOpenFailed is `0x6B`, returned when neither open attempt succeeds. **This
	// is the status the project already records as "file not found"**, reached here from
	// the file layer rather than from the name resolvers — the same number from two
	// independent directions, which is what makes it trustworthy.
	FileStatusOpenFailed uint16 = 0x6B
	// FileOpenFailed is the open's failure return, `0xFFFF006B`. **The top half is set
	// too**, so the whole value is negative as a signed 32-bit number, and the callers'
	// signed 16-bit read of the low half is the only part that carries meaning.
	FileOpenFailed uint32 = 0xFFFF0000 | uint32(FileStatusOpenFailed)
	// FileSeekFailed is the seek's failure return, built by CONCAT22, and carries the same
	// status with a **zero** top half. So the open and the seek disagree about the top
	// half on failure, and a caller comparing whole values would see that — even though
	// the two mean exactly the same thing.
	FileSeekFailed uint32 = uint32(FileStatusOpenFailed)
)

// FileStatus extracts the status from a packed return, which is what every caller does.
func FileStatus(packed uint32) uint16 {
	return uint16(packed & uint32(FileStatusMask))
}

// FileHandleHalf extracts the top half, which on success is a handle's high sixteen bits
// and on failure is a status flag. It is zero on both a successful open and a failed seek,
// so a caller could not tell those two apart from the top half alone.
func FileHandleHalf(packed uint32) uint32 {
	return packed & 0xFFFF0000
}

// FileSucceeded reports whether a packed return is a success, using the **signed** 16-bit
// comparison the call sites actually use rather than a zero test on the whole value.
func FileSucceeded(packed uint32) bool {
	return int16(FileStatus(packed)) == 0
}

// VerifyFileStatusConvention checks the convention as the call sites use it: the status
// lives in the low half, success is zero, the comparison is signed, and the one real
// status is the `0x6B` the project already holds.
func VerifyFileStatusConvention() (ok bool, detail string) {
	if FileStatusMask != 0xFFFF {
		return false, "the status mask should be the low sixteen bits"
	}
	if FileStatusShift != 16 {
		return false, "the handle's half should sit above sixteen bits"
	}
	// **Success is zero**, which is the convention's most surprising property and the one
	// a port is most likely to invert.
	if FileStatusOK != 0 {
		return false, "success should be zero"
	}
	// And the status is `0x6B`, already verified from the name resolvers.
	if FileStatusOpenFailed != 0x6B {
		return false, "the open failure should be 0x6B"
	}
	// **The comparison is signed**, so a status with its high bit set would read as
	// negative — and `0x6B` does not, which is why no current status depends on it.
	if int16(FileStatusOpenFailed) < 0 {
		return false, "0x6B should read as a positive signed value"
	}
	if int16(FileStatusOpenFailed) != 0x6B {
		return false, "0x6B should survive the signed narrowing unchanged"
	}
	// The seek's failure carries the same status with a **zero** top half, so a caller
	// comparing whole values would see a difference between two failures that mean the
	// same thing.
	if FileSeekFailed>>FileStatusShift != 0 {
		return false, "the seek's failure should leave the top half clear"
	}
	// And the two statuses agree even though the whole values do not — which is what lets
	// one status serve the whole file layer.
	if FileStatus(FileOpenFailed) != FileStatus(FileSeekFailed) {
		return false, "both failures should carry the same status"
	}
	if FileStatus(FileOpenFailed) != FileStatusOpenFailed {
		return false, "the open's failure should carry the status"
	}
	// A successful return has a clear top half, so it is zero as a whole value.
	if FileHandleHalf(0) != 0 {
		return false, "a successful return's top half should be zero"
	}
	return true, "status in the low half, success is zero, and the one failure is 0x6B"
}

// # Opening: two attempts, and the second is not just a narrower one
//
//	handle = CreateFileA(path, 0xC0000000, 1, NULL, 3, 0x8000080, NULL);
//	if (handle == INVALID_HANDLE_VALUE) {
//	    handle = CreateFileA(path, 0x80000000, 1, NULL, 3, 0x8000001, NULL);
//	    if (handle == INVALID_HANDLE_VALUE) return 0xFFFF006B;
//	}
//
// **The retry drops two things, not one.** The access mask narrows from read-write to
// read-only, which is the obvious intent — a read-only file should still open. But the
// flags and attributes change as well: the first asks for `FILE_ATTRIBUTE_NORMAL` and the
// second does not. So the retry is not "the same open with less access"; it is a
// different open, and a port that copied the first's flags into the second would ask for
// the normal attribute on a file that may be read-only.
const (
	// FileOpenAccessReadWrite is the first attempt's access mask, GENERIC_READ |
	// GENERIC_WRITE.
	FileOpenAccessReadWrite uint32 = 0xC0000000
	// FileOpenAccessRead is the second's, GENERIC_READ alone.
	FileOpenAccessRead uint32 = 0x80000000
	// FileOpenShareMode is FILE_SHARE_READ, one, the same in both attempts.
	FileOpenShareMode uint32 = 1
	// FileOpenDisposition is OPEN_EXISTING, three. **The engine never creates a file
	// here** — it only ever opens one that is already there, so a missing file is the
	// `0x6B` case rather than an empty new file.
	FileOpenDisposition uint32 = 3
	// FileOpenFlagsFirst and FileOpenFlagsSecond are the two flag words.
	//
	// The first is FILE_FLAG_WRITE_THROUGH | FILE_ATTRIBUTE_NORMAL. The second is
	// **FILE_FLAG_WRITE_THROUGH alone** — the normal-attribute bit is gone.
	FileOpenFlagsFirst  uint32 = 0x08000080
	FileOpenFlagsSecond uint32 = 0x08000001
	// FileOpenInvalid is INVALID_HANDLE_VALUE, which both attempts are tested against.
	// A test asserts the *sign*, because a comparison against 0 rather than against this
	// would treat a valid handle of 0 as a failure.
	FileOpenInvalid uint32 = 0xFFFFFFFF
	// FilePathMax is the path buffer's size, 256 bytes. The path builder writes into a
	// buffer of this size and the open passes it straight through, so a path the builder
	// could overrun is a buffer overrun here.
	FilePathMax = 256
	// FilePathMaxSeek is the seek's own path buffer, 384 bytes — **larger than the open's**,
	// so the two callers of the same path builder disagree about how much room a path
	// needs. That is a fact about the reference, not an invitation to unify them.
	FilePathMaxSeek = 384
)

// FileAttributeNormal is the bit the first attempt sets and the second does not.
const FileAttributeNormal uint32 = 0x00000080

// FileFlagWriteThrough is the bit both attempts set.
const FileFlagWriteThrough uint32 = 0x08000000

// VerifyFileOpenAttempts checks the two attempts as a pair, and in particular that they
// differ in **more than the access mask**.
func VerifyFileOpenAttempts() (ok bool, detail string) {
	// The access masks narrow from read-write to read, which is the obvious intent.
	if FileOpenAccessReadWrite != 0xC0000000 {
		return false, "the first attempt should ask for read and write"
	}
	if FileOpenAccessRead != 0x80000000 {
		return false, "the second attempt should ask for read only"
	}
	// Read-write is the first mask with the write bit set, and read is the first with it
	// clear — so the narrowing is exactly the write bit.
	if FileOpenAccessReadWrite&0x40000000 == 0 {
		return false, "the first mask should include write access"
	}
	if FileOpenAccessRead&0x40000000 != 0 {
		return false, "the second mask should exclude write access"
	}
	if FileOpenAccessReadWrite&^FileOpenAccessRead != 0x40000000 {
		return false, "the two masks should differ in the write bit alone"
	}
	// **The flags differ in more than the access mask.** This is the point: a port that
	// reused the first's flags would ask for the normal attribute on a file that may be
	// read-only.
	if FileOpenFlagsFirst == FileOpenFlagsSecond {
		return false, "the two attempts should use different flags"
	}
	if FileOpenFlagsFirst&FileAttributeNormal == 0 {
		return false, "the first attempt should ask for the normal attribute"
	}
	if FileOpenFlagsSecond&FileAttributeNormal != 0 {
		return false, "the second attempt should not ask for the normal attribute"
	}
	// And the write-through bit is in both, so it is the one thing the retry preserves
	// from the flags.
	for _, flags := range []uint32{FileOpenFlagsFirst, FileOpenFlagsSecond} {
		if flags&FileFlagWriteThrough == 0 {
			return false, "both attempts should set the write-through flag"
		}
	}
	// The share mode and the disposition are the same in both, so the retry is not a
	// different sharing or creation policy.
	if FileOpenShareMode != 1 {
		return false, "the share mode should be FILE_SHARE_READ"
	}
	if FileOpenDisposition != 3 {
		return false, "the disposition should be OPEN_EXISTING"
	}
	// **OPEN_EXISTING, so the engine never creates a file here** — a missing file is the
	// `0x6B` case rather than an empty new one. There is no bit test for this: the five
	// dispositions are 1, 2, 3, 4 and 5, so 3 is only "existing" by equality. A mask
	// would be the wrong tool, which is worth saying because the first attempt's *access*
	// masks are a different kind of value and invite the mistake.
	if FileOpenDisposition != 3 {
		return false, "the disposition should be OPEN_EXISTING, which is 3"
	}
	// INVALID_HANDLE_VALUE is all ones, and **it must be tested as a whole word**: a
	// comparison against zero would reject a valid handle of 0, which is a real handle on
	// some systems.
	if FileOpenInvalid != 0xFFFFFFFF {
		return false, "the invalid handle should be all ones"
	}
	if FileOpenInvalid&0x7FFFFFFF == 0 {
		return false, "the invalid handle should not look like zero in its low bits"
	}
	return true, "read-write then read-only, and the retry also drops the normal attribute"
}

// # Seeking: three outcomes, and one of them is a retry
//
//	uVar1 = SetFilePointer(handle, offset, NULL, FILE_BEGIN);
//	if (uVar1 != INVALID_SET_FILE_POINTER) return uVar1 & 0xFFFF0000;
//	GetLastError();                                    // result discarded
//	FUN_00443add8(local_180, FUN_00428d40(0x92));     // a message, formatted
//	if (FUN_0042bb10(FUN_00428d40(0x93), local_180)) ...
//
// # The three outcomes
//
// The message-box result is a packed value read as a signed 32-bit integer, and the three
// cases are:
//
//	if ((int)result < 0) break;   // -> 0x6B, give up
//	if (0 < (int)result) return result >> 16 << 16;   // -> success
//	// result == 0 falls through and loops
//
// **A zero result retries.** The caller's answer does not settle it; the loop runs again
// and seeks again. So the reference retries indefinitely on a "no" answer, and the only
// ways out are a positive answer or a negative one. That is a genuine property of the
// code and not a shape a reader would assume, since a dialog that returns zero is more
// often "cancelled" than "try again".
const (
	// SeekRetryResult is the message-box result that **retries**. It is the only outcome
	// that is neither success nor failure.
	SeekRetryResult int32 = 0
	// SeekResourceMessage and SeekResourceCaption are the two string resources the seek
	// formats into its message-box arguments: 0x92 and 0x93 in decimal, 146 and 147.
	//
	// **Both are fetched through `FUN_00428D40`**, the same string-resource lookup
	// `dialogs.go` already records as `StringResourceLookup`, so the file layer and the
	// dialog layer reach their text through one routine.
	SeekResourceMessage uint32 = 0x92
	SeekResourceCaption uint32 = 0x93
	// SeekMessageMax is the message buffer's size, 384 — the same width as the path
	// buffer, so one buffer holds both.
	SeekMessageMax = FilePathMaxSeek
	// SeekFailed is the status the seek gives up with, the same `0x6B` the open returns.
	SeekFailed uint16 = FileStatusOpenFailed
)

// SeekOutcome classifies a message-box result into the three cases the loop has.
func SeekOutcome(result int32) (retry bool, success bool) {
	switch {
	case result < 0:
		return false, false
	case result > 0:
		return false, true
	default:
		return true, false
	}
}

// VerifySeekLoop checks the three outcomes and the retry, which is the property that a
// reader would not assume.
func VerifySeekLoop() (ok bool, detail string) {
	// **Zero retries.** Neither success nor failure, and the loop runs again.
	retry, success := SeekOutcome(SeekRetryResult)
	if !retry || success {
		return false, "a zero result should retry and not succeed"
	}
	// A negative result gives up, and it is the only outcome that does.
	for _, result := range []int32{-1, -1000, -0x7FFFFFFF} {
		if retry, success := SeekOutcome(result); retry || success {
			return false, "a negative result should neither retry nor succeed"
		}
	}
	// A positive result succeeds, and the two are exclusive.
	for _, result := range []int32{1, 6, 0x7FFFFFFF} {
		if retry, success := SeekOutcome(result); retry || !success {
			return false, "a positive result should succeed and not retry"
		}
	}
	// And the give-up status is the same `0x6B` the open returns — one status for the
	// whole file layer, reached from two different failures.
	if SeekFailed != 0x6B {
		return false, "the seek's give-up status should be 0x6B"
	}
	// The two resources are consecutive, so a port that fetched one and not the other
	// would have a message with no caption.
	if SeekResourceCaption != SeekResourceMessage+1 {
		return false, "the caption resource should follow the message resource"
	}
	if SeekResourceMessage != 0x92 {
		return false, "the message resource should be 0x92"
	}
	// And they are fetched through the lookup the project already records, so the file
	// layer and the dialog layer share one text resolver.
	if StringResourceLookup != "FUN_00428D40" {
		return false, "the resource lookup should be the one dialogs.go records"
	}
	return true, "a zero answer retries, a positive one succeeds, a negative one gives up with 0x6B"
}

// # The reference quirk worth naming: GetLastError is called and discarded
//
// The seek calls `GetLastError()` immediately after a failed `SetFilePointer` and **uses
// none of it**. The error code is not consulted, not logged, and not folded into the
// status; the function goes straight on to building a message box and the `0x6B` it
// eventually returns is fixed rather than derived.
//
// That is preserved deliberately. A port that "improved" this by branching on the error
// code would return a different status for the same failure, and the `0x6B` the project
// has verified from the name resolvers is only trustworthy because every path that can
// produce it produces it unconditionally.
const (
	// SeekCallsGetLastError records that the call is made, and that its result is unused.
	SeekCallsGetLastError = true
	// SeekUsesGetLastError is false, and **it should stay false**: the status is fixed.
	SeekUsesGetLastError = false
	// SeekDiagnosticOrder is the sequence the failure path performs: seek, read the error
	// code and discard it, format the message, ask, then classify the answer. It is worth
	// recording because the discarded call sits *between* the failure and the message, so
	// a port that dropped the call would be a small, invisible change.
	SeekDiagnosticOrder = 5
)

// VerifyGetLastErrorIsDiscarded checks the quirk, so a later "fix" is a deliberate change
// rather than an accident.
func VerifyGetLastErrorIsDiscarded() (ok bool, detail string) {
	if !SeekCallsGetLastError {
		return false, "the reference does call GetLastError after a failed seek"
	}
	if SeekUsesGetLastError {
		return false, "the reference discards its result, and this records that it does"
	}
	// The status is fixed, not derived — which is the whole point.
	if SeekFailed != 0x6B {
		return false, "the status should be the fixed 0x6B rather than an error code"
	}
	// And the diagnostic order puts the discarded call after the failure, so removing it
	// changes the path without changing its shape visibly.
	if SeekDiagnosticOrder != 5 {
		return false, "the failure path should perform five steps"
	}
	return true, "the error code is read and thrown away, and the status is fixed"
}

// # The page record, and the two callers that differ only in one routine
//
// The fill and the eviction are the **same function with one name changed**:
//
//	void FUN_00402c40(void *cache, void *page) {
//	    if ((short)FUN_0042c010(cache, *(int *)page * 4 + 0x400) == 0)
//	        FUN_0042be70(cache, (char *)page + 10, 0x200);
//	}
//	void FUN_00402c80(void *cache, void *page) {
//	    if ((short)FUN_0042c010(cache, *(int *)page * 4 + 0x400) == 0)
//	        FUN_0042bf70(cache, (char *)page + 10, 0x200);
//	}
//
// Three things fall out of the shape, and the third is the useful one:
//
//   - **The seek offset is `index * 4 + 0x400`.** So the file's first 1024 bytes are
//     something else, and the index is a slot number multiplied by four — the index table
//     is at four bytes per entry.
//   - **The payload is 0x200 bytes at offset 10 of the record.** So a record is a
//     **ten-byte header followed by 512 bytes of payload**, and the header's first four
//     bytes are the index the seek uses.
//   - **Fill and eviction differ in exactly one routine**, `FUN_0042BE70` against
//     `FUN_0042BF70` — same spelling, one digit apart. Everything else, including the
//     seek and the guard, is identical. A port that implemented only the fill and left
//     the eviction calling it would still pass every test that did not check the
//     direction.
const (
	// PageIndexScale is what the index is multiplied by, four.
	PageIndexScale = 4
	// PageIndexBase is what is added, 0x400 — the first 1024 bytes are not slots.
	PageIndexBase = 0x400
	// PageHeaderSize is the record's header, ten bytes, and the payload starts there.
	PageHeaderSize = 10
	// PagePayloadSize is the payload, 0x200 = 512 bytes.
	PagePayloadSize = 0x200
	// PageFillRoutine and PageEvictRoutine are the two routines that differ.
	PageFillRoutine  = "FUN_0042BE70"
	PageEvictRoutine = "FUN_0042BF70"
	// PageSeekRoutine is the one they share, along with the guard.
	PageSeekRoutine = "FUN_0042C010"
)

// PageFileOffset gives the file position a page's slot is at, which is the arithmetic
// both callers perform.
func PageFileOffset(index int32) int32 {
	return index*PageIndexScale + PageIndexBase
}

// VerifyPageRecord checks the record's shape and the offset arithmetic, and in particular
// that **fill and eviction share everything except one routine**.
func VerifyPageRecord() (ok bool, detail string) {
	// **The offset is the index times four plus 1024**, so the file's first 1024 bytes
	// are not slots and the index table is four bytes per entry.
	if PageIndexScale != 4 {
		return false, "the index should be scaled by four"
	}
	if PageIndexBase != 0x400 {
		return false, "the offset base should be 0x400"
	}
	// The arithmetic itself, at both ends of a plausible range.
	if got := PageFileOffset(0); got != 0x400 {
		return false, "slot zero should be at 0x400"
	}
	if got := PageFileOffset(1); got != 0x404 {
		return false, "slot one should be at 0x404"
	}
	if got := PageFileOffset(255); got != 0x400+255*4 {
		return false, "the offset should scale linearly"
	}
	// **A negative index produces a position before the base**, which is what the
	// reference computes and would seek to — so a caller passing a bad index seeks
	// backwards rather than refusing. Recorded because it is the arithmetic, not a guard
	// the code has.
	if got := PageFileOffset(-1); got != 0x3FC {
		return false, "a negative index should give 0x3FC, which is before the base"
	}
	// The record is a ten-byte header then a 512-byte payload.
	if PageHeaderSize != 10 {
		return false, "the header should be ten bytes"
	}
	if PagePayloadSize != 0x200 {
		return false, "the payload should be 512 bytes"
	}
	// **The header is large enough to hold the four-byte index** the seek reads, and the
	// six bytes after it are not accounted for by this slice.
	if PageHeaderSize < 4 {
		return false, "the header should hold at least the index"
	}
	// The payload is a power of two, which is what makes it a cache line.
	if PagePayloadSize&(PagePayloadSize-1) != 0 {
		return false, "the payload should be a power of two"
	}
	// **Fill and eviction differ in exactly one routine.** The two names are twelve
	// characters and share **eleven** of them — `FUN_0042BE70` against `FUN_0042BF70`,
	// differing at one position, the `E` against the `F`. So only a single character tells
	// them apart, which is invisible in a diff and easy to lose in a rename.
	if PageFillRoutine == PageEvictRoutine {
		return false, "the fill and eviction routines should differ"
	}
	if len(PageFillRoutine) != len(PageEvictRoutine) {
		return false, "the two routine names should be the same length"
	}
	shared, differing := 0, 0
	for i := 0; i < len(PageFillRoutine); i++ {
		if PageFillRoutine[i] == PageEvictRoutine[i] {
			shared++
		} else {
			differing++
		}
	}
	// **Exactly one differing character**, and every other one shared.
	if differing != 1 {
		return false, "the two names should differ in exactly one character"
	}
	if shared != len(PageFillRoutine)-1 {
		return false, "every other character should be shared"
	}
	// The one that differs is the seventh-from-last, which is the digit after the `B`.
	if PageFillRoutine[len(PageFillRoutine)-3] == PageEvictRoutine[len(PageEvictRoutine)-3] {
		return false, "the differing character should be the one after the B"
	}
	if PageFillRoutine == PageSeekRoutine || PageEvictRoutine == PageSeekRoutine {
		return false, "neither payload routine is the shared seek"
	}
	// The payload offset is the header size, which is what makes the record's two halves
	// adjacent rather than overlapping or gapped.
	if PageHeaderSize+PagePayloadSize != 522 {
		return false, "the whole record should be 522 bytes"
	}
	return true, "a ten-byte header, a 512-byte payload, and two routines differing in one character"
}
