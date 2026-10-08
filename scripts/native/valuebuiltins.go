package native

import (
	. "redust/scripts"
	"fmt"
)

// Four small value builtins from live Ghidra decompilation, each specified
// completely: currentdir 16011, substring 20055, findfile 20067, and the status
// remap in plugin 12027.

// CurrentDir is the facing reported by `currentdir` 16011, whose value-side
// handler is FUN_0041B070. The function is a switch on the global DAT_00459A6A
// with four aligned cases and a fall-through tail:
//
//	if (DAT_00459A24 == 0) return "nowhere";
//	switch (DAT_00459A6A) {
//	case 0x40: return FUN_00406FA0() >= 0 ? "moving" : "south";
//	case 0x80: return FUN_00406FA0() >= 0 ? "moving" : "west";
//	case 0xC0: return FUN_00406FA0() >= 0 ? "moving" : "north";
//	default:   return "turning";
//	case 0:    break;                       // note: breaks, does not return
//	}
//	return FUN_00406FA0() >= 0 ? "moving" : "east";
//
// The `case 0` label is the load-bearing subtlety. It **breaks out of the switch
// rather than returning**, so control reaches the shared tail after the switch,
// which is why case 0 is "east" and not "no result". Reading it as an empty case
// would make a quarter of the compass report nothing.
//
// The four facings are therefore encoded in the top two bits, in the order
// east, south, west, north as the value increases: 0x00, 0x40, 0x80, 0xC0. The
// stride is 0x40. A pending walk overrides all four to "moving", and any
// unaligned value is "turning" rather than a facing.
type CurrentDir uint8

const (
	// CurrentDirEast is the zero case, reached through the switch's fall-through.
	CurrentDirEast CurrentDir = iota
	// CurrentDirSouth is 0x40.
	CurrentDirSouth
	// CurrentDirWest is 0x80.
	CurrentDirWest
	// CurrentDirNorth is 0xC0.
	CurrentDirNorth
	// CurrentDirCount is the number of aligned facings.
	CurrentDirCount = 4
	// CurrentDirStride is the spacing between the four case values, 0x40.
	CurrentDirStride = 0x40
	// CurrentDirUnaligned marks any value that is not one of the four aligned
	// cases, which the reference reports as "turning".
	CurrentDirUnaligned CurrentDir = 0xFF
)

// CurrentDirOpcode is the value-side opcode.
const CurrentDirOpcode uint16 = 16011

// CurrentDirHandler is the value-side native function.
const CurrentDirHandler = "FUN_0041B070"

// CurrentDirCommandHandler is the command-side function for the same opcode,
// which the hybrid band reaches only as a fallback and which is a different
// function. The command-side handler has not been decompiled, so only its
// identity, taken from the transcribed table, is recorded.
const CurrentDirCommandHandler = "FUN_0041ACE0"

// The globals the handler reads.
const (
	// CurrentDirSetSlot is DAT_00459A24, the set slot. Zero means no set, and the
	// answer is "nowhere" without consulting the direction at all.
	CurrentDirSetSlot = "DAT_00459A24"
	// CurrentDirDirectionSlot is DAT_00459A6A, the facing field.
	CurrentDirDirectionSlot = "DAT_00459A6A"
)

// CurrentDirName is the pool string each case returns, transcribed from the
// handler's own FUN_00427FC0 arguments and confirmed against the pool.
func (d CurrentDir) Name() (string, error) {
	switch d {
	case CurrentDirEast:
		return PoolFacingEast, nil
	case CurrentDirSouth:
		return PoolFacingSouth, nil
	case CurrentDirWest:
		return PoolFacingWest, nil
	case CurrentDirNorth:
		return PoolFacingNorth, nil
	case CurrentDirUnaligned:
		return PoolStateTurning, nil
	}
	return "", fmt.Errorf("currentdir has no name for facing %d", uint8(d))
}

// DecodeCurrentDir maps the raw direction field onto a facing, reproducing the
// switch's exact membership. Only the four values 0x00, 0x40, 0x80 and 0xC0 are
// cases; everything else, including 0x01 and 0x100, is the default.
func DecodeCurrentDir(raw uint16) CurrentDir {
	switch raw {
	case 0x00:
		return CurrentDirEast
	case 0x40:
		return CurrentDirSouth
	case 0x80:
		return CurrentDirWest
	case 0xC0:
		return CurrentDirNorth
	}
	return CurrentDirUnaligned
}

// ResolveCurrentDir reproduces FUN_0041B070's decision. setActive reports whether
// the set slot is non-zero, and walkPending is whether FUN_00406FA0 returned a
// non-negative value, which the reference tests as `-1 < result`.
//
// The order matters: the set check comes first, so with no set the direction field
// is never consulted. A pending walk then overrides all four facings, and only
// the unaligned default is not overridden.
func ResolveCurrentDir(setActive bool, rawDirection uint16, walkPending bool) (string, error) {
	if !setActive {
		return PoolStateNowhere, nil
	}
	facing := DecodeCurrentDir(rawDirection)
	// The default case returns immediately, so "turning" is never overridden by a
	// pending walk.
	if facing == CurrentDirUnaligned {
		return PoolStateTurning, nil
	}
	if walkPending {
		return PoolStateMoving, nil
	}
	return facing.Name()
}

// The `substring` builtin's name does not describe what it does. Verified
// FUN_004167E0 returns the **1-based position** at which the second argument
// occurs within the first, or -1 when it does not:
//
//	evaluate both arguments; load the first into scratch A, the second into B
//	result = -1;
//	if (len(B) != 0) {
//	    limit = len(A) - len(B) + 1;
//	    if (0 < limit)
//	        for (i = 1; i <= limit; i++)
//	            if (compare(A + i, B, len(B))) { result = i; break; }
//	}
//	*out = 4; *(int *)(out + 1) = result;
//
// Three consequences are worth stating, because each is a place a reasonable
// rewrite goes wrong. The result is type **4**, a number, not a string. Offsets
// are **1-based**, so a match at the very start is 1 and never 0. And an **empty
// needle returns -1** rather than 1, because the whole search is inside the
// `len(B) != 0` guard.
const (
	// SubstringOpcode is the value-side opcode.
	SubstringOpcode uint16 = 20055
	// SubstringHandler is the native function.
	SubstringHandler = "FUN_004167E0"
	// SubstringNotFound is the result when the needle is absent, or the needle is
	// empty, or the needle is longer than the haystack. All three are -1.
	SubstringNotFound int32 = -1
)

// FindOffset returns the 1-based position of needle in haystack, or -1, matching
// FUN_004167E0. The comparison is a length-limited scan, so an empty needle is the
// -1 case rather than a match at 1.
func FindOffset(haystack, needle string) int32 {
	if len(needle) == 0 {
		return SubstringNotFound
	}
	// limit is the last valid 1-based start, that is len(haystack) - len(needle) + 1.
	limit := len(haystack) - len(needle) + 1
	if limit <= 0 {
		return SubstringNotFound
	}
	for i := 0; i < limit; i++ {
		if haystack[i:i+len(needle)] == needle {
			return int32(i + 1)
		}
	}
	return SubstringNotFound
}

// `findfile` 20067 answers whether a file exists under any of the eight path
// prefixes. Verified FUN_00415780:
//
//	evaluate the name; load it into scratch A
//	result = 0;
//	for (i = 1; i < 9; i++) {
//	    FUN_00417820(i, &scratch);              // the prefix table
//	    FUN_0042E660(&A, &scratch);             // prefix + name
//	    if (FUN_0042C2A0(&scratch, ...) == 0) { result = 1; break; }
//	}
//	*out = 2;                                   // type 2, boolean
//
// The loop bound confirms the prefix count: it starts at 1 and stops before 9, so
// it tries exactly the eight prefixes the stage resolver uses.
//
// Note what `findfile` does **not** do. It never calls FUN_004178C0, the name
// validator, so a name the stage resolver would reject is searched here anyway
// and simply reports false. It is a raw existence check across the prefixes, not
// a stage resolution.
const (
	// FindFileOpcode is the value-side opcode.
	FindFileOpcode uint16 = 20067
	// FindFileHandler is the native function.
	FindFileHandler = "FUN_00415780"
)

// FindFileInPrefixes resolves findfile. prefix supplies the prefix text for an
// index, which is the caller-populated table the reference reaches through
// FUN_00417820, and exists reports whether a candidate names a regular file, which
// is what FUN_0042C2A0 answers. The loop stops at the first hit, so the returned
// probe count also says which prefix matched, counting from one.
func FindFileInPrefixes(name string, prefix func(index int) (string, bool), exists func(candidate string) bool) (found bool, probes int, matchedIndex int) {
	if prefix == nil || exists == nil {
		return false, 0, 0
	}
	for index := PathPrefixFirst; index <= PathPrefixLast; index++ {
		probes++
		text, ok := prefix(index)
		if !ok {
			// The reference has every slot populated; a missing one is a setup
			// failure, so it is skipped rather than probed with an empty prefix.
			continue
		}
		// The reference builds the prefix then appends the name onto it, so the
		// candidate is prefix + name.
		if exists(text + name) {
			return true, probes, index
		}
	}
	return false, probes, 0
}

// `plugin` 12027 remaps two statuses from its inner call. Verified FUN_0041DEF0:
//
//	u = FUN_0041DF30(...);
//	if ((short)u == 5) return 0;      // inner 5 becomes success
//	if ((short)u == 0) u = 6;         // inner success becomes 6
//	return u;
//
// So **inner 5 is promoted to success and inner success is turned into 6**, and
// every other status passes through unchanged.
//
// It is worth being precise that this is a *remap*, not an exchange, and the
// difference is observable: the outer 6 is a new code, so applying the map twice
// does not return the original value. 0 goes to 6 and 6 stays 6; 5 goes to 0 and
// 0 goes to 6. An earlier draft described it as codes 0 and 5 being swapped, which
// is wrong and which a test asserting self-inverseness is what caught.
//
// The practical consequence is that a caller of `plugin` sees success when the
// inner call returned 5, and sees 6 when the inner call succeeded, so a port that
// flattened this into a pass-through would invert the meaning of the opcode.
const (
	// PluginOpcode is the command opcode.
	PluginOpcode uint16 = 12027
	// PluginHandler is the native handler.
	PluginHandler = "FUN_0041DEF0"
	// PluginInnerHandler is the call whose status is remapped.
	PluginInnerHandler = "FUN_0041DF30"
	// PluginInnerSuccess is the inner code that becomes an outer failure.
	PluginInnerSuccess uint16 = 0
	// PluginInnerSkip is the inner code that becomes an outer success.
	PluginInnerSkip uint16 = 5
	// PluginOuterSuccess is what PluginInnerSkip becomes.
	PluginOuterSuccess uint16 = 0
	// PluginOuterSkipped is what PluginInnerSuccess becomes. It is a new code, not
	// a stand-in for 5, which is why the map is not self-inverse.
	PluginOuterSkipped uint16 = 6
)

// RemapPluginStatus applies plugin's exchange, reproducing FUN_0041DEF0.
func RemapPluginStatus(inner uint16) uint16 {
	if inner == PluginInnerSkip {
		return PluginOuterSuccess
	}
	if inner == PluginInnerSuccess {
		return PluginOuterSkipped
	}
	return inner
}
