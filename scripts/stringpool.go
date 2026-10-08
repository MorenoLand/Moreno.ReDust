package scripts

// EngineStringPool is the packed NUL-terminated C string pool at
// 0x0045D360..0x0045D733 in DF386.EXE .data, 980 bytes holding 77 strings.
//
// **The upper boundary was corrected.** This transcription previously ended at
// 0x0045D739 with a final entry `{0x0045D738, "<"}`, which was wrong: 0x0045D738
// holds the pointer 0x0045D93C, the first entry of the control-flow keyword table
// that FUN_0041DC00 scans. Read as ASCII, the pointer's low byte 0x3C is "<", which
// is how the phantom entry arose. The pool's last real string is "resume" at
// 0x0045D72D, NUL-terminated at 0x0045D733, followed by four padding zeros and then
// the pointer table. See keywords.go.
//
// The pool is the engine's whole message and keyword vocabulary: the context
// labels the script-entry handlers build, the property type names, the
// generated-event fragments, the arrow key names, the dialog literals and the
// one diagnostic message. FUN_00427FC0 resolves the address Ghidra renders as
// "&DAT_..." to the text; note that the byte at the address Ghidra names is the
// *previous* string's terminator, so the text begins one byte later, which is
// why the recorded addresses here are all odd and the byte at each address-1
// is zero.
//
// Entries are in address order and are byte-exact, including the reference's
// own asymmetries. Several are load-bearing and must not be "corrected":
//
//	openset() and closeset() carry no prefix at all, while the matching
//	openstage(), closestage(), openflat(), closeflat(), openscene(),
//	closescene(), openfloor() and closefloor() fragments are prefixed,
//	and the first four of those close with a trailing quote.
//	closefloor() is prefixed `, ` while every other close fragment is
//	prefixed `", `.
//	Boot Script carries no colon and no trailing space, while every other
//	Script and Message label ends in ": ".
//	strait is spelled without the trailing "gh" in the original.
//	"), openstage()" is prefixed with a closing quote, but the open
//	fragments elsewhere are prefixed with an opening one.
//
// The ordering and spelling are transcribed from the reference image; a test
// asserts the count, the extent and the entries that other conversions depend
// on, so a mistranscription fails loudly.
type PoolEntry struct {
	// Addr is the address of the first character of the text in the image.
	Addr uint32
	// Text is the NUL-terminated string, without its terminator.
	Text string
}

// PoolFirst and PoolLast bound the pool in the reference image. PoolLast is the
// NUL that terminates "resume", not the start of the keyword table that follows.
const (
	PoolFirst uint32 = 0x0045D360
	PoolLast  uint32 = 0x0045D733
)

// PoolSize is the verified byte extent of the pool, 980. Four padding bytes follow
// it before the keyword pointer table begins at 0x0045D738.
const PoolSize = 980

// PoolPaddingBytes is how many zero bytes separate the pool from the keyword table,
// 0x0045D734..0x0045D737.
const PoolPaddingBytes = 4

// KeywordTableFirst is where the keyword pointer table begins, immediately after the
// pool's padding.
const KeywordTableFirst uint32 = 0x0045D738

// EngineStringPool is the transcribed pool in address order.
var EngineStringPool = []PoolEntry{
	{0x0045D361, "all"},
	{0x0045D369, "none"},
	{0x0045D371, "()"},
	{0x0045D375, "\", "},
	{0x0045D37D, "unknown"},
	{0x0045D389, "walktostar"},
	{0x0045D395, "walkonpath"},
	{0x0045D3A1, "custom"},
	{0x0045D3A9, "walknodata"},
	{0x0045D3B5, "\", endwalk()"},
	{0x0045D3C5, "\", endturn()"},
	{0x0045D3D5, "\", endball(\"player\")"},
	{0x0045D3ED, "\")"},
	{0x0045D3F1, "\", endball(\""},
	{0x0045D401, "\", endball(\"building\")"},
	{0x0045D419, "\", endball(\"frames\")"},
	{0x0045D431, "flat"},
	{0x0045D439, "scene"},
	{0x0045D441, "prop"},
	{0x0045D449, "\", openflat()"},
	{0x0045D459, "\", closeflat()"},
	{0x0045D469, "openstage()"},
	{0x0045D479, "closestage()"},
	{0x0045D489, "Stage Script: "},
	{0x0045D499, "Flat Script: "},
	{0x0045D4A9, "Button Script: "},
	{0x0045D4BD, "Flat Message: "},
	{0x0045D4CD, "Stage Message: "},
	{0x0045D4E5, "button"},
	{0x0045D4ED, "logic"},
	{0x0045D4F5, "number"},
	{0x0045D4FD, "text"},
	{0x0045D505, "bootfile"},
	{0x0045D511, "Runtime"},
	{0x0045D51D, "idle()"},
	{0x0045D525, "boot()"},
	{0x0045D52D, "menustate()"},
	{0x0045D53D, "menuselect(\""},
	{0x0045D54D, "rightarrow"},
	{0x0045D559, "leftarrow"},
	{0x0045D565, "downarrow"},
	{0x0045D571, "uparrow"},
	{0x0045D57D, "keydown(\""},
	{0x0045D589, "keyrepeat(\""},
	{0x0045D599, ")"},
	{0x0045D59D, "mousedown("},
	{0x0045D5A9, "Boot Script"},
	{0x0045D5B9, "Boot Message: "},
	{0x0045D5C9, "OK"},
	{0x0045D5CD, "False"},
	{0x0045D5D5, "True"},
	{0x0045D5DD, "Msg Box Message: "},
	{0x0045D5F1, "tokenization error"},
	{0x0045D605, "right"},
	{0x0045D60D, "left"},
	{0x0045D615, "backwards"},
	{0x0045D621, "strait"},
	{0x0045D629, "\", openscene()"},
	{0x0045D639, "\", closescene()"},
	{0x0045D64D, "\", openfloor()"},
	{0x0045D65D, "openset()"},
	{0x0045D669, ", closefloor()"},
	{0x0045D679, "closeset()"},
	{0x0045D685, "Scene Message: "},
	{0x0045D699, "Set Script: "},
	{0x0045D6A9, "Floor Script: "},
	{0x0045D6B9, "Scene Script: "},
	{0x0045D6C9, "Floor Message: "},
	{0x0045D6DD, "Set Message: "},
	{0x0045D6ED, "west"},
	{0x0045D6F5, "south"},
	{0x0045D6FD, "north"},
	{0x0045D705, "east"},
	{0x0045D70D, "turning"},
	{0x0045D719, "moving"},
	{0x0045D721, "nowhere"},
	{0x0045D72D, "resume"},
	// 0x0045D738 is NOT a pool entry. It holds the pointer 0x0045D93C, the first
	// slot of the control-flow keyword table; its low byte 0x3C reads as "<" and
	// was previously mistaken for a one-character string. See keywords.go.
}

// PoolText returns the pool string at the given address. The address must be
// the first character of the text, which is one byte past the address Ghidra
// renders for the reference.
func PoolText(addr uint32) (string, bool) {
	for _, entry := range EngineStringPool {
		if entry.Addr == addr {
			return entry.Text, true
		}
	}
	return "", false
}

// Property type names. These are the four type tags the engine uses when it
// stores a script property, and they are the tokens the parser accepts as a
// type.
const (
	PoolTypeButton = "button"
	PoolTypeLogic  = "logic"
	PoolTypeNumber = "number"
	PoolTypeText   = "text"
)

// Entity kind names.
const (
	PoolKindFlat  = "flat"
	PoolKindScene = "scene"
	PoolKindProp  = "prop"
)

// Context labels. The script-entry handlers copy these into the shared scratch
// and install the result as the name of the child script context.
const (
	PoolLabelStageScript  = "Stage Script: "
	PoolLabelFlatScript   = "Flat Script: "
	PoolLabelButtonScript = "Button Script: "
	PoolLabelSetScript    = "Set Script: "
	PoolLabelSceneScript  = "Scene Script: "
	PoolLabelFloorScript  = "Floor Script: "
	// PoolLabelBootScript is the one label with no colon and no trailing
	// space. Verified FUN_004182F0 uses it as-is, so the asymmetry with the
	// other six is preserved rather than normalised.
	PoolLabelBootScript = "Boot Script"
)

// Message labels, used where a dialog names its owning context.
const (
	PoolLabelStageMessage   = "Stage Message: "
	PoolLabelFlatMessage    = "Flat Message: "
	PoolLabelSceneMessage   = "Scene Message: "
	PoolLabelSetMessage     = "Set Message: "
	PoolLabelFloorMessage   = "Floor Message: "
	PoolLabelBootMessage    = "Boot Message: "
	PoolLabelMsgBoxMessage  = "Msg Box Message: "
	PoolTextTokenizationErr = "tokenization error"
)

// Generated-event name fragments. The engine assembles an event name by writing
// a fragment, a caller-supplied name, then a closing fragment.
const (
	PoolEventIdle       = "idle()"
	PoolEventBoot       = "boot()"
	PoolEventMenuState  = "menustate()"
	PoolEventMenuSelect = "menuselect(\""
	PoolEventKeyDown    = "keydown(\""
	PoolEventKeyRepeat  = "keyrepeat(\""
	PoolEventMouseDown  = "mousedown("
	PoolEventSuffix     = ")"
	PoolEventEndWalk    = "\", endwalk()"
	PoolEventEndTurn    = "\", endturn()"
	PoolEventEndBall    = "\", endball(\""
)

// Arrow key names, as the key event builders spell them.
const (
	PoolKeyRight = "rightarrow"
	PoolKeyLeft  = "leftarrow"
	PoolKeyDown  = "downarrow"
	PoolKeyUp    = "uparrow"
)

// Dialog literals.
const (
	PoolLiteralOK    = "OK"
	PoolLiteralTrue  = "True"
	PoolLiteralFalse = "False"
)

// Turn and facing names, spelled as the reference stores them.
const (
	PoolTurnRight     = "right"
	PoolTurnLeft      = "left"
	PoolTurnBackwards = "backwards"
	PoolTurnStrait    = "strait"
	PoolFacingWest    = "west"
	PoolFacingSouth   = "south"
	PoolFacingNorth   = "north"
	PoolFacingEast    = "east"
	PoolStateTurning  = "turning"
	PoolStateMoving   = "moving"
	PoolStateNowhere  = "nowhere"
	PoolStateResume   = "resume"
	PoolWalkToStar    = "walktostar"
	PoolWalkOnPath    = "walkonpath"
	PoolWalkNoData    = "walknodata"
	PoolWalkCustom    = "custom"
	PoolNameAll       = "all"
	PoolNameNone      = "none"
	PoolNameUnknown   = "unknown"
	// There is deliberately no PoolLessThan. The transcription had one, named for
	// the string "<" at 0x0045D738, but that address holds the keyword table's first
	// pointer and its low byte 0x3C only reads as "<" when interpreted as ASCII. The
	// constant is removed rather than repointed, because no verified use of it
	// exists and a string the engine does not hold would be worse than an absent
	// one.
	PoolNameBootfile  = "bootfile"
	PoolNameRuntime   = "Runtime"
	PoolNameStageOpen = "openstage()"
	PoolNameStageShut = "closestage()"
)

// ScriptContextLabel resolves the context label for a script-entry opcode from
// the transcribed pool addresses verified in the FUN_00424890 script family.
// The returned label is byte-exact, including the boot label's missing colon.
func ScriptContextLabel(opcode uint16) (string, bool) {
	switch opcode {
	case 12031: // bootscript  -> FUN_004182F0, 0x0045D5A9
		return PoolLabelBootScript, true
	case 12035: // setscript   -> FUN_0041AA40, 0x0045D699
		return PoolLabelSetScript, true
	case 12036: // scenescript -> FUN_0041AAB0, 0x0045D6B9
		return PoolLabelSceneScript, true
	case 12063: // stagescript -> FUN_00412840, 0x0045D489
		return PoolLabelStageScript, true
	case 12064: // flatscript  -> FUN_004128B0, 0x0045D499
		return PoolLabelFlatScript, true
	}
	return "", false
}
