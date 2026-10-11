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
	case "actorscript":
		_, status := h.Actors.Lookup(name)
		return status == 0, nil
	case "castscript":
		if h.Env.CastCount == nil || h.Env.CastName == nil {
			return false, fmt.Errorf("%s has no cast table", statement)
		}
		for index := 0; index < h.Env.CastCount(); index++ {
			if cast, ok := h.Env.CastName(index); ok && strings.EqualFold(cast, name) {
				return true, nil
			}
		}
		return false, nil
	case "propscript":
		if h.Props == nil {
			return false, fmt.Errorf("props are unavailable")
		}
		return h.Props.Known(name), nil
	case "shopscript":
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

// scriptAttachCommand handles the script-attach statements. handled is false
// for opcodes it does not own.
func (h *GameHost) scriptAttachCommand(name string, call *ScriptCall) (consumed int, status uint16, handled bool, err error) {
	switch name {
	case "actorscript", "castscript", "propscript", "shopscript", "puppetscript":
	default:
		return 0, 0, false, nil
	}
	// The dispatcher checks the opening paren before the handler runs, so a
	// missing one is status 2 ahead of every guard below.
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, true, nil
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
