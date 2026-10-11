package scripts

import (
	"fmt"
	"strings"
)

// The script-attach statements actorscript, castscript, propscript, shopscript
// and puppetscript. Each handler was read from its cached decompile and
// assembly under .codex/ghidra-exports/: FUN_0040C900 (12015), FUN_0040C9B0
// (12047), FUN_00420A80 (12054), FUN_00420B30 (12058) and FUN_00408AD0 (12046).
//
// Each one has the script-entry shape of scriptentry.go: it reads one name,
// checks that it is a string, looks it up in one table, and then builds
// "<label><name>" in the shared scratch DAT_00459BD0 and clears the result slot
// DAT_00459720 through FUN_00418930. The looked-up record is a local copy that
// is never stored back, and nothing else is written. So a successful call has
// no effect on script-visible state. The port therefore carries the argument
// checks, the lookups and the status codes, and not the scratch or the result
// slot. Nothing in the port reads either one: the scratch is always rewritten
// before any cached function reads it, and the result slot is read only by
// result (16010), which the port does not wire.
//
// The warm-ups actorwarm (12085, FUN_0040B450), castwarm (12088, FUN_0040B4D0),
// propwarm (12086, FUN_0041F7E0) and shopwarm (12087, FUN_0041F860) read one
// name and look it up the same way. They then preload the record's resource
// blocks through FUN_0040E120 or FUN_00421DD0. Those call FUN_004014F0 and
// FUN_00401670, which only move the resource cache: a load leaves the entry at
// zero uses. The cache's last-found globals (DAT_00442004 and DAT_00442014) are a
// lookup hint that returns the same index either way. The port loads resources on
// demand, so it carries the warm-ups' checks and statuses and not the preload.
// The resource-missing diagnostics that FUN_0042C470 raises (0x100F, 0x1010,
// 0x1203 and 0x1204) are not reproduced.

// attachNameArgument reads the single name argument of an attach statement in
// the native order: the argument is evaluated, an empty one is 0x0E, and a
// non-string is 0x0E. propReadArgs is the shared native reader, so the closing
// paren and extra-argument handling match the prop statements.
func (h *GameHost) attachNameArgument(call *ScriptCall) (name string, consumed int, extra bool, status uint16, err error) {
	args, consumed, extra, status, err := h.propReadArgs(call, 1)
	if err != nil || status != 0 {
		return "", 0, false, status, err
	}
	if args[0].Kind != 3 {
		return "", 0, false, ScriptStatusWrongType, nil
	}
	return args[0].Text, consumed, extra, 0, nil
}

// attachTargetFound reports whether the name is in the table the statement
// resolves it in. A miss is the native 0x0A, and the lookups are the ones the
// sendto senders already use: the actor table (FUN_0040D730), the cast table
// (FUN_0040D870), the prop table (FUN_004213C0), the open shops (FUN_00421520)
// and the open puppet's script table (FUN_00409160). Name comparison uses
// strings.EqualFold, which the port uses for the other tables too; the native
// byte table DAT_0045DDF0 has not been checked.
func (h *GameHost) attachTargetFound(statement, name string) (bool, error) {
	switch statement {
	case "actorscript", "actorwarm":
		_, status := h.Actors.Lookup(name)
		return status == 0, nil
	case "castscript", "castwarm":
		if h.Env.CastCount == nil || h.Env.CastName == nil {
			return false, fmt.Errorf("%s has no cast table", statement)
		}
		for index := 0; index < h.Env.CastCount(); index++ {
			if cast, ok := h.Env.CastName(index); ok && strings.EqualFold(cast, name) {
				return true, nil
			}
		}
		return false, nil
	case "propscript", "propwarm":
		if h.Props == nil {
			return false, fmt.Errorf("props are unavailable")
		}
		return h.Props.Known(name), nil
	case "shopscript", "shopwarm":
		if h.Env.Shop == nil {
			return false, fmt.Errorf("%s has no shop table", statement)
		}
		_, _, ok := h.Env.Shop(name)
		return ok, nil
	case "puppetscript":
		for _, script := range h.puppet.info.Scripts {
			if strings.EqualFold(script.Name, name) {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

// buttonScriptCommand is FUN_00412970, buttonscript(flat, button). The stage
// guard comes first, so no open stage is 0x0B before any argument is read. Then
// both arguments are read (0x1C for a missing comma, 0x0E for an empty one), the
// flat name must be a string (0x0E) and name a flat of the open stage (0x0A), and
// the button name must be a string (0x0E) and name a row of that flat's button
// table (0x0A). The native then builds "Button Script: " plus the button row's own
// name in the scratch, the same no-effect write as the name statements above.
// The button table is FUN_00413630 over the flat row's dword 2, which the port
// reads through flatButtonIndex.
func (h *GameHost) buttonScriptCommand(call *ScriptCall) (int, uint16, error) {
	if h.Env.StageOpen == nil {
		return 0, 0, fmt.Errorf("buttonscript has no stage state")
	}
	if !h.Env.StageOpen() {
		return 0, StatusNoActiveStage, nil
	}
	args, consumed, extra, status, err := h.propReadArgs(call, 2)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if h.Env.FlatIndex == nil || h.Env.FlatButtonCount == nil || h.Env.FlatButtonName == nil {
		return 0, 0, fmt.Errorf("buttonscript has no flat or button table")
	}
	if args[0].Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	flat, found := h.Env.FlatIndex(args[0].Text)
	if !found {
		return 0, StatusNameNotFound, nil
	}
	if args[1].Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	_, found, err = h.flatButtonIndex(flat, args[1].Text)
	if err != nil {
		return 0, 0, err
	}
	if !found {
		return 0, StatusNameNotFound, nil
	}
	if extra {
		return consumed, ScriptStatusMalformed, nil
	}
	return consumed, 0, nil
}

// floorScriptCommand is floorscript, FUN_0041AB70 (12037), a command with one
// number argument. The native order, from the cached decompile and assembly: with
// no open set (DAT_00459A24 clear) the status is 0x28 before the argument is read;
// the argument is evaluated and an expression status passes through; a non-number
// is 0x0E; an index outside 0 to 31 is 0x0E. A valid index reads the open set's
// floor table at +0x1B8C (the value is not used), builds "Floor Script: " and the
// index in the scratch DAT_00459BD0, clears the result slot DAT_00459720 through
// FUN_00418930, and returns 0. It runs no script: its callees are the expression
// reader, the string helpers, GlobalLock and GlobalUnlock, and FUN_00418930. The
// text is the pool string at 0x0045D6A8, which FUN_00427FC0 turns into a Pascal
// string in place. Nothing reads the scratch before a cached function rewrites
// it (the four READ references to DAT_00459BD0 each follow a write in the same
// function), and the cleared slot is read only by result (16010), which the port
// does not wire. So the port carries the gates and the statuses, and not the
// scratch or the slot. Extra arguments run the checks and then report 2, as in
// the attach statements.
func (h *GameHost) floorScriptCommand(call *ScriptCall) (int, uint16, error) {
	if _, open := h.setRect(); !open {
		return 0, StatusNoActiveSet, nil
	}
	args, consumed, extra, status, err := h.propReadArgs(call, 1)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if args[0].Kind != 4 || args[0].Int < 0 || args[0].Int >= 0x20 {
		return 0, ScriptStatusWrongType, nil
	}
	if extra {
		return consumed, ScriptStatusMalformed, nil
	}
	return consumed, 0, nil
}

// scriptAttachCommand handles the script-attach statements. handled is false
// for opcodes it does not own.
func (h *GameHost) scriptAttachCommand(name string, call *ScriptCall) (consumed int, status uint16, handled bool, err error) {
	switch name {
	case "actorscript", "castscript", "propscript", "shopscript", "puppetscript", "buttonscript",
		"actorwarm", "castwarm", "propwarm", "shopwarm", "floorscript":
	default:
		return 0, 0, false, nil
	}
	// The dispatcher checks the opening paren before the handler runs, so a
	// missing one is status 2 ahead of every guard below.
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, true, nil
	}
	if name == "buttonscript" {
		consumed, status, err = h.buttonScriptCommand(call)
		return consumed, status, true, err
	}
	if name == "floorscript" {
		consumed, status, err = h.floorScriptCommand(call)
		return consumed, status, true, err
	}
	// FUN_00408AD0 tests DAT_00459A92, the open-puppet flag, before it reads its
	// argument. With no puppet open the status is 0x2D whatever the argument is.
	if name == "puppetscript" && h.puppet == nil {
		return 0, StatusNoPuppet, true, nil
	}
	target, consumed, extra, status, err := h.attachNameArgument(call)
	if err != nil || status != 0 {
		return 0, status, true, err
	}
	found, err := h.attachTargetFound(name, target)
	if err != nil {
		return 0, 0, true, err
	}
	if !found {
		return 0, StatusNameNotFound, true, nil
	}
	// The handler has run by now. Extra arguments are reported after it, the
	// same way the native dispatcher reports them.
	if extra {
		return consumed, ScriptStatusMalformed, true, nil
	}
	return consumed, 0, true, nil
}
