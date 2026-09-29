package scripts

import "fmt"

// The single-block executor's body syntax, and the successor-offset scheme that
// closes control flow inside a block list. Together these complete what can be said
// about block execution without running anything: the frame runner and the list
// walker were converted previously, and these are the two remaining pieces they
// call.
//
// **Every offset in this file is in shorts, not bytes**, because the reference walks
// these structures with `short *` pointers. An earlier draft mixed the two and
// produced a body walk that read the wrong records; the names below all say
// `Shorts` so the unit cannot be confused again.

// A block is `0x18` bytes. From `FUN_0041CA70`, the executor:
//
//	operand expression   at block + 0x00
//	the body's "("        at block + 0x10
//	the body itself       at block + 0x18
//
// so the record proper holds the operand and the body's opener, and the body
// follows immediately after the record.
const (
	// BlockRecordSize is the block record's size in bytes.
	BlockRecordSize = 0x18
	// BlockOperandOffset is where the block's operand expression starts, in bytes.
	BlockOperandOffset = 0x00
	// BlockOpenerOffset is where the body's opening paren sits, in bytes.
	BlockOpenerOffset = 0x10
	// BlockBodyOffset is where the body begins, in bytes, which is immediately after
	// the record ends.
	BlockBodyOffset = 0x18
	// BlockOpenerShorts is the body's opener offset expressed in shorts, 8.
	BlockOpenerShorts = BlockOpenerOffset / 2
)

// IsBlockRecord reports whether a buffer is long enough to hold the block record
// itself. The body follows the record and is a separate region, so this does not
// require room for the body.
func IsBlockRecord(record []byte) bool {
	return len(record) >= BlockRecordSize
}

// BlockMatch is the executor's decision about a block, which is a **conditional
// jump**. Verified:
//
//	u = eval(block + 0x00, &A);        // the block's operand
//	v = eval(context, &B);             // the incoming value
//	if (!equalASCII(A, B)) { *offset = -1; return; }   // no jump
//	if (*(short *)(block + 0x10) != 0x0FB2) return 2;
//	if (context[8] != 0x0FB2) return 2;
//	... run the body ...
//
// So a block that does not match produces **-1, meaning "fall through, take no
// branch"** — not a failure. That is the key property: a block list is a chain of
// guarded branches, and a block whose guard is false simply does not apply. The
// value -1 is distinguishable from every real block offset, which are non-negative.
const (
	// NoBranch is the offset a non-matching block produces, -1.
	NoBranch int32 = -1
	// BlockNoOpener is the status when either the block's or the context's body
	// record is not the paren token. Both sides share it, so a caller cannot tell
	// which was malformed.
	BlockNoOpener uint16 = 2
)

// BlockDecision is what the executor decided about one block.
type BlockDecision uint8

const (
	// BlockDidNotMatch means the guard was false and no branch is taken. The
	// reference reports -1, which is not a failure.
	BlockDidNotMatch BlockDecision = iota
	// BlockRunsBody means the guard was true and the body should run.
	BlockRunsBody
)

// MatchBlock reproduces the executor's first two steps: compare the block's operand
// with the incoming value, and if they match require both the block's and the
// context's body records to be open parens. It returns the branch offset, which is
// NoBranch when the guard is false.
func MatchBlock(block []byte, contextShorts []uint16, operandEqual bool) (offset int32, decision BlockDecision, status uint16, err error) {
	// A non-matching block falls through, and this test runs first, so a malformed
	// body on a non-matching block is never looked at.
	if !operandEqual {
		return NoBranch, BlockDidNotMatch, 0, nil
	}
	// The guard passed, so both opener records are required.
	if len(block) < BlockRecordSize {
		return NoBranch, BlockRunsBody, BlockNoOpener,
			fmt.Errorf("the block is %d bytes, shorter than the %d byte record", len(block), BlockRecordSize)
	}
	if word16At(block, BlockOpenerOffset) != ValueKindOpenParen {
		return NoBranch, BlockRunsBody, BlockNoOpener,
			fmt.Errorf("the block's body opener at +%#x is %#x, want the paren token",
				BlockOpenerOffset, word16At(block, BlockOpenerOffset))
	}
	if len(contextShorts) <= BlockOpenerShorts {
		return NoBranch, BlockRunsBody, BlockNoOpener,
			fmt.Errorf("the context is %d shorts, too short for an opener at short %d",
				len(contextShorts), BlockOpenerShorts)
	}
	if contextShorts[BlockOpenerShorts] != ValueKindOpenParen {
		return NoBranch, BlockRunsBody, BlockNoOpener,
			fmt.Errorf("the context's opener at short %d is %#x, want the paren token",
				BlockOpenerShorts, contextShorts[BlockOpenerShorts])
	}
	return 0, BlockRunsBody, 0, nil
}

// word16At reads a little-endian 16-bit word from a buffer, or 0 if it is too short.
// Every offset used here is even, so this models the reference's aligned word reads.
func word16At(data []byte, offset int) uint16 {
	if offset < 0 || offset+2 > len(data) {
		return 0
	}
	return uint16(data[offset]) | uint16(data[offset+1])<<8
}

// The block body is a **list of `value , value` pairs compared for equality**, which
// is a multi-way branch: "if the incoming value is any of these, take this path".
// Verified `FUN_0041CA70`'s loop, with offsets in shorts as the reference writes them:
//
//	psVar6 = body;                     // the block side
//	psVar7 = context + 8;              // the context side, past its opener
//	for (;;) {
//	    status = evaluate(*psVar6, &A);        // the left-hand value
//	    if (status) return status;
//	    psVar6 += 4;                            // the terminator is four shorts on
//	    status = evaluate(*psVar7, &B);         // the right-hand value
//	    if (status) return status;
//	    psVar7 += consumed * 4;                 // VARIABLE, sized by the operand
//	    status = compare(A, B);
//	    if (status) return status;
//	    if (*psVar6 != 0x0FB4) {                // a comma continues ...
//	        if (*psVar6 != 0x0FB3) return 2;    // a close paren ends the list
//	        goto end_of_body;
//	    }
//	    psVar6 += 4;                            // past the comma
//	    if (*psVar7 != 0x0FB4) return 0x1C;     // the context needs its comma too
//	    psVar7 += 4;                            // past it
//	}
//
// Two things are worth stating. The list ends at a **close paren** on the left side,
// and the right side is the *same shape* against the context's records, so each pair
// is `blockValue , contextValue`. And the comma is required on **both** sides: a
// missing one on the context side is **`0x1C`**, the general missing-comma status,
// which is now its **fourth** independent appearance and strengthens the case that it
// belongs to the shared two-argument evaluator rather than to any one builtin.
//
// The block side's advance is a fixed eight shorts per pair, while the context
	// side's is **variable**: it is the evaluated operand's consumed record count
	// times four shorts. That asymmetry is why the advance is a parameter on the
	// context side rather than a constant, and a port that fixed both would mis-walk
	// any body whose operands are longer than one record.
const (
	// BlockTerminatorShorts is how far past a value the terminator sits, 4 shorts.
	BlockTerminatorShorts = 4
	// BlockAdvanceShorts is how far the block side advances per pair, 8 shorts, from
	// `psVar5 += 8`.
	BlockAdvanceShorts = 8
	// BlockPastCommaShorts is how far the block side moves past a comma, 4 shorts,
	// from `psVar6 += 4`.
	BlockPastCommaShorts = 4
	// BlockContextStartShorts is the short index of the context side's first record,
	// which is past its opener at short 8.
	BlockContextStartShorts = BlockOpenerShorts
	// BlockContextPerRecord is the multiplier from an operand's consumed record count
	// to the context side's advance.
	BlockContextPerRecord = 4
)

// BlockBodyWalk is the position of the executor's two-sided walk over a body.
//
// It is distinct from `BlockListWalk`, which walks a *frame's* list of blocks. This
// one walks *inside* one block, across the `value , value` pairs of its body, and it
// keeps two offsets because the block side and the context side are separate record
// streams advancing by different rules.
type BlockBodyWalk struct {
	// BlockAt is the block side's current record index, counting from the body's
	// first record.
	BlockAt int
	// ContextAt is the context side's current record index, counting from its own
	// first record.
	ContextAt int
}

// Step advances one pair. It reports whether the list continues, and the status when
// it does not: 0x1C for a missing comma on the context side and 2 for a malformed
// terminator.
//
// blockKinds is the block side's kind stream from the body's first record, and
// contextKinds the context side's from its own. contextAdvanceShorts is how far the
// context side moves past its value, which the reference derives from the evaluated
// operand's consumed record count times four; it is passed in because that count is
// not knowable here.
func (w *BlockBodyWalk) Step(blockKinds, contextKinds []uint16, contextAdvanceShorts int) (more bool, status uint16, err error) {
	if w.BlockAt >= len(blockKinds) {
		return false, BlockNoOpener,
			fmt.Errorf("the block side is at record %d, past its %d records", w.BlockAt, len(blockKinds))
	}
	if w.ContextAt >= len(contextKinds) {
		return false, BlockNoOpener,
			fmt.Errorf("the context side is at record %d, past its %d records", w.ContextAt, len(contextKinds))
	}

	// The record after the block's value decides: a comma continues, a close paren
	// ends the list, and anything else is malformed.
	terminator := w.BlockAt + BlockTerminatorShorts
	if terminator >= len(blockKinds) {
		return false, BlockNoOpener,
			fmt.Errorf("the block side has no terminator record at index %d", terminator)
	}
	switch blockKinds[terminator] {
	case ValueKindCloseParen:
		return false, 0, nil

	case SendToCommaKind:
		// The context side's own comma is required, at the offset its value
		// advanced to. contextAdvanceShorts is the reference's own `consumed * 4`,
		// so it already covers the value's extent and nothing is added to it here.
		contextComma := w.ContextAt + contextAdvanceShorts
		if contextComma >= len(contextKinds) {
			return false, StatusMissingComma,
				fmt.Errorf("the context side has no comma record at index %d", contextComma)
		}
		if contextKinds[contextComma] != SendToCommaKind {
			return false, StatusMissingComma, fmt.Errorf(
				"the context side expects a comma at index %d and found kind %d",
				contextComma, contextKinds[contextComma])
		}
		w.BlockAt += BlockAdvanceShorts
		w.ContextAt = contextComma + BlockPastCommaShorts
		return true, 0, nil

	default:
		return false, BlockNoOpener, fmt.Errorf(
			"the block side expects a comma or a close paren at index %d and found kind %d",
			terminator, blockKinds[terminator])
	}
}

// The successor offset, from `FUN_0041DB90`. This is what makes a block list a chain
// rather than a sequence, and it has a **cache**:
//
//	if (*block != 0x0FA1) FUN_0042C470(0, 0x10CC);   // must be the sentinel
//	cached = *(int *)(block + 1);                     // a 32-bit field at +2 bytes
//	if (cached == 0) {
//	    scan forward four shorts at a time until a sentinel or a zero block
//	    if (a zero block) { cached = -1; *out = -1; return 0; }
//	    cached = (found - start) >> 3;
//	    *(int *)(block + 1) = cached;                  // CACHE it into the sentinel
//	    *out = cached;
//	    return 0;
//	}
//	*out = cached;
//	return cached & 0xFFFF0000;
//
// So the sentinel block is **not** merely a terminator: it is a jump target that
// **caches the distance to the next sentinel the first time it is fallen into**. A
// later fall-through reuses the stored offset instead of rescanning.
//
// The distance is measured in **eight-byte units**, since it is a byte difference
// divided by eight. Records are four shorts, that is eight bytes, so the distance is
// simply the number of records advanced — which is what makes it directly usable as
// the caller's `position += offset * 4` step in shorts.
//
// The two paths return differently, and that difference is observable rather than an
// artifact. The computed path returns **0** and the caller continues the walk, while
// the explicit path returns the offset in the **high half of the status word**, and
// `FUN_0041C9A0` stops the walk on any non-zero status. So a cached offset ends the
// walk and a freshly computed one does not. That is a real asymmetry; the arithmetic
// is transcribed as written and the consequence recorded rather than rationalised,
// since which path a given branch takes is not established here.
const (
	// SuccessorSentinel is the value the successor helper requires, 0x0FA1.
	SuccessorSentinel uint16 = 0x0FA1
	// SuccessorCacheShorts is the short index of the sentinel's cached distance,
	// since the field is a 32-bit word at byte +2.
	SuccessorCacheShorts = 1
	// SuccessorNotSentinelDiag is raised when the helper is handed a block that is
	// not the sentinel, 0x10CC.
	SuccessorNotSentinelDiag uint16 = 0x10CC
	// SuccessorEndOfList is the offset the helper reports when a zero block
	// terminates the scan, -1, which is the same value a non-matching block
	// produces for "no branch".
	SuccessorEndOfList int32 = -1
	// SuccessorRecordShorts is a block's size in shorts, four.
	SuccessorRecordShorts = 4
	// SuccessorUnitBytes is the unit the distance is measured in, eight.
	SuccessorUnitBytes = 8
)

// SuccessorOutcome is what the successor helper decided.
type SuccessorOutcome uint8

const (
	// SuccessorComputed means the distance was scanned for, cached into the sentinel,
	// and returned with a zero status, so the walk continues.
	SuccessorComputed SuccessorOutcome = iota
	// SuccessorCached means a stored distance was returned, and the caller stops
	// because the status word is non-zero.
	SuccessorCached
	// SuccessorEnd means a zero block, or the end of the region, ended the scan,
	// with offset -1 and a zero status.
	SuccessorEnd
)

// Successor reproduces FUN_0041DB90. blocks is the region as shorts, sentinelAt is
// the sentinel's record index, and the cached distance is written back into the
// sentinel's own record so the cache is visible to the caller and to the test.
//
// The sentinel's record must be long enough for its 32-bit cache field, which spans
// two shorts, and that is checked rather than assumed: the reference does not check
// it, so a short region would read past there.
func Successor(blocks []uint16, sentinelAt int) (outcome SuccessorOutcome, offset int32, status uint16, err error) {
	base := sentinelAt * SuccessorRecordShorts
	// The whole record must be present: the first short for the sentinel value and
	// two more for the 32-bit cache field.
	if base < 0 || base+SuccessorRecordShorts > len(blocks) {
		return SuccessorEnd, 0, SuccessorNotSentinelDiag, fmt.Errorf(
			"sentinel record %d spans shorts %d..%d, past the %d short region (FUN_0042C470(0, %#x))",
			sentinelAt, base, base+SuccessorRecordShorts-1, len(blocks), SuccessorNotSentinelDiag)
	}
	if blocks[base] != SuccessorSentinel {
		return SuccessorEnd, 0, SuccessorNotSentinelDiag, fmt.Errorf(
			"record %d holds %#x, not the successor sentinel %#x (FUN_0042C470(0, %#x))",
			sentinelAt, blocks[base], SuccessorSentinel, SuccessorNotSentinelDiag)
	}

	cached := int32(uint32(blocks[base+SuccessorCacheShorts]) |
		uint32(blocks[base+SuccessorCacheShorts+1])<<16)
	if cached != 0 {
		// The explicit path returns the offset in the status word's high half, which
		// is what makes the caller stop.
		return SuccessorCached, cached, uint16(uint32(cached) >> 16), nil
	}

	// No stored distance: scan forward a record at a time for the next sentinel or a
	// zero block.
	for step := 1; ; step++ {
		next := base + step*SuccessorRecordShorts
		if next >= len(blocks) {
			// No sentinel found before the region ended.
			return SuccessorEnd, SuccessorEndOfList, 0, nil
		}
		switch blocks[next] {
		case 0:
			// A zero block ends the list, and nothing is cached.
			return SuccessorEnd, SuccessorEndOfList, 0, nil
		case SuccessorSentinel:
			// The distance is a byte difference shifted right by three, and records
			// are exactly eight bytes, so it is the record count advanced.
			distance := int32(step * SuccessorRecordShorts * 2 / SuccessorUnitBytes)
			blocks[base+SuccessorCacheShorts] = uint16(distance)
			blocks[base+SuccessorCacheShorts+1] = uint16(distance >> 16)
			return SuccessorComputed, distance, 0, nil
		}
	}
}
