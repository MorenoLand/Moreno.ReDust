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

	// Frame is the sequence cursor at record byte +0x24. FUN_00421B70 selects the
	// frame it points at and steps it once per pump, wrapping at the view's count;
	// see assets.NextFrameCursor. Its starting value for a new record is unverified.
	Frame int16

	// Projected is record word 9 being non-zero, which selects path B of the
	// projection (FUN_00421820). propset (FUN_0041E9E0), propstar (FUN_0041EF60) and
	// propxyz (FUN_0041FBD0) each store 1 there, and propxy (FUN_0041FDF0) stores 0.
	// The port's new records start at 0, which is path A. The shop record constructor
	// FUN_00420370 can set word 9 to 1 when the record's set is the open set and its
	// star resolves (FUN_0041B850 not traced); the port does not build those records.
	Projected bool
}

// ScriptProps holds the props scripts have touched, keyed case-insensitively.
type ScriptProps struct {
	props map[string]*PropRecord
	// PlayerBox is the player's hit box: [0] the z half-size, [1] the x and y
	// half-size (actorhitbox("player", z, xy)).
	PlayerBox [2]int16
	// table is the game's prop table in native order: each open shop's props in
	// list order, then the props propinstance adds.
	table []string
	Balls [16]PropBallJob
}

// AddToTable appends a prop name to the prop table.
func (t *ScriptProps) AddToTable(name string) { t.table = append(t.table, name) }

// TableCount is countprops: the number of props in the table.
func (t *ScriptProps) TableCount() int { return len(t.table) }

// TableName is indextoprop: the 1-based entry, false when it is out of range.
func (t *ScriptProps) TableName(index int) (string, bool) {
	if index < 1 || index > len(t.table) {
		return "", false
	}
	return t.table[index-1], true
}

// Lookup resolves a prop name the way FUN_004213C0 does: the name must be in the
// prop table, and the first match in table order is the record. Unlike Get it never
// creates a record, so a read by pointinprop leaves the store as it found it. A
// table name the scripts never touched reads as the native default record, which
// is hidden and has no view.
func (t *ScriptProps) Lookup(name string) (*PropRecord, bool) {
	for _, entry := range t.table {
		if !strings.EqualFold(entry, name) {
			continue
		}
		if prop, ok := t.props[strings.ToLower(name)]; ok {
			return prop, true
		}
		return &PropRecord{Name: name, Owner: "none", Scale: 1000}, true
	}
	return nil, false
}

// PropBallJob is one ball slot, the 48-byte job FUN_00410760 builds: speeds
// (+0x04..+0x08), the gravity and bounce flags (+0x0A, +0x0C), the lifetime in
// passes (+0x0E) and the fine position (+0x12..+0x16) that is the prop's world
// position, with the cells at +0x18 and +0x1A.
type PropBallJob struct {
	Name     string
	Active   bool
	VX, VY   int16
	VZ       int16
	Gravity  bool
	Bounce   bool
	Lifetime int16
	X, Y, Z  int16
	CellX    int16
	CellY    int16
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

// propReadArgs reads the count arguments of a prop statement or value the way the
// native readers do: FUN_00417070 for two (propset, propstar and the value forms of
// propxy and propxyz), FUN_00417100 for three (propxy) and FUN_004171C0 for four
// (propxyz). Each argument is evaluated in turn. A comma must follow each argument but
// the last, and it is checked before the next argument is read: 0x1C when it is
// missing. An argument that starts with a closing paren or a comma has no expression,
// so FUN_004220A0 reaches FUN_00422040 and returns 0x0E. That makes propxy() 0x0E.
//
// Nothing after the count-th argument is read here. The native dispatcher checks it
// after the handler has run (FUN_00424890, FUN_004137B0), so extra reports that the
// next token is not the closing paren, and the caller runs its work and then returns 2.
func (h *GameHost) propReadArgs(call *ScriptCall, count int) ([]ScriptValue, int, bool, uint16, error) {
	if call.Kind(call.Start+1) != opOpen {
		return nil, 0, false, ScriptStatusMalformed, nil
	}
	var args []ScriptValue
	position := call.Start + 2
	for index := 0; index < count; index++ {
		if index > 0 {
			if call.Kind(position) != opComma {
				return nil, 0, false, ScriptStatusMissingComma, nil
			}
			position++
		}
		if kind := call.Kind(position); kind == opClose || kind == opComma {
			return nil, 0, false, ScriptStatusWrongType, nil
		}
		record, used, status, err := call.Eval(position)
		if err != nil || status != 0 {
			return nil, 0, false, status, err
		}
		value, status, err := call.Interpreter.Value(record)
		if err != nil || status != 0 {
			return nil, 0, false, status, err
		}
		args = append(args, value)
		position += used
	}
	if call.Kind(position) == opClose {
		return args, position + 1 - call.Start, false, 0, nil
	}
	return args, position - call.Start, true, 0, nil
}

// propStatementArgs reads a prop setter's two arguments and checks the name the
// native way: the readers run first (0x1C for a missing comma, 0x0E for an empty
// argument), then the name must be a string (0x0E) and in the prop table (0x0A).
// Unlike Get it creates no record, so an unknown name leaves the store as it was.
// The caller checks its value and stores, and reports extra arguments as 2 after the
// store, since FUN_00424890 runs the handler before it checks the closing paren.
func (h *GameHost) propStatementArgs(call *ScriptCall) ([]ScriptValue, int, bool, uint16, error) {
	args, consumed, extra, status, err := h.propReadArgs(call, 2)
	if err != nil || status != 0 {
		return nil, 0, false, status, err
	}
	if h.Props == nil {
		return nil, 0, false, 0, fmt.Errorf("props are unavailable")
	}
	if args[0].Kind != 3 {
		return nil, 0, false, ScriptStatusWrongType, nil
	}
	if !h.Props.Known(args[0].Text) {
		return nil, 0, false, StatusNameNotFound, nil
	}
	return args, consumed, extra, 0, nil
}

// propCommand handles the prop setters. handled is false for other opcodes.
func (h *GameHost) propCommand(name string, call *ScriptCall) (consumed int, status uint16, handled bool, err error) {
	switch name {
	case "propset", "propstar":
		// FUN_0041E9E0 (propset) and FUN_0041EF60 (propstar) share these checks. The
		// reader parses both arguments first (0x1C), then the name must be a string
		// (0x0E) and in the prop table (0x0A, which creates no record). The value must
		// be a string (0x0E) of at most 15 bytes (0x1A). Then the ball stops, the value
		// goes into the set (propset) or the star (propstar), word 9 is set and the
		// record is written back. Extra arguments run all of that and return 2.
		args, consumed, extra, status, err := h.propReadArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.Props == nil {
			return 0, 0, true, fmt.Errorf("props are unavailable")
		}
		if args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if !h.Props.Known(args[0].Text) {
			return 0, StatusNameNotFound, true, nil
		}
		if args[1].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if status := nativeName(args[1].Text); status != 0 {
			return 0, status, true, nil
		}
		h.Props.StopBall(args[0].Text)
		prop := h.Props.Get(args[0].Text)
		if name == "propset" {
			prop.Set = args[1].Text
		} else {
			prop.Star = args[1].Text
		}
		prop.Projected = true
		// The record's set is compared with the open set (FUN_0042E5B0 against
		// DAT_00459A82). propset has just stored the new set, and propstar leaves the set
		// alone, so each compares the set it means. On a match the record's star is
		// resolved (FUN_0041B850) into words 13 to 15.
		if strings.EqualFold(prop.Set, h.currentSet()) && h.Env.ResolveStar != nil {
			if position, ok := h.Env.ResolveStar(prop.Set, prop.Star); ok {
				prop.X, prop.Y, prop.Z = int32(position[0]), int32(position[1]), int32(position[2])
			}
		}
		if extra {
			return consumed, ScriptStatusMalformed, true, nil
		}
		return consumed, 0, true, nil
	case "propowner":
		// FUN_0041EB30. The readers run first, then the name (0x0E, and 0x0A with no record
		// created), then the value must be a string (0x0E) of at most 15 bytes (0x1A). The
		// owner is copied into the record, which FUN_004214A0 stores. Extra arguments run
		// all of that and then return 2.
		args, consumed, extra, status, err := h.propStatementArgs(call)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if args[1].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if status := nativeName(args[1].Text); status != 0 {
			return 0, status, true, nil
		}
		h.Props.Get(args[0].Text).Owner = args[1].Text
		if extra {
			return consumed, ScriptStatusMalformed, true, nil
		}
		return consumed, 0, true, nil
	case "propview":
		// FUN_0041F130. The readers run first, then the name (0x0E, and 0x0A with no record
		// created), then the value must be a string (0x0E) of at most 15 bytes (0x1A). The
		// native then tests the view against the prop's view list (FUN_00421D00, 0x0A when it
		// is absent). That list is not modelled, so any view name is accepted here. The
		// frame cursor at record +0x24 is cleared, the view string is copied into the record,
		// and FUN_004214A0 stores it. Extra arguments run all of that and then return 2.
		args, consumed, extra, status, err := h.propStatementArgs(call)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if args[1].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if status := nativeName(args[1].Text); status != 0 {
			return 0, status, true, nil
		}
		prop := h.Props.Get(args[0].Text)
		prop.View = args[1].Text
		prop.Frame = 0
		if extra {
			return consumed, ScriptStatusMalformed, true, nil
		}
		return consumed, 0, true, nil
	case "makeball":
		consumed, status, err := h.makeBall(call)
		return consumed, status, true, err
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
	case "propxyz":
		// FUN_0041FBD0. FUN_004171C0 reads four arguments (0x1C for a missing comma). The
		// name must be a string (0x0E) and in the prop table (0x0A, which creates no
		// record). Arguments 2 to 4 must be numbers (0x0E). The ball stops, words 13, 14
		// and 15 take the low halves of the three numbers, and word 9 is set. Extra
		// arguments run all of that and return 2.
		args, consumed, extra, status, err := h.propReadArgs(call, 4)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.Props == nil {
			return 0, 0, true, fmt.Errorf("props are unavailable")
		}
		if args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if !h.Props.Known(args[0].Text) {
			return 0, StatusNameNotFound, true, nil
		}
		for _, arg := range args[1:] {
			if arg.Kind != 4 {
				return 0, ScriptStatusWrongType, true, nil
			}
		}
		h.Props.StopBall(args[0].Text)
		prop := h.Props.Get(args[0].Text)
		prop.X, prop.Y, prop.Z = int32(int16(args[1].Int)), int32(int16(args[2].Int)), int32(int16(args[3].Int))
		prop.Projected = true
		if extra {
			return consumed, ScriptStatusMalformed, true, nil
		}
		return consumed, 0, true, nil
	case "propxy":
		// FUN_0041FDF0. FUN_00417100 reads three arguments, so propxy() is 0x0E. The name
		// must be a string (0x0E) and in the prop table (0x0A, no record created), then
		// the two coordinates must be numbers (0x0E). Extra arguments run all of that and
		// return 2.
		args, consumed, extra, status, err := h.propReadArgs(call, 3)
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.Props == nil {
			return 0, 0, true, fmt.Errorf("props are unavailable")
		}
		if args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if !h.Props.Known(args[0].Text) {
			return 0, StatusNameNotFound, true, nil
		}
		for _, arg := range args[1:] {
			if arg.Kind != 4 {
				return 0, ScriptStatusWrongType, true, nil
			}
		}
		// FUN_0040F7D0 stops the prop's ball, a slot whose active word is cleared, and
		// the record is stored after that (FUN_004214A0). The statement stores the low
		// 16-bit word of each number (MOV word ptr): argument 2 goes to word 11 (x, the
		// high word of the anchor) and argument 3 to word 10 (y, the low word), and word
		// 9 is cleared (FUN_0041FDF0). The port keeps those two words in X and Y.
		h.Props.StopBall(args[0].Text)
		prop := h.Props.Get(args[0].Text)
		prop.X, prop.Y = int32(int16(args[1].Int)), int32(int16(args[2].Int))
		prop.Projected = false
		if extra {
			return consumed, ScriptStatusMalformed, true, nil
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
			h.Props.AddToTable(args[1].Text)
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
	case "propxy":
		// FUN_0041FEE0. FUN_00417070 reads the name and the selector, so a call with no
		// arguments is 0x0E. The name must be a string (0x0E) and in the prop table
		// (0x0A, no record created), and the selector must be a number (0x0E).
		args, consumed, extra, status, err := h.propReadArgs(call, 2)
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if h.Props == nil {
			return Record{}, 0, 0, true, fmt.Errorf("props are unavailable")
		}
		if args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		prop, found := h.Props.Lookup(args[0].Text)
		if !found {
			return Record{}, 0, StatusNameNotFound, true, nil
		}
		if args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		// The selector reads the anchor words the statement stored: word 10 is y (the
		// low half) and word 11 is x (the high half), so the packed value is x<<16|y.
		// Propxy (proptable.go, FUN_0041FEE0) applies the selector: 1 and 2 are the
		// sign-extended halves, 3 is the whole dword, and anything else is 0x0E.
		packed := uint32(uint16(int16(prop.Y))) | uint32(uint16(int16(prop.X)))<<16
		value, _, err := Propxy(packed, args[1].Int)
		if err != nil {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if extra {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		return Record{Kind: 4, Data: uint32(value)}, consumed, 0, true, nil
	case "propxyz":
		// FUN_0041FCD0. FUN_00417070 reads the name and the selector (0x1C for a missing
		// comma). The name must be a string (0x0E) and in the prop table (0x0A, no record
		// created), and then the selector must be a number (0x0E). Selectors 1, 2 and 3
		// are words 13, 14 and 15, sign-extended, and selector 4 is words 13 and 14 as one
		// dword with word 13 high. Any other selector is 0x0E. Extra arguments compute the
		// value and then return 2.
		args, consumed, extra, status, err := h.propReadArgs(call, 2)
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if h.Props == nil {
			return Record{}, 0, 0, true, fmt.Errorf("props are unavailable")
		}
		if args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		prop, found := h.Props.Lookup(args[0].Text)
		if !found {
			return Record{}, 0, StatusNameNotFound, true, nil
		}
		if args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		var data uint32
		switch args[1].Int {
		case 1:
			data = uint32(int32(int16(prop.X)))
		case 2:
			data = uint32(int32(int16(prop.Y)))
		case 3:
			data = uint32(int32(int16(prop.Z)))
		case 4:
			data = uint32(uint16(int16(prop.X)))<<16 | uint32(uint16(int16(prop.Y)))
		default:
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if extra {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		return Record{Kind: 4, Data: data}, consumed, 0, true, nil
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
