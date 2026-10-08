package native

import (
	. "redust/scripts"
	"fmt"
)

// The prop and shop string region, at 0x0045D960..0x0045DA02, found by following
// `sendtoprop` and `sendtoshop` to the labels their child frames use.
//
// **This region follows the transcribed pool's convention, not the actor region's.**
// It is separated by single NULs like the pool, so the byte at the address Ghidra
// names is the previous string's terminator and each entry sits one byte after it:
// `&DAT_0045D994` resolves to `0x0045D995`, which is `"Prop Script: "`. So the engine
// has three string regions and **two different addressing conventions** among them —
// the actor region at `0x0045D001..0x0045D14D` names each entry's own first byte, while
// this one and the pool name the byte before it. A test asserts the convention per
// region rather than globally, since applying either one everywhere would truncate
// half the labels.
const (
	// PropShopRegionFirst is the region's first printable string.
	PropShopRegionFirst uint32 = 0x0045D960
	// PropShopRegionLast is its last string, "current". **This boundary was corrected**:
	// it was previously recorded as 0x0045DA01, which is past the end of the region, and
	// the six short names and the value table that follow the save-game messages had not
	// been read at all. A test failing on the movie player's label address is what exposed
	// it — the player's other label lies beyond this boundary, so the region had to extend.
	PropShopRegionLast uint32 = 0x0045DA6D
	// PropShopRegionSeparator is how many NUL bytes separate strings here, one, the
	// same as the transcribed pool and unlike the actor region.
	PropShopRegionSeparator = 1
	// PropShopRegionTextFollowsName records that this region's text is one byte after
	// the address Ghidra names, as in the pool.
	PropShopRegionTextFollowsName = true
)

// The labels the prop and shop members' child frames use, verified from the two
// addresses the handlers pass to FUN_00427FC0.
const (
	// PoolLabelPropScript is "Prop Script: " at 0x0045D995.
	PoolLabelPropScript = "Prop Script: "
	// PoolLabelShopScript is "Shop Script: " at 0x0045D9A5.
	PoolLabelShopScript = "Shop Script: "
	// PoolLabelPropMessage is "Prop Message: " at 0x0045D9B5.
	PoolLabelPropMessage = "Prop Message: "
	// PoolLabelShopMessage is "Shop Message: " at 0x0045D9C5.
	PoolLabelShopMessage = "Shop Message: "
)

// The prop/shop region entries, read from the image.
var propShopRegionEntries = []ActorRegionEntry{
	{0x0045D960, "()", true},
	{0x0045D965, "\", openshop()\"", true},
	{0x0045D975, "\", closeprop()\"", true},
	{0x0045D985, "\", closeshop()\"", true},
	{0x0045D995, "Prop Script: ", true},
	{0x0045D9A5, "Shop Script: ", true},
	{0x0045D9B5, "Prop Message: ", true},
	{0x0045D9C5, "Shop Message: ", true},
	{0x0045D9D5, "SaveGame", true},
	{0x0045D9E1, "This saved game is from a different version of this title.", true},
	{0x0045DA1D, "This is not a valid saved game file.", true},
	{0x0045DA45, "watch", true},
	{0x0045DA4D, "arrow", true},
	{0x0045DA55, "puppet", true},
	{0x0045DA5D, "stage", true},
	{0x0045DA65, "set", true},
	{0x0045DA6D, "current", true},
}

// The six short names at the region's end, which are a run of their own: "watch",
// "arrow", "puppet", "stage", "set" and "current". They are bare words with no
// punctuation, no colon and no trailing space, unlike the `Script: ` and `Message: `
// labels above them — so they are a **separate run**, not more labels.
//
// # Their relationship to the keyword table is stems, not membership
//
// **None of the six is a control-flow keyword**, and a first reading that they were was
// wrong. What the run actually holds is partly the *stems* of the keyword table's
// `open`/`close` pairs: the table has `openpuppet`/`closepuppet`, `openstage`/
// `closestage` and `openset`/`closeset`, and the bare stems `puppet`, `stage` and `set`
// appear here. So:
//
//   - **three of the six are stems** of keyword pairs — `puppet`, `stage`, `set`;
//   - **three are not stems of anything in the table** — `watch`, `arrow`, `current`;
//
// and the table's other five stems — `cast`, `actor`, `flat`, `shop`, `prop` — do **not**
// appear here. So the overlap is partial in both directions: neither set contains the
// other, and there is no single vocabulary being written twice.
//
// That is worth recording because a port that built a stem list from one set and expected
// it to cover the other would silently miss five keyword stems and would have three names
// with no keyword at all. The stems are also a strictly shorter list than the table: eight
// stems across sixteen open/close keywords, and only three of the eight are stored bare.
//
// What the run is *for* is not established here — context labels, event names and script
// short forms all fit. What is established is the shape: six bare names, of which three
// are keyword stems and three are not.
var propShopContextNames = []string{"watch", "arrow", "puppet", "stage", "set", "current"}

// PropShopContextNameCount is how many there are, six.
const PropShopContextNameCount = 6

// propShopKeywordStems are the stems of the keyword table's open/close pairs, eight:
// cast, actor, flat, stage, shop, prop, puppet and set. `propShopContextNames` holds
// three of them.
var propShopKeywordStems = []string{"cast", "actor", "flat", "stage", "shop", "prop", "puppet", "set"}

// IsKeywordStem reports whether a bare word is the stem of an `open`/`close` keyword
// pair, which is a **different question** from IsControlFlowKeyword — none of the stems
// is itself a keyword, and eight stems cover sixteen keywords.
func IsKeywordStem(word string) bool {
	for _, stem := range propShopKeywordStems {
		if EqualASCIIFold(word, stem) {
			return true
		}
	}
	return false
}

// KeywordPairFor returns the open and close keywords built from a stem, and whether the
// stem has a pair. Every stem in the table does.
func KeywordPairFor(stem string) (open ControlFlowKeyword, close ControlFlowKeyword, ok bool) {
	for k, text := range controlFlowKeywordText {
		if len(text) <= len("open") || text[:len("open")] != "open" {
			continue
		}
		if !EqualASCIIFold(text[len("open"):], stem) {
			continue
		}
		open = ControlFlowKeyword(k)
		// The matching close keyword is the same stem with the other prefix.
		for c, ctext := range controlFlowKeywordText {
			if len(ctext) <= len("close") || ctext[:len("close")] != "close" {
				continue
			}
			if EqualASCIIFold(ctext[len("close"):], stem) {
				return open, ControlFlowKeyword(c), true
			}
		}
		return open, 0, false
	}
	return 0, 0, false
}

// PropShopRegionText returns a string from this region. The address given **is** the
// text's first byte, so the same lookup as the actor region's.
func PropShopRegionText(addr uint32) (string, bool) {
	for _, entry := range propShopRegionEntries {
		if entry.Addr == addr {
			return entry.Text, entry.Printable
		}
	}
	return "", false
}

// SaveGameWarning is the save-compatibility message, at 0x0045D9E1.
//
// **This string has now been read three times and the first two readings were both
// wrong**, so the record is worth keeping carefully:
//
//  1. An earlier entry recorded it as **31 characters, cut mid-word at "diffe"**, and
//     warned that completing it would break whatever compares against it. The length was
//     wrong. The text runs unbroken to a full stop.
//  2. Reading two concluded it was **split into two pieces** — a first half ending at
//     "diffe" and a second beginning with " is from a different version of this title.".
//     That came from a scan of the image that **started at 0x0045D9F0, in the middle of
//     this string**, so it reported the tail as a separate string. The two halves
//     concatenate to a sentence with "is from a diffe" duplicated, which is what should
//     have exposed the reading immediately.
//  3. The correct reading: **58 characters, whole**, from 0x0045D9E1 to 0x0045DA1A
//     inclusive, followed by two NULs and then the invalid-file message.
//
// The lesson is mechanical rather than about this string: **a scan that does not begin at
// a NUL boundary will invent strings inside the one it is scanning.** Every region
// enumeration in this project starts at a known first string for that reason, and a
// mid-string start is the one case where that discipline does not protect you.
const SaveGameWarning = "This saved game is from a different version of this title."

// SaveGameWarningFirst is the address of the message's text, the byte the region's
// one-before convention names at 0x0045D9E0.
const SaveGameWarningFirst uint32 = 0x0045D9E1

// SaveGameInvalid is the movie player's other message, at 0x0045DA1D: "This is not a
// valid saved game file." So the player **validates the save before playing**, and both
// its failure messages are save-compatibility complaints rather than anything about the
// movie itself.
const SaveGameInvalid = "This is not a valid saved game file."

// SaveGameInvalidFirst is that message's text address; the player names it at
// 0x0045DA1C.
const SaveGameInvalidFirst uint32 = 0x0045DA1D

// VerifySaveGameMessages checks both messages: whole, complete, correctly addressed, and
// separated by exactly the two NULs the region uses before a message.
func VerifySaveGameMessages() (ok bool, detail string) {
	// **Fifty-eight characters**, which is the length that has to be checked, because
	// every wrong reading of this string was a wrong length.
	if len(SaveGameWarning) != 58 {
		return false, "the warning should be 58 characters, not the length a partial scan reported"
	}
	// It ends in a full stop, so it is a whole sentence and not a cut fragment.
	if SaveGameWarning[len(SaveGameWarning)-1] != '.' {
		return false, "the warning should end in a full stop"
	}
	// And it must not contain the duplicated text a split reading produced. "is from a
	// diffe" appearing twice is the signature of that error.
	if countOccurrences(SaveGameWarning, "is from a diffe") != 1 {
		return false, "the warning should contain \"is from a diffe\" exactly once"
	}
	if countOccurrences(SaveGameWarning, "this title.") != 1 {
		return false, "the warning should end its clause once"
	}
	// The text starts one byte after the address the reference names, which is this
	// region's convention.
	if SaveGameWarningFirst != 0x0045D9E1 {
		return false, "the warning's text should be at 0x0045D9E1"
	}
	if SaveGameWarningFirst-1 != 0x0045D9E0 {
		return false, "the region should name the warning one byte before its text"
	}
	// **The text runs to 0x0045DA1A and is followed by two NULs**, then the other
	// message. So the two messages are adjacent and distinctly separated.
	if SaveGameWarningFirst+uint32(len(SaveGameWarning)) != 0x0045DA1B {
		return false, "the warning should end just before 0x0045DA1B"
	}
	if SaveGameInvalidFirst != 0x0045DA1D {
		return false, "the invalid-file message should be at 0x0045DA1D"
	}
	// There is a two-NUL gap, so the messages cannot run together.
	if SaveGameInvalidFirst-SaveGameWarningFirst-uint32(len(SaveGameWarning)) != 2 {
		return false, "there should be exactly two NULs between the two messages"
	}
	// The second message is its own whole sentence, and it is shorter than the first.
	if SaveGameInvalid[len(SaveGameInvalid)-1] != '.' {
		return false, "the invalid-file message should end in a full stop"
	}
	if len(SaveGameInvalid) >= len(SaveGameWarning) {
		return false, "the invalid-file message is the shorter of the two"
	}
	if SaveGameInvalid == SaveGameWarning {
		return false, "the two messages should differ"
	}
	return true, "both messages are whole, complete, and separated by two NULs"
}

// countOccurrences counts non-overlapping appearances of a substring, which is how the
// duplicated-text signature of a split reading is detected.
func countOccurrences(text, sub string) int {
	if sub == "" {
		return 0
	}
	n := 0
	for i := 0; i+len(sub) <= len(text); i++ {
		if text[i:i+len(sub)] == sub {
			n++
			i += len(sub) - 1
		}
	}
	return n
}

// SendToShape is completed for all seven members. The three added here were
// established by decompiling their inner handlers in full, which also removed the two
// "no shape" gaps the earlier entry recorded.
//
//	20094 sendtostage  FUN_00413230  0 args  guard stage  1 frame  "Stage Script: "
//	20085 sendtoscene  FUN_0041A1A0  1 arg   guard set    3 frames "Scene", "Floor", "Set"
//	20084 sendtoactor  FUN_0040CB60  1 arg   guard stage  2 frames "Cast", "Actor"
//	20088 sendtoprop   FUN_00420CE0  1 arg   no guard    2 frames "Prop Script: ", "Shop Script: "
//	20089 sendtoshop   FUN_00421040  1 arg   no guard    1 frame  "Shop Script: "
//	20093 sendtoflat   FUN_00412EE0  1 arg   guard stage  2 frames "Stage", "Flat"
//	20092 sendtobutton FUN_00412A90  2 args  guard stage  4 frames
//
// **`sendtoprop` builds a `Prop Script: ` frame and a `Shop Script: ` frame**, which
// reads as nesting the prop's script inside the shop that owns it — a prop living in a
// shop, so sending to one enters both. That is a plausible reading of the two labels,
// and it is recorded as the two labels themselves rather than as an asserted nesting
// order, since the handler's build order was read but the intent was not.
//
// `sendtoscene` is the member with the **most** frames and the only one needing a set,
// and its three labels are all already in the transcribed pool — `Scene Script: `,
// `Floor Script: ` and `Set Script: ` — which is one of the few times a new handler
// turned up no new strings at all.
var sendToShapes = []SendToShape{
	// Member, name, arity, parser, resolver, frames, innermost label.
	{SendToStage, "sendtostage", ArityNone, "FUN_00413230", "",
		1, PoolLabelStageScript},
	{SendToScene, "sendtoscene", ArityOne, "FUN_004220A0", "",
		3, PoolLabelSceneScript},
	{SendToActor, "sendtoactor", ArityOne, "FUN_004220A0",
		ActorVisibleResolver, 2, PoolLabelActorScript},
	{SendToProp, "sendtoprop", ArityOne, "FUN_004220A0", "",
		2, PoolLabelPropScript},
	{SendToShop, "sendtoshop", ArityOne, "FUN_004220A0", "",
		1, PoolLabelShopScript},
	{SendToFlat, "sendtoflat", ArityOne, "FUN_004220A0",
		"FUN_00413530", 2, PoolLabelFlatScript},
	{SendToButton, "sendtobutton", ArityTwo, TwoArgFormHandler,
		"FUN_00413630", 4, PoolLabelButtonScript},
}

// SendToFrameLabels returns a member's full label chain, **in the order the handler
// builds it** — which is not the same as innermost-first for every member, and is not
// claimed to be. sendtoscene's three and sendtoprop's two are the chains a port would
// most likely drop, since neither is the member's own label alone.
func SendToFrameLabels(target SendToTarget) ([]string, bool) {
	switch target {
	case SendToStage:
		return []string{PoolLabelStageScript}, true
	case SendToScene:
		return []string{PoolLabelSceneScript, PoolLabelFloorScript, PoolLabelSetScript}, true
	case SendToActor:
		return []string{PoolLabelCastScript, PoolLabelActorScript}, true
	case SendToProp:
		return []string{PoolLabelPropScript, PoolLabelShopScript}, true
	case SendToShop:
		return []string{PoolLabelShopScript}, true
	case SendToFlat:
		return []string{PoolLabelStageScript, PoolLabelFlatScript}, true
	case SendToButton:
		return []string{
			PoolLabelActorScript, PoolLabelFlatScript,
			PoolLabelStageScript, PoolLabelButtonScript,
		}, true
	}
	return nil, false
}

// PropShopRegionCheck resolves a label address through this region's convention,
// which is the one byte *after* the named address. It exists so the convention is
// applied in one place rather than at each call site.
func PropShopRegionCheck(named uint32) (string, error) {
	if !PropShopRegionTextFollowsName {
		return "", fmt.Errorf("this region's text follows its named address, but the table says otherwise")
	}
	text, ok := PropShopRegionText(named + 1)
	if !ok {
		return "", fmt.Errorf("no string at %#x, which is one past the named address %#x", named+1, named)
	}
	return text, nil
}
