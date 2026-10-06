package scripts

import (
	"fmt"
	"strings"
)

// pauseAdjust is FUN_0040FD30 for loops and FUN_004106B0 for walk jobs: the
// pause count of the named owner (or of all) rises when pausing and falls,
// never below zero, when resuming.
func pauseAdjust(depth int16, pause bool) int16 {
	if pause {
		return depth + 1
	}
	if depth-1 < 0 {
		return 0
	}
	return depth - 1
}

// findWord is FUN_00416650 (findword): the nth word of text split on the
// separator. An empty separator selects the nth character. The loop ends a
// word at each separator match and once more at the last start position, and
// it copies the span before that position, so a word that is not followed by a
// separator comes back one character short; scripts append the trailing
// separator the original's code requires.
func findWord(text, separator string, n int32) string {
	if separator == "" {
		if n > 0 && int(n) <= len(text) {
			return text[n-1 : n]
		}
		return ""
	}
	limit := len(text) - len(separator) + 1
	start := 1
	for position := 1; position <= limit; position++ {
		matched := strings.EqualFold(text[position-1:position-1+len(separator)], separator)
		if matched || limit <= position {
			n--
			if n < 1 {
				return text[start-1 : position-1]
			}
			start = len(separator) + position
		}
	}
	return ""
}

// subString is FUN_004167E0 (substring): the 1-based position of the first
// case-insensitive match of the second string in the first, or -1.
func subString(text, part string) int32 {
	if part == "" {
		return -1
	}
	for position := 1; position <= len(text)-len(part)+1; position++ {
		if strings.EqualFold(text[position-1:position-1+len(part)], part) {
			return int32(position)
		}
	}
	return -1
}

// worldCommand handles the pause, error, message and view statements.
func (h *GameHost) worldCommand(name string, call *ScriptCall) (consumed int, status uint16, handled bool, err error) {
	switch name {
	case "error":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		_ = consumed
		// FUN_00426380 reports native status 0x2F, the script-raised error.
		return 0, 0x2f, true, nil
	case "message":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		h.logf("script-message %s", args[0].Text)
		return consumed, 0, true, nil
	case "pauseloop", "pausewalk", "pauseball":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		want := 3
		if name != "pauseloop" {
			want = 2
		}
		if len(args) != want {
			return 0, ScriptStatusMalformed, true, nil
		}
		for index := 0; index < want-1; index++ {
			if args[index].Kind != 3 {
				return 0, ScriptStatusWrongType, true, nil
			}
		}
		if args[want-1].Kind != 2 {
			return 0, ScriptStatusWrongType, true, nil
		}
		pause := args[want-1].Int != 0
		switch name {
		case "pauseloop":
			kind, ok := LoopKindByName(args[0].Text)
			if !ok {
				return 0, 0x0a, true, nil
			}
			h.Loops.SetPaused(kind, args[1].Text, pause)
		case "pausewalk":
			for _, key := range h.Actors.Names() {
				actor := h.Actors.actors[key]
				if actor.Job == nil {
					continue
				}
				if strings.EqualFold(args[0].Text, "all") || strings.EqualFold(args[0].Text, actor.Name) {
					actor.Job.Pause = pauseAdjust(actor.Job.Pause, pause)
					actor.Job.Paused = actor.Job.Pause > 0
				}
			}
		}
		// Ball jobs have no counterpart in the port yet.
		return consumed, 0, true, nil
	case "currentscene", "currentdir":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if h.currentSet() == "" {
			return 0, 0x28, true, nil
		}
		if h.Env.SetView == nil {
			return 0, 0, true, fmt.Errorf("%w: %s has no view control", ErrHostOpcodeUnimplemented, name)
		}
		scene, direction := "", ""
		if h.Env.View != nil {
			scene, direction = h.Env.View()
		}
		if name == "currentscene" {
			scene = args[0].Text
		} else {
			switch strings.ToLower(args[0].Text) {
			case "north", "south", "east", "west":
				direction = strings.ToLower(args[0].Text)
			default:
				return 0, 0x0a, true, nil
			}
		}
		if err := h.Env.SetView(scene, direction); err != nil {
			return 0, 0, true, err
		}
		return consumed, 0, true, nil
	}
	return 0, 0, false, nil
}

// worldValue handles the point, string and view value builtins.
func (h *GameHost) worldValue(name string, call *ScriptCall) (Record, int, uint16, bool, error) {
	number := func(value int32, consumed int) (Record, int, uint16, bool, error) {
		return Record{Kind: 4, Data: uint32(value)}, consumed, 0, true, nil
	}
	text := func(value string, consumed int) (Record, int, uint16, bool, error) {
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: value})
		return record, consumed, status, true, err
	}
	switch name {
	case "pointx", "pointy":
		// FUN_00415CB0/FUN_00415D10: the high word is x, the low word y,
		// each sign-extended.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		point := unpackPoint(args[0].Int)
		if name == "pointx" {
			return number(int32(point[0]), consumed)
		}
		return number(int32(point[1]), consumed)
	case "makepoint":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 2 || args[0].Kind != 4 || args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		return number(packPoint([3]int16{int16(args[0].Int), int16(args[1].Int)}), consumed)
	case "stringlength":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		return number(int32(len(args[0].Text)), consumed)
	case "substring":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		return number(subString(args[0].Text, args[1].Text), consumed)
	case "findword":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 3 || args[0].Kind != 3 || args[1].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if args[2].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		return text(findWord(args[0].Text, args[1].Text, args[2].Int), consumed)
	case "pauseloop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		kind, ok := LoopKindByName(args[0].Text)
		paused := ok && h.Loops.PauseDepth(kind, args[1].Text) > 0
		return Record{Kind: 2, Data: boolWord(paused)}, consumed, 0, true, nil
	case "currentscene", "currentdir":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		scene, direction := "none", ""
		if h.currentSet() != "" && h.Env.View != nil {
			scene, direction = h.Env.View()
		}
		if name == "currentscene" {
			return text(strings.ToLower(scene), consumed)
		}
		return text(direction, consumed)
	}
	return Record{}, 0, 0, false, nil
}
