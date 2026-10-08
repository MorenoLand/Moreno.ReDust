package native

import (
	"fmt"
)

// The actor and cast string region, at 0x0045D001..0x0045D14D, and the sendto
// family corrections it forced.
//
// **The engine has two string regions, and they use different conventions.** The pool
// transcribed in stringpool.go runs 0x0045D360..0x0045D733, where the byte at the
// address Ghidra names is the *previous* string's terminator, so each entry is one
// byte after its name. This region runs 0x0045D001..0x0045D14D, is separated by
// **double** NULs, and there the byte at the named address is the string's **own
// first character**. So the off-by-one rule is a property of one region's packing, not
// of the reference's addressing, and applying it here would truncate every label by one
// letter — `"ctor Script: "` instead of `"Actor Script: "`. A test asserts both
// regions' conventions separately so neither is applied to the other.
//
// The region was read out of the image rather than inferred, and it contains the three
// fallback names `values.go` already recorded, at exactly the addresses recorded there.
// That is an independent confirmation: `None` at 0x0045D07D, `actor` at 0x0045D0ED, and
// the third likewise. So the fallbacks live in *this* region, not in the transcribed
// pool, which is what the earlier entry said without saying where.
const (
	// ActorRegionFirst is the region's first printable string, 0x0045D001.
	ActorRegionFirst uint32 = 0x0045D001
	// ActorRegionLast is its last printable string, 0x0045D14D, before the value table.
	ActorRegionLast uint32 = 0x0045D14D
	// ActorRegionSeparator is how many NUL bytes separate strings here, two. The
	// transcribed pool uses one, which is why its entries sit one byte lower.
	ActorRegionSeparator = 2
	// ActorRegionTextStartsAtNamedAddress records this region's convention, in contrast
	// with the pool's. It is the fact a port is most likely to get wrong here.
	ActorRegionTextStartsAtNamedAddress = true
)

// The labels the script-entry family builds child contexts from. Three of these were
// already in the transcribed pool; the actor and cast pair are here and were not.
const (
	// PoolLabelActorScript is "Actor Script: " at 0x0045D119.
	PoolLabelActorScript = "Actor Script: "
	// PoolLabelCastScript is "Cast Script: " at 0x0045D129.
	PoolLabelCastScript = "Cast Script: "
	// PoolLabelPuppetScript is "Puppet Script: " at 0x0045D069.
	PoolLabelPuppetScript = "Puppet Script: "
	// PoolLabelActorMessage is "Actor Message: " at 0x0045D139.
	PoolLabelActorMessage = "Actor Message: "
	// PoolLabelCastMessage is "Cast Message: " at 0x0045D14D.
	PoolLabelCastMessage = "Cast Message: "
	// PoolLabelPuppetMessage is "Puppet Message: " at 0x0045D04D.
	PoolLabelPuppetMessage = "Puppet Message: "
)

// ActorRegionEntry is one verified string in this region.
type ActorRegionEntry struct {
	// Addr is the address of the string's **first character**. In this region that is
	// the address Ghidra names, unlike the transcribed pool.
	Addr uint32
	// Text is the string, without its terminator.
	Text string
	// Printable records that every byte was in the printable ASCII range, which is how
	// the region's own contents were validated.
	Printable bool
}

// actorRegionEntries is the verified region, read from the image. Twenty-five
// printable strings plus the one entry after the value table, which is data rather
// than text and is recorded separately below.
var actorRegionEntries = []ActorRegionEntry{
	{0x0045D001, "Movie Theme", true},
	{0x0045D011, "Movie Sound", true},
	{0x0045D021, "\", openpuppet()\"", true},
	{0x0045D035, "\"", true},
	{0x0045D039, "\", closecast()\"", true},
	{0x0045D04D, "Puppet Message: ", true},
	{0x0045D061, "System", true},
	{0x0045D069, "Puppet Script: ", true},
	{0x0045D07D, "None", true},
	{0x0045D085, "Player Voice", true},
	{0x0045D095, "Puppet Voice", true},
	{0x0045D0A5, "idle 4", true},
	{0x0045D0AD, "idle 3", true},
	{0x0045D0B5, "idle 2", true},
	{0x0045D0BD, "idle 1", true},
	{0x0045D0C5, "player", true},
	{0x0045D0CD, "\", openactor()\"", true},
	{0x0045D0DD, "\", opencast()\"", true},
	{0x0045D0ED, "actor", true},
	{0x0045D0F5, "\", closeactor()\"", true},
	{0x0045D109, "\", closecast()\"", true},
	{0x0045D119, "Actor Script: ", true},
	{0x0045D129, "Cast Script: ", true},
	{0x0045D139, "Actor Message: ", true},
	{0x0045D14D, "Cast Message: ", true},
}

// ActorRegionText returns a string from this region by address, and reports whether it
// was found. Unlike the transcribed pool, the address given **is** the text's first
// byte.
func ActorRegionText(addr uint32) (string, bool) {
	for _, entry := range actorRegionEntries {
		if entry.Addr == addr {
			return entry.Text, entry.Printable
		}
	}
	return "", false
}

// ActorValueTableAddress is where the region's text ends and a 256-byte table begins.
// The table is **sixteen groups of sixteen identical bytes**, with the group's value
// running from `0x01` to `0x10`. So it is a 16×16 grid whose entries depend only on the
// row, which is the shape of a sixteen-level intensity or palette table rather than
// ordinary packed RGB.
//
// What it is *for* is not established here, and the shape is recorded rather than a
// purpose guessed at. It is the only non-text data between the region's last string and
// the transcribed pool's first entry.
const (
	ActorValueTableAddress uint32 = 0x0045D16D
	ActorValueTableBytes          = 256
	ActorValueTableGroups         = 16
	ActorValueTableGroupSize      = 16
	ActorValueTableFirstValue     = 0x01
	ActorValueTableLastValue      = 0x10
)

// ActorValueTableEntry returns the byte at a group and an index within it. The table is
// uniform across each group, so the value depends only on the group.
func ActorValueTableEntry(group, index int) (byte, error) {
	if group < 0 || group >= ActorValueTableGroups {
		return 0, fmt.Errorf("group %d is outside the %d groups", group, ActorValueTableGroups)
	}
	if index < 0 || index >= ActorValueTableGroupSize {
		return 0, fmt.Errorf("index %d is outside the %d entries of a group", index, ActorValueTableGroupSize)
	}
	return byte(ActorValueTableFirstValue + group), nil
}

// The version field's offset, confirmed independently. The re-entrancy guards were
// found sampling a field at **byte** offset `0xD34` — 3380 — in a puppet block and in a
// set block. Every `sendto` inner handler reads the same field, and reads it as
// `context[0x34D]` where the context is a **DWORD pointer**, so its index `0x34D` is a
// byte offset of `0x34D * 4 = 3380 = 0xD34`.
//
// That is the same field, found by a different route and expressed in a different unit,
// and it is worth recording because a reader seeing `[0x34D]` would reasonably suspect
// a different field from one written `+ 0xD34`.
const (
	// ContextVersionByteOffset is the field's byte offset, 0xD34.
	ContextVersionByteOffset = 0xD34
	// ContextVersionDwordIndex is the same field as a DWORD array index, 0x34D. The two
	// spellings differ by a factor of four and name one field.
	ContextVersionDwordIndex = 0x34D
)

// VerifyContextVersionOffsetsAgree checks the two spellings describe one field, which
// is the whole point of recording both.
func VerifyContextVersionOffsetsAgree() bool {
	return ContextVersionDwordIndex*4 == ContextVersionByteOffset
}
