package scripts

import "fmt"

// The block-execution machinery: `FUN_0041C890`, which finds a block body and
// runs it, and `FUN_0041C9A0`, which walks a frame's block list. These were the last
// two functions the ledger had carried as unmoved since the first conversion.

// The frame layout, from `FUN_0041C890`. The frame cursor advances by `0x27`
// shorts, so a frame is **78 bytes**, and the function first scans forward counting
// parentheses to find where the body starts:
//
//	if (FUN_0042C460() < 0x800) return 0x2C;
//	depth = 0;
//	for (p = body; ; p += 4) {
//	    if (*p == 0x0FB2) depth++;                  // "("
//	    if (*p == 0x0FB3) { depth--; if (depth < 1) { ...the body... } }
//	    if (*p == 6) break;                          // kind 6 ends the scan
//	    if (*p == 0) return 2;                       // a zero record is malformed
//	}
//	return 2;
//
// Three details are load-bearing. The **depth is counted, not merely matched
// once**, so a body containing nested parentheses is found correctly. **Kind 6 ends
// the scan** and anything else falls through to the same malformed status 2, so an
// unterminated body and a zero record are indistinguishable to a caller. And the
// **`0x800` stack check and its `0x2C` status are the same ones the expression
// evaluator uses**, which is the clearest evidence that `FUN_0042C460` is a shared
// stack probe rather than a per-function check.
const (
	// ContextFrameSize is the frame stride, 0x27 shorts.
	ContextFrameSize = 0x27
	// BlockListOffset is where a frame keeps its block list pointer, at frame
	// offset 0x14.
	BlockListOffset = 0x14
	// FrameStatusOffset is where a frame records a failure code, at frame offset
	// 0x0C, in shorts.
	FrameStatusOffset = 0x0C
	// BlockRecordBytes is a block's size, four shorts, since the walk advances by 4.
	BlockRecordBytes = 8
	// BlockListEndSentinel is 0x0FA1, the value that terminates a frame's block
	// list. A block whose first word is zero also ends the list, reporting 4.
	BlockListEndSentinel uint16 = 0x0FA1
	// FrameScanEndKind is kind 6, which ends the body scan.
	FrameScanEndKind uint16 = 6
	// FrameStatusMalformed is status 2, for an unterminated body or a zero record.
	FrameStatusMalformed uint16 = 2
	// FrameStatusEndOfBody is the value the block runner returns when it runs out
	// of blocks. It is 4, and the caller treats it as "reached the end" rather than
	// as a failure.
	FrameStatusEndOfBody uint16 = 4
	// FrameCommit is the call that records a frame's outcome, FUN_00418520.
	FrameCommit = "FUN_00418520"
	// FrameRunBlock is the single-block runner, FUN_0041C9A0.
	FrameRunBlock = "FUN_0041C9A0"
	// FrameRunBlockInner is the call that executes one block, FUN_0041CA70.
	FrameRunBlockInner = "FUN_0041CA70"
	// FrameBranch is the conditional control-flow range, FUN_0041CC20.
	FrameBranch = "FUN_0041CC20"
	// FrameNextBlock computes a block's successor, FUN_0041DB90.
	FrameNextBlock = "FUN_0041DB90"
	// FrameAtEnd asks whether the cursor has reached the end, FUN_0041DC00.
	FrameAtEnd = "FUN_0041DC00"
	// FramePool and FramePoolFree are the value pool the block runner takes and
	// returns, FUN_00426D80 and FUN_004272C0.
	FramePool      = "FUN_00426D80"
	FramePoolFree  = "FUN_004272C0"
)

// FindBlockBody scans for the end of a block body by counting parentheses, which
// is what FUN_0041C890 does before running anything. kinds is the record-kind
// stream starting at the body's opening. It returns the index of the first record
// after the body's closing paren and the number of records the body spanned.
//
// A body is found when the depth falls below one, so a nested pair is skipped
// correctly. A kind 6 or a zero record ends the scan, and either way the reference
// reports status 2, so a truncated body and a stray zero are indistinguishable.
func FindBlockBody(kinds []uint16) (afterBody int, depth int, status uint16, err error) {
	depth = 0
	for i, kind := range kinds {
		switch kind {
		case ValueKindOpenParen:
			depth++
			continue
		case ValueKindCloseParen:
			depth--
			if depth < 1 {
				// The reference computes the span as (position - start + 8) >> 3,
				// that is the closing record plus one more, in eight-byte record
				// units, so the body ends just past the closing paren.
				return i + 1, depth, 0, nil
			}
			continue
		case FrameScanEndKind:
			return 0, depth, FrameStatusMalformed,
				fmt.Errorf("the body scan hit kind %d at record %d without closing", FrameScanEndKind, i)
		case 0:
			return 0, depth, FrameStatusMalformed,
				fmt.Errorf("the body scan hit a zero record at index %d", i)
		}
	}
	return 0, depth, FrameStatusMalformed, fmt.Errorf("the body scan ran out of records at depth %d", depth)
}

// BlockListWalk is the state of a walk over a frame's block list, reproducing
// FUN_0041C9A0's loop. Blocks are four shorts each and the list ends at the
// `0x0FA1` sentinel, or at a block whose first word is zero.
type BlockListWalk struct {
	// Blocks is the block stream, addressed in four-short steps.
	Blocks []uint16
	// Index is the current block's index, which the reference also records in the
	// frame at frame offset 0x0C.
	Index int
}

// Next advances to the next block. It reports the block's index, whether the list
// has ended, and the index the reference would jump to.
//
// The reference's loop order is: run the current block; if it succeeded, ask for
// the next offset; if that offset is non-negative, branch to it; otherwise ask the
// block for its successor and jump there. A block whose first word is zero ends the
// list with status 4.
func (w *BlockListWalk) Next() (index int, ended bool, err error) {
	for {
		base := w.Index * 4
		if base+1 > len(w.Blocks) {
			return w.Index, true, nil
		}
		if w.Blocks[base] == BlockListEndSentinel {
			return w.Index, true, nil
		}
		if w.Blocks[base] == 0 {
			// A zero block ends the list, reporting end-of-body.
			return w.Index, true, nil
		}
		if base+4 > len(w.Blocks) {
			return w.Index, true, fmt.Errorf("block %d at +%d is truncated", w.Index, base)
		}
		return w.Index, false, nil
	}
}

// Advance moves to block target, which the reference computes as
// `position + offset * 4`, so the offset is counted in blocks.
func (w *BlockListWalk) Advance(target int) {
	w.Index = target
}

// FrameCursor is the frame cursor, which advances by ContextFrameSize shorts.
type FrameCursor struct {
	// Index is the frame number, starting at zero.
	Index int
	// Finished is the per-frame stop flag the reference reads through `*param_1`.
	Finished bool
	// Status is the failure code the frame records at frame offset 0x0C.
	Status uint16
}

// Advance moves to the next frame, which the reference does by adding
// ContextFrameSize shorts. It reports whether the walk has ended, since the
// reference stops when the frame's stop flag is set.
func (c *FrameCursor) Advance() (more bool) {
	c.Index++
	return !c.Finished
}

// ResourceRelease reproduces `FUN_004188C0`, the reference-counted resource
// release. Verified:
//
//	if (FUN_00418A60(handle, &slot) != 0) {
//	    count = table[slot * 0x20 + 0x10] - 1;
//	    table[slot * 0x20 + 0x10] = count;
//	    if (count < 0) FUN_0042C470(0x6E, 0x1462);      // the count went negative
//	    return;
//	}
//	FUN_0042C470(0x6E, 0x1463);                        // no such slot
//
// So the count is **decremented unconditionally** and going below zero is a
// diagnostic, not a no-op. That is a correction to the `Release` in sendto.go,
// which treated a release on an already-zero entry as a harmless no-op; the
// reference treats it as a bug worth reporting, and the difference is exactly the
// case that would otherwise hide a double release.
const (
	// ResourceSlotLookup is the slot finder the release calls, FUN_00418A60.
	ResourceSlotLookup = "FUN_00418A60"
	// ResourceUnderflowDiag is raised when a release drives the count negative.
	ResourceUnderflowDiag uint16 = 0x1462
	// ResourceNoSlotDiag is raised when the handle has no cache entry.
	ResourceNoSlotDiag uint16 = 0x1463
)

// ReleaseResource drops a reference, reproducing FUN_004188C0. found reports
// whether the handle has a cache entry. A release that drives the count below zero
// is reported with the native diagnostic rather than silently tolerated.
func ReleaseResource(entries []EntityResourceEntry, slot int, found bool) (remaining int, status uint16, err error) {
	if !found || slot < 0 || slot >= len(entries) {
		return 0, ResourceNoSlotDiag, fmt.Errorf(
			"releasing resource %d, which has no cache entry (FUN_0042C470(0, %#x))", slot, ResourceNoSlotDiag)
	}
	entry := &entries[slot]
	if int(entry.RefCount) == 0 {
		// The reference would decrement to -1 and raise the underflow diagnostic.
		return -1, ResourceUnderflowDiag, fmt.Errorf(
			"releasing resource %d, whose count is already zero (FUN_0042C470(0, %#x))",
			slot, ResourceUnderflowDiag)
	}
	entry.RefCount--
	return int(entry.RefCount), 0, nil
}
