package scripts

import (
	"fmt"
	"strconv"
	"strings"

	"redust/assets"
)

// PuppetPresenter is the game side of a conversation: it owns the artwork,
// voice and choice panel, and reports when a blocking command may continue.
type PuppetPresenter interface {
	// OpenPuppet loads a PUP's artwork and line table.
	OpenPuppet(file string) error
	// ClosePuppet releases the open puppet and returns to the world.
	ClosePuppet() error
	// Speak starts a line from the open puppet's table. found is false when
	// the name is not a line, in which case native shows the text instead.
	Speak(line string) (found bool, err error)
	// Speaking is true while the line plays; Skipped reports a click that
	// cut the line short, which skips the rest until the next puppetevent.
	Speaking() bool
	Skipped() bool
	// Choose shows the choice panel; Chosen reports the clicked event.
	Choose(choices []PuppetChoice) error
	Chosen() (int32, bool)
	// Show switches the displayed layer for screentoblack/blacktoscreen and
	// visualeffect: "puppet", "set", "flat" or "current".
	Show(layer string) error
	// Cursor sets the pointer shape.
	Cursor(name string) error
	// GotoFlat switches the displayed flat ("avatar" is the inventory
	// screen, "mainpanel" the world panel).
	GotoFlat(name string) error
	// PickInventory opens the inventory for choosing a hand item; Picked
	// reports when the player has finished and left it.
	PickInventory() error
	Picked() bool
}

// puppetSession is the native open-puppet state: DAT_00459A92/94 and the
// choice list DAT_00442430/DAT_00442434.
type puppetSession struct {
	file    string
	info    assets.PuppetFile
	choices []PuppetChoice
	skipped bool
}

// SetTask records the task whose goroutine is executing script work, so
// blocking commands can suspend it.
func (h *GameHost) SetTask(task *ScriptTask) { h.task = task }

// Task returns the current script task.
func (h *GameHost) Task() *ScriptTask { return h.task }

// PuppetOpen reports whether a puppet file is open.
func (h *GameHost) PuppetOpen() bool { return h.puppet != nil }

// PassRequested reports whether the suspended task is waiting for one
// scheduler pass (forceupdate), and Passed acknowledges that pass.
func (h *GameHost) PassRequested() bool { return h.passWanted }
func (h *GameHost) Passed()             { h.passWanted = false }

func (h *GameHost) wait(ready func() bool) error {
	if !h.task.Running() {
		return ErrNotInTask
	}
	h.task.Wait(ready)
	return nil
}

func (h *GameHost) waitTicks(ticks int32) error {
	if ticks <= 0 || h.Env.Ticks == nil {
		return nil
	}
	until := h.Env.Ticks() + uint32(ticks)
	return h.wait(func() bool { return int32(h.Env.Ticks()-until) >= 0 })
}

// sendToScript runs `name, message(args)` against a single-frame chain, the
// shape sendtopuppet (FUN_00408950), sendtoshop (FUN_00421040) and
// sendtocast use. result is the fx variants' return slot.
func (h *GameHost) sendToScript(call *ScriptCall, label string, resolve func(name string) (*Program, string, uint16, error), result *Record) (int, uint16, error) {
	if call.Kind(call.Start+1) != opOpen {
		return 0, ScriptStatusMalformed, nil
	}
	target, consumed, status, err := call.Eval(call.Start + 2)
	if err != nil || status != 0 {
		return 0, status, err
	}
	value, status, err := call.Interpreter.Value(target)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if value.Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	message := call.Start + 2 + consumed
	if call.Kind(message) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	message++
	program, me, status, err := resolve(value.Text)
	if err != nil || status != 0 {
		return 0, status, err
	}
	chain := []ScriptFrame{{Program: program, Me: me, Target: me, Label: label, Last: true}}
	used, status, err := call.Interpreter.callCode(chain, 0, call.Chain, call.FrameIndex, call.Locals, call.Program, message, result)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(message+used) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return message + used + 1 - call.Start, 0, nil
}

func (h *GameHost) puppetScript(name string) (*Program, string, uint16, error) {
	if h.puppet == nil {
		return nil, "", 0x2d, nil
	}
	for _, script := range h.puppet.info.Scripts {
		if strings.EqualFold(script.Name, name) {
			if h.Env.Program == nil {
				return nil, "", 0, fmt.Errorf("puppet scripts have no loader")
			}
			program, err := h.Env.Program(h.puppet.file, script.Resource)
			return program, script.Name, 0, err
		}
	}
	return nil, "", 0x0a, nil
}

func (h *GameHost) shopScript(name string) (*Program, string, uint16, error) {
	if h.Env.Shop == nil || h.Env.Program == nil {
		return nil, "", 0, fmt.Errorf("sendtoshop has no shop table")
	}
	file, resource, ok := h.Env.Shop(name)
	if !ok {
		return nil, "", 0x0a, nil
	}
	program, err := h.Env.Program(file, resource)
	return program, strings.ToLower(name), 0, err
}

// parseCoordinateStar is FUN_0041B960: a star written "x,y,z".
func parseCoordinateStar(star string) ([3]int16, bool) {
	parts := strings.Split(star, ",")
	if len(parts) != 3 {
		return [3]int16{}, false
	}
	var position [3]int16
	for index, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return [3]int16{}, false
		}
		position[index] = int16(value)
	}
	return position, true
}

// puppetCommand handles the conversation and pacing statements. handled is
// false for opcodes it does not own.
func (h *GameHost) puppetCommand(name string, call *ScriptCall) (consumed int, status uint16, handled bool, err error) {
	present := h.Env.Presenter
	needPresenter := func() error {
		if present == nil {
			return fmt.Errorf("%s has no puppet presenter", name)
		}
		return nil
	}
	switch name {
	case "openpuppetfile":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.puppet != nil {
			return 0, 0x2d, true, nil
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if err := needPresenter(); err != nil {
			return 0, 0, true, err
		}
		file := args[0].Text
		if !strings.Contains(file, "/") {
			file = "PUPPETS/" + strings.ToUpper(file)
		}
		if h.Env.PuppetFile == nil {
			return 0, 0, true, fmt.Errorf("openpuppetfile has no file reader")
		}
		info, err := h.Env.PuppetFile(file)
		if err != nil {
			return 0, 0, true, err
		}
		if err := present.OpenPuppet(file); err != nil {
			return 0, 0, true, err
		}
		h.puppet = &puppetSession{file: file, info: info}
		// FUN_00408310 then sends openpuppet() to the boot script.
		if len(info.Scripts) > 0 {
			status, err := call.Interpreter.RunSource(nil, fmt.Sprintf("sendtopuppet(%q,openpuppet())", info.Scripts[0].Name))
			if err != nil || status != 0 {
				return 0, status, true, err
			}
		}
		return consumed, 0, true, nil
	case "closepuppetfile":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.puppet == nil {
			return 0, 0x2d, true, nil
		}
		if len(h.puppet.info.Scripts) > 0 {
			status, err := call.Interpreter.RunSource(nil, fmt.Sprintf("sendtopuppet(%q,closepuppet())", h.puppet.info.Scripts[0].Name))
			if err != nil || status != 0 {
				return 0, status, true, err
			}
		}
		h.puppet = nil
		if err := needPresenter(); err != nil {
			return 0, 0, true, err
		}
		return consumed, 0, true, present.ClosePuppet()
	case "sendtopuppet":
		consumed, status, err := h.sendToScript(call, "Puppet Message: ", h.puppetScript, nil)
		return consumed, status, true, err
	case "sendtoshop":
		consumed, status, err := h.sendToScript(call, "Shop Message: ", h.shopScript, nil)
		return consumed, status, true, err
	case "puppetspeak":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.puppet == nil {
			return 0, 0x2d, true, nil
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if err := needPresenter(); err != nil {
			return 0, 0, true, err
		}
		if h.puppet.skipped {
			// DAT_00442E60: a clicked-through line skips the rest until the
			// next puppetevent.
			return consumed, 0, true, nil
		}
		found, err := present.Speak(args[0].Text)
		if err != nil {
			return 0, 0, true, err
		}
		if !found {
			// FUN_0040A6F0 shows the text; FUN_0042B730 waits len/2+60.
			return consumed, 0, true, h.waitTicks(int32(len(args[0].Text))/2 + 60)
		}
		if err := h.wait(func() bool { return !present.Speaking() }); err != nil {
			return 0, 0, true, err
		}
		if present.Skipped() {
			h.puppet.skipped = true
		}
		return consumed, 0, true, nil
	case "puppetclear":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.puppet == nil {
			return 0, 0x2d, true, nil
		}
		h.puppet.choices = h.puppet.choices[:0]
		return consumed, 0, true, nil
	case "puppetbevel":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.puppet == nil {
			return 0, 0x2d, true, nil
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 4 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if len(h.puppet.choices) > 4 {
			return 0, 0x2e, true, nil
		}
		h.puppet.choices = append(h.puppet.choices, PuppetChoice{Text: args[0].Text, EventID: args[1].Int})
		return consumed, 0, true, nil
	case "puppetscramble":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.puppet == nil {
			return 0, 0x2d, true, nil
		}
		PuppetScrambleChoices(h.puppet.choices, h.Random)
		return consumed, 0, true, nil
	case "delay":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return 0, ScriptStatusWrongType, true, nil
		}
		return consumed, 0, true, h.waitTicks(args[0].Int)
	case "forceupdate":
		// FUN_00426200 runs one FUN_0040F4E0 pass; the task waits for the
		// game loop to run it.
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if !h.task.Running() {
			return consumed, 0, true, nil
		}
		h.passWanted = true
		return consumed, 0, true, h.wait(func() bool { return !h.passWanted })
	case "screentoblack", "blacktoscreen":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 2 || args[1].Kind != 4 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if err := needPresenter(); err != nil {
			return 0, 0, true, err
		}
		layer := "current"
		if args[0].Kind == 3 {
			layer = strings.ToLower(args[0].Text)
		}
		// The native palette ramp (FUN_0042EA50/FUN_0042EAA0) runs for the
		// given number of ticks; the port switches the layer and waits.
		if name == "blacktoscreen" {
			if err := present.Show(layer); err != nil {
				return 0, 0, true, err
			}
		}
		return consumed, 0, true, h.waitTicks(args[1].Int)
	case "visualeffect":
		// visualeffect(<effect keyword>, ticks): the keyword is a 24000-band
		// record, not an expression.
		if call.Kind(call.Start+1) != opOpen {
			return 0, ScriptStatusMalformed, true, nil
		}
		effect := call.Kind(call.Start + 2)
		if effect < 24000 || call.Kind(call.Start+3) != opComma {
			return 0, ScriptStatusWrongType, true, nil
		}
		ticks, used, status, err := call.Eval(call.Start + 4)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if ticks.Kind != 4 || call.Kind(call.Start+4+used) != opClose {
			return 0, ScriptStatusMalformed, true, nil
		}
		return 4 + used + 1, 0, true, h.waitTicks(int32(ticks.Data))
	case "cursor":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if present != nil {
			if err := present.Cursor(args[0].Text); err != nil {
				return 0, 0, true, err
			}
		}
		return consumed, 0, true, nil
	case "gotoflat":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if err := needPresenter(); err != nil {
			return 0, 0, true, err
		}
		return consumed, 0, true, present.GotoFlat(args[0].Text)
	case "setvisible", "puppetvisible":
		// The layer switch happens through gotoflat/blacktoscreen; the flag
		// itself has no separate effect in the port.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 2 {
			return 0, ScriptStatusWrongType, true, nil
		}
		return consumed, 0, true, nil
	case "flushevents":
		_, consumed, status, err := call.Args()
		return consumed, status, true, err
	case "puppetgrab":
		// FUN_00408700: the argument must be a boolean; it sets DAT_004599AC.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 2 {
			return 0, ScriptStatusWrongType, true, nil
		}
		h.PuppetGrab = args[0].Int != 0
		return consumed, 0, true, nil
	case "puppetbase":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		h.PuppetBase = args[0].Text
		return consumed, 0, true, nil
	}
	return 0, 0, false, nil
}

// puppetValue handles the conversation value builtins.
func (h *GameHost) puppetValue(name string, call *ScriptCall) (Record, int, uint16, bool, error) {
	switch name {
	case "puppetevent":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if h.puppet == nil {
			return Record{}, 0, 0x2d, true, nil
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.Presenter == nil {
			return Record{}, 0, 0, true, fmt.Errorf("puppetevent has no puppet presenter")
		}
		h.puppet.skipped = false
		choices := append([]PuppetChoice(nil), h.puppet.choices...)
		if err := h.Env.Presenter.Choose(choices); err != nil {
			return Record{}, 0, 0, true, err
		}
		var event int32
		if err := h.wait(func() bool {
			chosen, ok := h.Env.Presenter.Chosen()
			event = chosen
			return ok
		}); err != nil {
			return Record{}, 0, 0, true, err
		}
		return Record{Kind: 4, Data: uint32(event)}, consumed, 0, true, nil
	case "sendtopuppetfx", "sendtoshopfx":
		resolve, label := h.puppetScript, "Puppet Message: "
		if name == "sendtoshopfx" {
			resolve, label = h.shopScript, "Shop Message: "
		}
		var result Record
		consumed, status, err := h.sendToScript(call, label, resolve, &result)
		return result, consumed, status, true, err
	case "countpuppets", "countbevels", "currentpuppet":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		switch name {
		case "countpuppets":
			if h.puppet == nil {
				return Record{}, 0, 0x2d, true, nil
			}
			return Record{Kind: 4, Data: uint32(len(h.puppet.info.Scripts))}, consumed, 0, true, nil
		case "countbevels":
			count := 0
			if h.puppet != nil {
				count = len(h.puppet.choices)
			}
			return Record{Kind: 4, Data: uint32(count)}, consumed, 0, true, nil
		}
		text := "None"
		if h.puppet != nil {
			text = h.puppet.info.Name
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: text})
		return record, consumed, status, true, err
	case "indextopuppet":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if h.puppet == nil {
			return Record{}, 0, 0x2d, true, nil
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		index := int(args[0].Int)
		if index < 1 || index > len(h.puppet.info.Scripts) {
			return Record{}, 0, 0x0a, true, nil
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: h.puppet.info.Scripts[index-1].Name})
		return record, consumed, status, true, err
	case "walkdest":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		destination := ""
		if actor, missing := h.Actors.Lookup(args[0].Text); missing == 0 && actor.Job != nil && actor.Job.Mode != ActorJobTurn {
			destination = actor.Job.Target
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: destination})
		return record, consumed, status, true, err
	case "currentdeg":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		heading := int32(-1)
		if h.currentSet() != "" && h.Env.PlayerHeading != nil {
			heading = int32(h.Env.PlayerHeading())
		}
		return Record{Kind: 4, Data: uint32(heading)}, consumed, 0, true, nil
	case "calcvectx", "calcvecty":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 2 || args[0].Kind != 4 || args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.Vector == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%s has no vector function", name)
		}
		dx, dy := h.Env.Vector(int16(args[0].Int), int16(args[1].Int))
		value := dx
		if name == "calcvecty" {
			value = dy
		}
		return Record{Kind: 4, Data: uint32(int32(value))}, consumed, 0, true, nil
	case "currentflat":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if h.Env.CurrentFlat == nil {
			return Record{}, 0, 0, true, fmt.Errorf("currentflat has no flat source")
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: h.Env.CurrentFlat()})
		return record, consumed, status, true, err
	case "numtostring":
		// FUN_004164A0 -> FUN_0042E7D0: signed decimal.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: strconv.FormatInt(int64(args[0].Int), 10)})
		return record, consumed, status, true, err
	case "stringtonum":
		// FUN_00416420 -> FUN_0042EBC0: sscanf "%d", zero when nothing parses.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		return Record{Kind: 4, Data: uint32(scanDecimal(args[0].Text))}, consumed, 0, true, nil
	case "propdeg":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.PropDegree == nil {
			return Record{}, 0, 0, true, fmt.Errorf("propdeg has no prop table")
		}
		degree, ok := h.Env.PropDegree(args[0].Text)
		if !ok {
			return Record{}, 0, 0x0a, true, nil
		}
		return Record{Kind: 4, Data: uint32(int32(degree))}, consumed, 0, true, nil
	}
	return Record{}, 0, 0, false, nil
}

// scanDecimal is sscanf("%d"): optional leading space and sign, then digits.
func scanDecimal(text string) int32 {
	text = strings.TrimLeft(text, " \t\r\n")
	end := 0
	if end < len(text) && (text[end] == '-' || text[end] == '+') {
		end++
	}
	digits := end
	for end < len(text) && text[end] >= '0' && text[end] <= '9' {
		end++
	}
	if end == digits {
		return 0
	}
	value, err := strconv.ParseInt(text[:end], 10, 64)
	if err != nil {
		return 0
	}
	return int32(value)
}

// PickInventoryBuiltin replaces INVEN.PRP's handleselect: the player chooses
// from the inventory screen until they leave it, and the engine's hand item is
// then published back into the script's globals.
func (h *GameHost) PickInventoryBuiltin(call *ScriptCall) (int, uint16, error) {
	_, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return 0, status, err
	}
	if h.Env.Presenter == nil {
		return 0, 0, fmt.Errorf("handleselect has no presenter")
	}
	if err := h.Env.Presenter.PickInventory(); err != nil {
		return 0, 0, err
	}
	if err := h.wait(h.Env.Presenter.Picked); err != nil {
		return 0, 0, err
	}
	if h.Env.Sync != nil {
		if err := h.Env.Sync(); err != nil {
			return 0, 0, err
		}
	}
	return consumed, 0, nil
}
