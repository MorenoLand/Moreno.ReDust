package scripts

import "fmt"

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
	// PropShopRegionLast is its last, the save-game warning.
	PropShopRegionLast uint32 = 0x0045DA01
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
	{0x0045D9E1, "This saved game is from a diffe", true},
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

// SaveGameWarning is the truncated compatibility message the region ends with. The
// reference stores only the first thirty-one characters, so the text is cut mid-word
// and **must not be completed** — a port that "fixes" it would stop matching whatever
// compares against it.
const SaveGameWarning = "This saved game is from a diffe"

// VerifySaveGameWarningIsTruncated checks the warning is recorded exactly as stored,
// and that completing it is treated as a change rather than a correction.
func VerifySaveGameWarningIsTruncated() bool {
	if len(SaveGameWarning) != 31 {
		return false
	}
	// It ends mid-word: the last two characters are "fe", the cut through "different".
	return SaveGameWarning[len(SaveGameWarning)-1] == 'e' &&
		SaveGameWarning[len(SaveGameWarning)-2] == 'f'
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
