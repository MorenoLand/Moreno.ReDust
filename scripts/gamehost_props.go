package scripts

import (
	"fmt"
	"strings"
)

// PropRecord is the script-visible part of one prop block, the record
// FUN_004213C0 resolves by name and FUN_004214A0 stores back (propowner
// FUN_0041EB30/FUN_0041EDC0, propdeg FUN_0041F2B0/FUN_0041F4C0, propvisible
// FUN_0041F910/FUN_0041F9B0, propvalue FUN_0041EC00/FUN_0041EE40).
type PropRecord struct {
	Name string
	// Owner is the 15-byte owner name, "none" by default.
	Owner string
	// Degree is propdeg's value, masked to a byte.
	Degree int16
	// Value is propvalue's number.
	Value   int32
	Visible bool
	View    string
	Set     string
	Star    string
	X, Y, Z int32
	Scale   int32
	ZClip   int32
	Speed   int32
}

// ScriptProps holds the props scripts have touched, keyed case-insensitively.
type ScriptProps struct {
	props map[string]*PropRecord
	Balls [16]PropBallJob
}
type PropBallJob struct {
	Name   string
	Active bool
}

func NewScriptProps() *ScriptProps { return &ScriptProps{props: map[string]*PropRecord{}} }

// Get returns the named prop, creating it with native defaults on first use.
func (t *ScriptProps) Get(name string) *PropRecord {
	key := strings.ToLower(name)
	if prop, ok := t.props[key]; ok {
		return prop
	}
	prop := &PropRecord{Name: name, Owner: "none", Scale: 1000}
	t.props[key] = prop
	return prop
}

// Names lists the props in a stable order.
func (t *ScriptProps) Names() []string {
	names := make([]string, 0, len(t.props))
	for key := range t.props {
		names = append(names, key)
	}
	sortStrings(names)
	return names
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// propArgs evaluates the call's arguments and resolves the first as a prop
// name, in the order the native handlers do.
func (h *GameHost) propArgs(call *ScriptCall, count int) (*PropRecord, []ScriptValue, int, uint16, error) {
	args, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return nil, nil, 0, status, err
	}
	if len(args) != count {
		return nil, nil, 0, ScriptStatusMalformed, nil
	}
	if args[0].Kind != 3 {
		return nil, nil, 0, ScriptStatusWrongType, nil
	}
	if h.Props == nil {
		return nil, nil, 0, 0, fmt.Errorf("props are unavailable")
	}
	return h.Props.Get(args[0].Text), args, consumed, 0, nil
}

// propCommand handles the prop setters. handled is false for other opcodes.
func (h *GameHost) propCommand(name string, call *ScriptCall) (consumed int, status uint16, handled bool, err error) {
	switch name {
	case "propowner", "propview", "propset", "propstar":
		prop, args, consumed, status, err := h.propArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if args[1].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if status := nativeName(args[1].Text); status != 0 {
			return 0, status, true, nil
		}
		switch name {
		case "propowner":
			prop.Owner = args[1].Text
		case "propview":
			prop.View = args[1].Text
		case "propset":
			prop.Set = args[1].Text
		case "propstar":
			prop.Star = args[1].Text
		}
		if name == "propset" || name == "propstar" {
			h.Props.StopBall(prop.Name)
			if strings.EqualFold(prop.Set, h.currentSet()) && h.Env.ResolveStar != nil {
				if position, ok := h.Env.ResolveStar(prop.Set, prop.Star); ok {
					prop.X, prop.Y, prop.Z = int32(position[0]), int32(position[1]), int32(position[2])
				}
			}
		}
		return consumed, 0, true, nil
	case "sendtoprop":
		consumed, status, err := h.sendToProp(call)
		return consumed, status, true, err
	case "stopball":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if status := nativeName(args[0].Text); status != 0 {
			return 0, status, true, nil
		}
		if h.Props == nil {
			return 0, 0, true, fmt.Errorf("props are unavailable")
		}
		h.Props.StopBall(args[0].Text)
		return consumed, 0, true, nil
	case "propdeg", "propvalue", "propscale", "propzclip", "propspeed":
		prop, args, consumed, status, err := h.propArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if args[1].Kind != 4 {
			return 0, ScriptStatusWrongType, true, nil
		}
		switch name {
		case "propdeg":
			prop.Degree = int16(args[1].Int & 0xff)
		case "propvalue":
			prop.Value = args[1].Int
		case "propscale":
			prop.Scale = args[1].Int
		case "propzclip":
			prop.ZClip = args[1].Int
		case "propspeed":
			prop.Speed = args[1].Int
		}
		return consumed, 0, true, nil
	case "propvisible":
		prop, args, consumed, status, err := h.propArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if args[1].Kind != 2 {
			return 0, ScriptStatusWrongType, true, nil
		}
		prop.Visible = args[1].Int != 0
		return consumed, 0, true, nil
	case "propxy", "propxyz":
		count := 3
		if name == "propxyz" {
			count = 4
		}
		prop, args, consumed, status, err := h.propArgs(call, count)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		for _, arg := range args[1:] {
			if arg.Kind != 4 {
				return 0, ScriptStatusWrongType, true, nil
			}
		}
		prop.X, prop.Y = args[1].Int, args[2].Int
		if name == "propxyz" {
			prop.Z = args[3].Int
		}
		return consumed, 0, true, nil
	case "propinstance":
		// FUN_0041E840: propinstance(source, name) appends a copy of the source
		// prop under a new name of at most 15 characters, unless that name is
		// already a prop.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if len(args[1].Text) > 15 {
			return 0, 0x1a, true, nil
		}
		if h.Env.PropInstance == nil {
			return 0, 0, true, fmt.Errorf("%w: propinstance", ErrHostOpcodeUnimplemented)
		}
		status, added := h.Env.PropInstance(args[0].Text, args[1].Text)
		if status != 0 {
			return 0, status, true, nil
		}
		if added && h.Props != nil {
			copied := *h.Props.Get(args[0].Text)
			copied.Name = args[1].Text
			h.Props.props[strings.ToLower(args[1].Text)] = &copied
		}
		return consumed, 0, true, nil
	case "voicesound", "singlesound", "dualsound", "multiplesound":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) == 0 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if name == "voicesound" && h.Env.Voice != nil {
			return consumed, 0, true, h.Env.Voice(args[0].Text)
		}
		if h.Env.Sound == nil {
			return 0, 0, true, fmt.Errorf("%w: %s has no audio output", ErrHostOpcodeUnimplemented, name)
		}
		return consumed, 0, true, h.Env.Sound(args[0].Text)
	}
	return 0, 0, false, nil
}

// propValue handles the prop getters.
func (h *GameHost) propValue(name string, call *ScriptCall) (Record, int, uint16, bool, error) {
	switch name {
	case "propowner", "propdeg", "propvalue", "propvisible", "propview", "propset", "propstar", "propscale", "propzclip", "propspeed":
		prop, _, consumed, status, err := h.propArgs(call, 1)
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		switch name {
		case "propowner", "propview", "propset", "propstar":
			text := map[string]string{"propowner": prop.Owner, "propview": prop.View, "propset": prop.Set, "propstar": prop.Star}[name]
			record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: text})
			return record, consumed, status, true, err
		case "propvisible":
			return Record{Kind: 2, Data: boolWord(prop.Visible)}, consumed, 0, true, nil
		}
		number := map[string]int32{"propdeg": int32(prop.Degree), "propvalue": prop.Value, "propscale": prop.Scale, "propzclip": prop.ZClip, "propspeed": prop.Speed}[name]
		return Record{Kind: 4, Data: uint32(number)}, consumed, 0, true, nil
	case "propxy", "propxyz":
		count := 2
		if name == "propxyz" {
			count = 2
		}
		prop, args, consumed, status, err := h.propArgs(call, count)
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		switch args[1].Int {
		case 1:
			return Record{Kind: 4, Data: uint32(prop.X)}, consumed, 0, true, nil
		case 2:
			return Record{Kind: 4, Data: uint32(prop.Y)}, consumed, 0, true, nil
		case 3:
			return Record{Kind: 4, Data: uint32(prop.Z)}, consumed, 0, true, nil
		}
		return Record{}, 0, ScriptStatusWrongType, true, nil
	}
	return Record{}, 0, 0, false, nil
}
func (t *ScriptProps) StopBall(name string) {
	for index := range t.Balls {
		if strings.EqualFold(name, "all") {
			t.Balls[index].Active = false
			continue
		}
		if t.Balls[index].Active && strings.EqualFold(t.Balls[index].Name, name) {
			t.Balls[index].Active = false
			return
		}
	}
}
func (h *GameHost) sendToProp(call *ScriptCall) (int, uint16, error) {
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
	if status := nativeName(value.Text); status != 0 {
		return 0, status, nil
	}
	message := call.Start + 2 + consumed
	if call.Kind(message) != opComma {
		return 0, ScriptStatusMissingComma, nil
	}
	message++
	if h.Env.PropScript == nil || h.Env.Program == nil {
		return 0, 0, fmt.Errorf("%w: sendtoprop has no prop script resolver", ErrHostOpcodeUnimplemented)
	}
	file, resource, shop, found := h.Env.PropScript(value.Text)
	if !found {
		return 0, 0x0a, nil
	}
	program, err := h.Env.Program(file, resource)
	if err != nil {
		return 0, 0, err
	}
	shared, me, status, err := h.shopScript(shop)
	if err != nil || status != 0 {
		return 0, status, err
	}
	chain := []ScriptFrame{{Program: program, Me: value.Text, Target: value.Text, Label: "Prop Script: "}, {Program: shared, Me: me, Target: value.Text, Label: "Shop Script: ", Last: true}}
	used, status, err := call.Interpreter.Call(chain, call.Chain, call.FrameIndex, call.Locals, call.Program, message)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(message+used) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return message + used + 1 - call.Start, 0, nil
}
