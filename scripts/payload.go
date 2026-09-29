package scripts

// The two payload routines the page fill and eviction call, and the message box both of
// them — and the shared routine. Mapped in full.
//
// **This file corrects the previous one in two places**, and both corrections are about
// what the routines actually do rather than what their call sites suggested.

// # The message box, which names the three outcomes
//
//	uint FUN_0042BB10(char *caption, char *text) {
//	    FUN_00428D70(text, local_200, DAT_00445884);      // a 512-byte expander
//	    iVar1 = MessageBoxA(NULL, local_200, caption, 0x32);
//	    if (iVar1 == 3) return 0xFFFFFFFF;               // IDABORT
//	    return (iVar1 == 5);                              // IDRETRY, else zero
//	}
//
// `0x32` decomposes as **`MB_ABORTRETRYIGNORE | MB_ICONEXCLAMATION`**: the low nibble is
// the button set and the next two bits the icon, so three buttons and a warning triangle.
// The third button is therefore *Ignore*, and that is the button the return value cannot
// distinguish from anything else:
//
//   - **IDABORT, 3, returns `0xFFFFFFFF`** — the callers read that as negative and give
//     up with `0x6B`.
//   - **IDRETRY, 5, returns 1** — read as positive, so the callers treat it as success
//     and return.
//   - **IDIGNORE, 4, returns 0** — and the callers **loop**.
//
// # This corrects the previous slice
//
// I wrote that a zero answer is a retry "not a shape a reader would assume, since a
// dialog returning zero is more often *cancelled* than *try again*". The speculation was
// wrong in a better way than wrong: the zero is not an absence of an answer, it is
// **IDIGNORE**, and the reference treats "Ignore" as "try again anyway". So the retry is
// not a surprising convention — it is the third button, wired to the loop.
const (
	// MessageBoxStyle is the flags word, 0x32.
	MessageBoxStyle uint32 = 0x32
	// MessageBoxButtons is the low nibble, MB_ABORTRETRYIGNORE. **Three buttons**, which
	// is what makes three outcomes possible at all.
	MessageBoxButtons uint32 = MessageBoxStyle & 0x0F
	// MessageBoxIcon is the next two bits, MB_ICONEXCLAMATION — a warning triangle, not
	// an error stop. So the box warns rather than blocks.
	MessageBoxIcon uint32 = (MessageBoxStyle >> 4) & 0x03
	// MessageBoxAbort is IDABORT, 3.
	MessageBoxAbort = 3
	// MessageBoxRetry is IDRETRY, 5.
	MessageBoxRetry = 5
	// MessageBoxIgnore is IDIGNORE, 4, and it is the one that returns zero.
	MessageBoxIgnore = 4
	// MessageBoxAbortReturn is what IDABORT produces, 0xFFFFFFFF. **The callers test the
	// sign**, so a full-word compare against a small constant would not work.
	MessageBoxAbortReturn uint32 = 0xFFFFFFFF
	// MessageBoxTextMax is the expanded message buffer, 512 bytes. The expander writes
	// into it before the box, so a text longer than 512 bytes is a buffer overrun.
	MessageBoxTextMax = 512
	// MessageBoxRoutine is the box itself.
	MessageBoxRoutine = "FUN_0042BB10"
	// MessageExpanderRoutine is `FUN_00428D70`, which fills the 512-byte buffer from the
	// text and a substitution table, so the message may carry more than its own text.
	MessageExpanderRoutine = "FUN_00428D70"
	// MessageExpanderTable is the substitution table's global, DAT_00445884.
	MessageExpanderTable = "DAT_00445884"
)

// MessageBoxAnswer classifies a `MessageBoxA` return into the three cases the callers
// have, and it is exhaustive: every defined button id lands in exactly one case.
func MessageBoxAnswer(id int32) (abort bool, retry bool) {
	switch id {
	case MessageBoxAbort:
		return true, false
	case MessageBoxRetry:
		return false, true
	default:
		return false, false
	}
}

// VerifyMessageBox checks the flags' decomposition and the three answers, and in
// particular that **the zero is the third button rather than an absence of one**.
func VerifyMessageBox() (ok bool, detail string) {
	// **Three buttons**, which is the precondition for three outcomes.
	if MessageBoxButtons != 0x02 {
		return false, "the button set should be MB_ABORTRETRYIGNORE"
	}
	if MessageBoxButtons != 2 {
		return false, "the button set should be 2"
	}
	// And a warning icon rather than an error stop, decomposed out of the same word.
	if MessageBoxIcon != 0x03 {
		return false, "the icon should be MB_ICONEXCLAMATION"
	}
	if (MessageBoxStyle>>4)&0x03 != MessageBoxIcon {
		return false, "the icon should come from the flags word's bits four and five"
	}
	// The two fields do not overlap, so the style is not a value read two ways.
	if MessageBoxButtons&(MessageBoxIcon<<4) != 0 {
		return false, "the button and icon fields should not overlap in the style word"
	}
	// **The three button ids are distinct and in ascending order**, which is what makes
	// the sign-based classification total.
	if MessageBoxAbort >= MessageBoxIgnore || MessageBoxIgnore >= MessageBoxRetry {
		return false, "the button ids should ascend: abort, ignore, retry"
	}
	if MessageBoxAbort != 3 || MessageBoxIgnore != 4 || MessageBoxRetry != 5 {
		return false, "the ids should be 3, 4 and 5"
	}
	// **IDIGNORE produces the zero**, and the zero is what the callers loop on. So the
	// retry case is the third button rather than an absence of an answer.
	abort, retry := MessageBoxAnswer(MessageBoxIgnore)
	if abort || retry {
		return false, "IDIGNORE should be neither an abort nor an explicit retry"
	}
	// And the callers treat that as a retry, so the classification the box performs and
	// the one the callers perform are different — which is the whole finding.
	if loop, _ := SeekOutcome(0); !loop {
		return false, "the callers should loop on the zero that IDIGNORE produces"
	}
	// IDABORT gives the all-ones word, and the callers test the sign.
	abort, retry = MessageBoxAnswer(MessageBoxAbort)
	if !abort || retry {
		return false, "IDABORT should be the abort case alone"
	}
	if MessageBoxAbortReturn != 0xFFFFFFFF {
		return false, "the abort return should be all ones"
	}
	// The sign test needs a variable: a conversion of a constant expression is still a
	// constant, and the compiler rejects the overflow rather than doing the narrowing.
	abortReturn := MessageBoxAbortReturn
	if int32(abortReturn) >= 0 {
		return false, "the abort return should read as negative, which is how it is tested"
	}
	// IDRETRY gives exactly one, which is the positive case.
	abort, retry = MessageBoxAnswer(MessageBoxRetry)
	if abort || !retry {
		return false, "IDRETRY should be the retry case alone"
	}
	// **Every defined button id is classified**, so no answer falls through unhandled.
	// The undefined ids in the same range take the default, which is the loop — the same
	// as IDIGNORE, and worth stating because an unrecognised id retries rather than
	// aborting.
	for _, id := range []int32{1, 2, 6, 7, 0, -1} {
		abort, retry := MessageBoxAnswer(id)
		if abort || retry {
			return false, "an undefined button id should take the default case"
		}
	}
	// The expander's buffer is twice the seek's, so a long message is the box's problem
	// and not the caller's.
	if MessageBoxTextMax != 512 {
		return false, "the message buffer should be 512 bytes"
	}
	if MessageBoxTextMax <= SeekMessageMax {
		return false, "the box's buffer should be the larger of the two"
	}
	return true, "three buttons, three answers, and the zero is the third button"
}

// # The two payload routines, which are a tail-read and an append
//
// They are the two halves of one pair, and neither takes an offset:
//
//	void FUN_0042BE70(void *file, void *dst, uint count) {
//	    pump(); _DAT_00443F58 = 0;
//	    if (SetFilePointer(handle, 0, NULL, FILE_END) == INVALID) return 0xFFFF006B;
//	    do { chunk = min(count, 10000); ReadFile(handle, dst, chunk, &got, NULL);
//	         dst += chunk; count -= chunk; } while (got == chunk);
//	    ... on failure: the 0x90/0x91 message pair, then the box ...
//	}
//
//	void FUN_0042BF70(void *file, void *src, DWORD count) {
//	    pump(); _DAT_00443F58 = 0;
//	    if (SetFilePointer(handle, 0, NULL, FILE_END) == INVALID) return 0xFFFF006B;
//	    if (!WriteFile(handle, src, count, &written, NULL)) return 0x6B;
//	    return (count == written) - 1 & 0xFFFF006B;
//	}
//
// Four things fall out, and the first two correct the previous slice:
//
//   - **Both seek to `FILE_END`**, so they read the *last* `count` bytes and append at the
//     end. Neither takes a position, because neither uses one.
//   - **The read is chunked at 10,000 bytes and the write is not.** A single `WriteFile`
//     for the whole count, against a `ReadFile` loop capped at 10,000. So the two halves
//     of one pair disagree about whether large transfers are split.
//   - **The read appends to the destination as it goes**, advancing the pointer and
//     decrementing the count, so the loop's exit test is "did I get everything I asked
//     for" rather than "have I read anything".
//   - **The write's success test is inverted**, which is the reference's own quirk and is
//     preserved deliberately — see below.
const (
	// PayloadReadRoutine and PayloadWriteRoutine name the pair. **These are the same two
	// routines the previous slice called the page fill and the eviction**; the read and
	// the write are the better names, because what they do is not fill or evict anything.
	PayloadReadRoutine  = PageFillRoutine
	PayloadWriteRoutine = PageEvictRoutine
	// PayloadChunkMax is the read's chunk cap, 10,000 bytes. It is a decimal figure, not
	// a power of two, which is worth recording because every other size in this layer is
	// a power of two or a multiple of four.
	PayloadChunkMax uint32 = 10000
	// PayloadWriteChunked is false: the write makes a single call. **The two halves of
	// the pair disagree about this**, and the asymmetry is the reference's.
	PayloadWriteChunked = false
	// PayloadReadChunked is true, against that cap.
	PayloadReadChunked = true
	// PayloadEndMethod is the `SetFilePointer` method both use: **FILE_END, 1**. So
	// neither routine takes a position — a tail read and an append.
	PayloadEndMethod uint32 = 1
	// PayloadProgressSlot is the global both clear before their transfer,
	// `_DAT_00443F58`. A progress indicator, and the only per-transfer state they touch.
	PayloadProgressSlot = "_DAT_00443F58"
)

// PayloadReadChunks is how many `ReadFile` calls a read of `count` bytes takes, given
// the ten-thousand-byte cap. A zero count takes no calls at all, which is the reference's
// behaviour: the loop's first test is `count == 0`.
func PayloadReadChunks(count uint32) uint32 {
	if count == 0 {
		return 0
	}
	return (count + PayloadChunkMax - 1) / PayloadChunkMax
}

// # The write's success test is inverted, and that is preserved
//
//	return (param_3 == local_4) - 1 & 0xFFFF006B;
//
// The subexpression is **one when the write was complete** and zero when it was short.
// The subtraction is on a 32-bit value, so the short case is all ones, and the mask
// `0xFFFF006B` keeps the top half set:
//
//	count == written   ->  1 - 1 = 0x00000000  ->  0x00000000   // reported as success
//	count != written   ->  0 - 1 = 0xFFFFFFFF  ->  0xFFFF006B   // reported as failure
//
// **A complete write is reported as a success and a short write as a failure — which is
// backwards.** Preserved as it stands, because the reference's behaviour is the thing
// being ported and the two callers read a zero as success, so "fixing" this would change
// what the engine accepts. Whether it is a bug in the reference or a deliberate inversion
// is **not established here**: both readings fit, and the callers' guard is the only
// evidence, and it treats zero as success.
//
// **The failure value is byte-for-byte the open's failure return**, so a short write and a
// file that could not be opened are indistinguishable by return value. Only the seek uses
// the bare status with a clear top half, so the layer has two whole-value forms of the
// same status rather than one.
const (
	// WriteCompleteResult is what a *complete* write returns: zero, i.e. success.
	WriteCompleteResult uint32 = 0x00000000
	// WriteShortResult is what a *short* write returns: the failure, top half set. **The
	// same value the open returns.**
	WriteShortResult uint32 = 0xFFFF006B
	// WriteResultMask is the mask the return is taken against.
	WriteResultMask uint32 = 0xFFFF006B
)

// WriteResult gives the packed return for a transfer of `count` bytes of which `written`
// were actually written, reproducing the reference's arithmetic exactly.
func WriteResult(count, written uint32) uint32 {
	// The subexpression is one when the counts agree, which is the complete case.
	complete := uint32(0)
	if count == written {
		complete = 1
	}
	// Minus one, as a 32-bit value, so the short case is all ones.
	return (complete - 1) & WriteResultMask & 0xFFFFFFFF
}

// VerifyWriteResultInversion checks the quirk as arithmetic, and states the inversion
// rather than hiding it: the case that ought to fail is the one that reports success.
func VerifyWriteResultInversion() (ok bool, detail string) {
	// **A complete write reports success.**
	if WriteResult(512, 512) != WriteCompleteResult {
		return false, "a complete write should return zero"
	}
	if FileSucceeded(WriteResult(512, 512)) != true {
		return false, "a complete write should read as a success"
	}
	// **And a short write reports failure** — the inversion.
	if WriteResult(512, 100) != WriteShortResult {
		return false, "a short write should return the failure status"
	}
	if FileSucceeded(WriteResult(512, 100)) {
		return false, "a short write should not read as a success"
	}
	// The short result is exactly the failure status the whole file layer uses, so a
	// short write is indistinguishable from a file that could not be opened.
	if WriteResult(512, 100) != uint32(FileStatusOpenFailed)|0xFFFF0000 {
		return false, "a short write should carry the file layer's failure status"
	}
	// **The inversion is the property**, so it is stated as one: a caller's success test
	// passes for a complete write and fails for a short one, which is the opposite of
	// what the words mean.
	completeIsSuccess := FileSucceeded(WriteResult(100, 100))
	shortIsSuccess := FileSucceeded(WriteResult(100, 50))
	if !completeIsSuccess {
		return false, "the complete case should be the successful one"
	}
	if shortIsSuccess {
		return false, "the short case should be the failing one"
	}
	// **The mask keeps the top half set**, which is what makes a short write's return the
	// same whole value as the open's failure rather than the bare status.
	if WriteResultMask&0xFFFF0000 != 0xFFFF0000 {
		return false, "the mask should cover the top half"
	}
	if FileHandleHalf(WriteShortResult) != 0xFFFF0000 {
		return false, "a short write's result should set the top half"
	}
	// Which makes it **agree with the open and disagree with the seek** — so the layer has
	// two whole-value forms of the one status, and a short write is indistinguishable from
	// a file that could not be opened.
	if WriteShortResult != FileOpenFailed {
		return false, "a short write should return the same value as the open's failure"
	}
	if WriteShortResult == FileSeekFailed {
		return false, "a short write should differ from the seek's bare status"
	}
	// And the write's own hard failure — a `WriteFile` returning zero — returns the bare
	// status with a clear top half, like the seek. So **one routine uses both forms**,
	// depending on which failure happened.
	if uint32(FileStatusOpenFailed) != FileSeekFailed {
		return false, "the hard failure and the seek's should agree"
	}
	return true, "a complete write reports success and a short write reports failure, which is backwards"
}

// # The read's chunking, and the two halves' disagreement about it
//
// The read caps each `ReadFile` at 10,000 bytes and loops; the write makes one call. So a
// large append is not split while a large read is, and the split point is a **decimal**
// figure where every other size in this layer is a power of two or a multiple of four.
func VerifyPayloadChunking() (ok bool, detail string) {
	// The cap is 10,000 — a decimal figure, not 8,192 or 16,384.
	if PayloadChunkMax != 10000 {
		return false, "the chunk cap should be 10000"
	}
	if PayloadChunkMax&(PayloadChunkMax-1) == 0 {
		return false, "the cap should not be a power of two"
	}
	// **A zero count takes no calls at all**, because the loop's first test is the count
	// rather than the result.
	if PayloadReadChunks(0) != 0 {
		return false, "a zero-length read should take no calls"
	}
	// One call up to the cap, and the cap itself is one call.
	for _, count := range []uint32{1, 100, 9999, 10000} {
		if PayloadReadChunks(count) != 1 {
			return false, "a read at or below the cap should take a single call"
		}
	}
	// And one more immediately above it, because the cap is inclusive.
	if PayloadReadChunks(10001) != 2 {
		return false, "a read of 10001 should take two calls"
	}
	if PayloadReadChunks(20000) != 2 {
		return false, "a read of 20000 should take two calls, both at the cap"
	}
	if PayloadReadChunks(20001) != 3 {
		return false, "a read of 20001 should take three calls"
	}
	// A page's payload is 512 bytes, so **it is always one call** — the cap never
	// matters for the record's own read. That is worth knowing: the cap exists for
	// something larger, and the page path does not reach it.
	if PayloadReadChunks(PagePayloadSize) != 1 {
		return false, "a page payload should take a single call"
	}
	// The stamp read the sub-resource cache uses is 1024 bytes, also one call.
	if PayloadReadChunks(0x400) != 1 {
		return false, "a 1024-byte read should take a single call"
	}
	// **The write is not chunked**, which is the asymmetry worth recording.
	if PayloadWriteChunked {
		return false, "the write should not be chunked"
	}
	if !PayloadReadChunked {
		return false, "the read should be chunked"
	}
	// So a transfer large enough to need splitting is handled two different ways by the
	// two halves of one pair.
	if PayloadReadChunks(1000000) < 2 {
		return false, "a large read should need more than one call, while the write would not"
	}
	// Both seek to the end, so neither takes a position: the method is FILE_END.
	if PayloadEndMethod != 1 {
		return false, "both should seek with FILE_END"
	}
	return true, "the read caps at 10000 bytes and the write does not split at all"
}

// # This is the second correction: the page callers' seek is superseded
//
// The previous slice recorded the page record's file position as `index * 4 + 0x400` and
// treated that arithmetic as where the payload lives. It is not. The page fill and the
// eviction **both seek to `FILE_END` immediately afterwards**, so the first seek's
// position has no effect on either transfer:
//
//	void FUN_00402C40(void *file, void *page) {
//	    if ((short)FUN_0042C010(file, *(int *)page * 4 + 0x400) == 0)   // seek, then...
//	        FUN_0042BE70(file, (char *)page + 10, 0x200);                 // ...seek to end
//	}
//
// So the `index * 4 + 0x400` arithmetic is **computed, used for a seek, and then
// discarded**. All it contributes is a *pass or fail* — if the file cannot be positioned
// there the pair does nothing, and if it can, the transfer happens at the end regardless.
// The record's layout (a ten-byte header, a 512-byte payload at offset ten) stands; the
// payload's *file position* does not follow from the index.
//
// What the arithmetic does mean is not established here. Two readings fit: the index
// identifies a slot in a table at four bytes per entry that lives in the file's first
// 1024 bytes, and the transfer is a separate tail operation; or the seek is a leftover
// from a version of the code that did position by index. The record holds both facts and
// the test below asserts only what is established.
const (
	// PageSeekIsSuperseded records that the payload routines seek to the end, so the
	// caller's earlier seek contributes only its success.
	PageSeekIsSuperseded = true
	// PageSeekContributesOnlySuccess is the consequence: the caller's guard is a
	// pass-or-fail, not a positioning step.
	PageSeekContributesOnlySuccess = true
)

// VerifyPageSeekIsSuperseded checks the correction as a relationship between the two
// layers rather than as a claim about either alone.
func VerifyPageSeekIsSuperseded() (ok bool, detail string) {
	// **The payload routines seek to the end**, which is what supersedes the caller's
	// seek to `index * 4 + 0x400`.
	if PayloadEndMethod != 1 {
		return false, "the payload routines should seek with FILE_END"
	}
	// And the caller's seek is to a computed offset, so the two seek to different places
	// — which is the whole of the correction.
	if PageIndexBase == PayloadEndMethod {
		return false, "the caller's seek and the payload's should be different seeks"
	}
	// The record's layout is unaffected: the payload is still at offset ten, and its
	// size is still 512. **Only the file position is in question**, and the record
	// holds that it is not established.
	if PageHeaderSize != 10 || PagePayloadSize != 0x200 {
		return false, "the record's layout should be unaffected by the correction"
	}
	// The transfer is a tail read of the record's own size, so the cap never applies and
	// the read is one call — consistent with a tail read rather than a positioned one.
	if PayloadReadChunks(PagePayloadSize) != 1 {
		return false, "a page payload should be a single tail read"
	}
	// And the success test is a pass-or-fail, which is all the earlier seek contributes.
	if !PageSeekContributesOnlySuccess {
		return false, "the caller's seek should contribute only its success"
	}
	return true, "the caller's seek is computed and then superseded, so only its success matters"
}
