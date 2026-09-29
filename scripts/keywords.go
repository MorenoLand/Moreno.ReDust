package scripts

import "fmt"

// The control-flow keyword table that `FUN_0041DC00` scans, read out of the
// reference image.
//
// The decompilation renders the loop as:
//
//	sVar2 = 0x1F;
//	ppuVar3 = &PTR_s_opencast_0045d738;
//	do {
//	    if (equalASCII(*ppuVar3, token)) { latch = 2; return 1; }
//	    sVar2--;
//	    ppuVar3 += 6;
//	} while (0 < sVar2);
//
// and its rendering of the step is ambiguous, because `ppuVar3` is typed
// `undefined **` and Ghidra emitted a byte-pointer cast. The disassembly settles
// it, and against the decompiler's arithmetic:
//
//	MOV  BX, 0x1f          ; 31 comparisons
//	MOV  EDI, 0x45d738     ; the table
//	  MOV  EAX, [EDI]      ; read one pointer
//	  ...
//	  DEC  BX
//	  ADD  EDI, 0x6        ; six BYTES, not six elements
//	  TEST BX, BX
//	  JG   loop
//
// **The step is six bytes, so the 31 reads overlap and the table is sixteen
// twelve-byte records holding two pointers each.** The first pointer of each record
// sits at the record's base and the second at base+6, separated by two zero bytes.
// Read that way, all 31 reads decode to a keyword and all 31 are distinct, which is
// the check that confirms the layout: a 24-byte stride or a 16-entry table would
// have run off the end into the string pool.
const (
	// KeywordTableAddress is where the table sits, 0x0045D738, four bytes of
	// padding after the string pool's last terminator.
	KeywordTableAddress uint32 = 0x0045D738
	// KeywordRecordSize is the record stride, 12 bytes, holding two pointers.
	KeywordRecordSize = 12
	// KeywordPointerOffset is where the first of a record's two pointers sits, at
	// the record base.
	KeywordPointerOffset = 0
	// KeywordPadBytes is the two zero bytes between a record's two pointers.
	KeywordPadBytes = 2
	// KeywordSecondPointerOffset is where the second pointer sits, at base+6.
	KeywordSecondPointerOffset = 6
	// KeywordRecordCount is how many records the 31 reads cover: two reads per
	// record, so sixteen records with the last one's second pointer unread.
	KeywordRecordCount = 16
	// KeywordScanReads is how many comparisons the reference makes, 31.
	KeywordScanReads = 31
	// The latch word's offset within the cursor, in shorts, from `MOV word ptr
	// [ESI + 6]`. Byte offset 6 is short index 3.
	KeywordLatchOffset = 3
)

// ControlFlowKeyword identifies one of the thirty-one keywords. The values are the
// scan order, which is the order the table is read and therefore the order a match
// is found in; the first match wins and the scan stops, so the order is observable
// only if two entries could ever compare equal, which they cannot.
type ControlFlowKeyword uint8

// The thirty-one keywords, in scan order. Each is either the first or the second
// pointer of one of the sixteen records, and the pairing is the reference's own:
//
//	opencast / openactor     closecast / closeactor
//	openflat / openstage     closeflat / closestage
//	endwalk / endturn        endball / endloop
//	openshop / openprop      closeshop / closeprop
//	openpuppet / closepuppet menustate / boot
//	idle / menuselect        keydown / keyrepeat
//	mousedown / openset      openfloor / openscene
//	closeset / closefloor    closescene
//
// Most records pair two verbs over the same subsystem, but two do not:
// `openpuppet` is paired with `closepuppet`, and `endwalk` and `endball` are paired
// with `endturn` and `endloop` rather than with each other. The pairing is a layout
// choice, not a semantic grouping, so it is recorded and not relied on.
const (
	// KeywordOpenCast is "opencast".
	KeywordOpenCast ControlFlowKeyword = iota
	KeywordOpenActor
	KeywordCloseCast
	KeywordCloseActor
	KeywordOpenFlat
	KeywordOpenStage
	KeywordCloseFlat
	KeywordCloseStage
	KeywordEndWalk
	KeywordEndTurn
	KeywordEndBall
	KeywordEndLoop
	KeywordOpenShop
	KeywordOpenProp
	KeywordCloseShop
	KeywordCloseProp
	KeywordOpenPuppet
	KeywordClosePuppet
	KeywordMenuState
	KeywordBoot
	KeywordIdle
	KeywordMenuSelect
	KeywordKeyDown
	KeywordKeyRepeat
	KeywordMouseDown
	KeywordOpenSet
	KeywordOpenFloor
	KeywordOpenScene
	KeywordCloseSet
	KeywordCloseFloor
	KeywordCloseScene
	// KeywordCount is how many keywords the scan can match.
	KeywordCount = 31
)

// controlFlowKeywordText is the verified table, in scan order. Every entry was read
// from the image and every one decodes to a short printable name.
var controlFlowKeywordText = [KeywordCount]string{
	KeywordOpenCast:     "opencast",
	KeywordOpenActor:    "openactor",
	KeywordCloseCast:    "closecast",
	KeywordCloseActor:   "closeactor",
	KeywordOpenFlat:     "openflat",
	KeywordOpenStage:    "openstage",
	KeywordCloseFlat:    "closeflat",
	KeywordCloseStage:   "closestage",
	KeywordEndWalk:      "endwalk",
	KeywordEndTurn:      "endturn",
	KeywordEndBall:      "endball",
	KeywordEndLoop:      "endloop",
	KeywordOpenShop:     "openshop",
	KeywordOpenProp:     "openprop",
	KeywordCloseShop:    "closeshop",
	KeywordCloseProp:    "closeprop",
	KeywordOpenPuppet:   "openpuppet",
	KeywordClosePuppet:  "closepuppet",
	KeywordMenuState:    "menustate",
	KeywordBoot:         "boot",
	KeywordIdle:         "idle",
	KeywordMenuSelect:   "menuselect",
	KeywordKeyDown:      "keydown",
	KeywordKeyRepeat:    "keyrepeat",
	KeywordMouseDown:    "mousedown",
	KeywordOpenSet:      "openset",
	KeywordOpenFloor:    "openfloor",
	KeywordOpenScene:    "openscene",
	KeywordCloseSet:     "closeset",
	KeywordCloseFloor:   "closefloor",
	KeywordCloseScene:   "closescene",
}

// Text is the keyword's spelling.
func (k ControlFlowKeyword) Text() (string, error) {
	if int(k) >= len(controlFlowKeywordText) {
		return "", fmt.Errorf("control-flow keyword %d has no text", uint8(k))
	}
	return controlFlowKeywordText[k], nil
}

// Record is which of a record's two pointers the keyword sits in.
func (k ControlFlowKeyword) Record() int { return int(k) / 2 }

// PointerOffset is where in its twelve-byte record the keyword's pointer sits, one
// of KeywordPointerOffset or KeywordSecondPointerOffset.
func (k ControlFlowKeyword) PointerOffset() int {
	if int(k)%2 == 0 {
		return KeywordPointerOffset
	}
	return KeywordSecondPointerOffset
}

// SlotAddress is the address the reference reads for this keyword, which is the
// table base plus six bytes per scan step.
func (k ControlFlowKeyword) SlotAddress() uint32 {
	return KeywordTableAddress + uint32(k)*KeywordSecondPointerOffset
}

// ControlFlowKeywordFor resolves a name to its keyword, comparing with the same
// case fold every other name in the engine uses. The comparison is the verified
// 256-entry table at 0x0045DDF0, so "OPENCast" matches.
//
// **None of the thirty-one is a dispatched opcode.** `opencastfile` 12013 is, but
// `opencast` alone is not, and neither are `idle`, `boot`, `menustate` or
// `keyrepeat`. These words are recognised by name against a hardcoded table, on a
// path that never consults either dispatcher, which is why a search of the port's
// opcode names finds none of them and that absence is correct rather than a gap.
func ControlFlowKeywordFor(name string) (ControlFlowKeyword, bool) {
	for i, text := range controlFlowKeywordText {
		if equalASCIIFold(name, text) {
			return ControlFlowKeyword(i), true
		}
	}
	return 0, false
}

// IsControlFlowKeyword reports whether a name is one of the thirty-one.
func IsControlFlowKeyword(name string) bool {
	_, ok := ControlFlowKeywordFor(name)
	return ok
}

// KeywordLatch is the one-shot mechanism `FUN_0041DC00` uses, verified in full:
//
//	if (eval(token, &text) != 0) { latch = 1; return 0; }
//	if (latch != 0) return latch - 1;
//	for each of the 31 slots:
//	    if (equalASCII(slot, text)) { latch = 2; return 1; }
//	latch = 1;
//	return 0;
//
// The latch is a one-way flag at cursor offset 6, and **it does not decay**. The
// disassembly is explicit:
//
//	MOV  AX, [ESI + 6]
//	TEST AX, AX
//	JZ   scan
//	DEC  AX          ; AX = latch - 1 ...
//	RET              ; ... and the value is never stored back
//
// So a latched state is read, decremented in the register and returned, while the
// stored latch keeps its original value. The effect is that **the scan runs exactly
// once per latch**: a keyword answers 1 on the call that scanned, and every later
// call answers 1 again without rescanning; a non-keyword answers 0 on the call that
// scanned and 0 on every later call. The latch's purpose is to stop the scan
// re-running on every call while the caller consumes one statement, not to make the
// answer transient.
//
// The two failure paths both latch 1, so an evaluation failure and a non-keyword are
// indistinguishable to a later caller, which is why the latch's value is not a
// status.
type KeywordLatch uint16

const (
	// LatchClear is the initial state, where the scan runs.
	LatchClear KeywordLatch = 0
	// LatchNotAKeyword is set when the token is not one of the thirty-one, or when
	// evaluating it failed. Later calls answer 0.
	LatchNotAKeyword KeywordLatch = 1
	// LatchFound is set when a keyword matched. Later calls answer 1.
	LatchFound KeywordLatch = 2
)

// Step reports what this call answers, reproducing the reference's order: a latched
// state is decremented in a register and returned without scanning and without
// changing the stored latch, and only a clear latch runs the scan.
//
// matches is the result of the scan, which the caller performs by consulting
// ControlFlowKeywordFor. It is ignored when the latch is already set, which is what
// keeps the scan from re-running.
func (l *KeywordLatch) Step(matches bool) int {
	if *l != LatchClear {
		return int(*l) - 1
	}
	if matches {
		*l = LatchFound
		return 1
	}
	*l = LatchNotAKeyword
	return 0
}

// Found reports whether the latch is in the found state.
func (l KeywordLatch) Found() bool { return l == LatchFound }
