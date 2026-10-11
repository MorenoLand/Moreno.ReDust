package scripts

import (
	"fmt"
	"strings"
)

// The result slot DAT_00459720 is read by one native, the result value, and
// written by several. Verified from the cached exports:
//
//	FUN_004153E0  hittest 20070 (value)    writes a label, then returns a name
//	FUN_00425CD0  result 16010 (statement) copies a string register into the slot
//	FUN_00415C20  result 16010 (value)     *status = 0; returns a copy of the slot
//	FUN_00418930  script-entry clear       copies the zero-length string
//	FUN_0040D090  indextoactor 20013       writes the owning cast's name
//	FUN_00421210  indextoprop 20032        writes the owning shop's name
//
// The port writes the slot from hittest, the result statement, indextoactor and
// indextoprop. ClearResultBuffer (scriptentry.go) is the script-entry clear, which
// no runtime path calls yet.
//
// The slot holds a Pascal string: a length byte then the text. FUN_0042E6C0
// copies the length byte and that many bytes, and FUN_00421F60 stores the copy
// in a string register. A zero value of ScriptContextState is an empty slot.

// Labels hittest writes into the slot. Each is the Pascal string at the address
// named in the comment; the text sits one byte past the address Ghidra names,
// as FUN_00427FC0 reads them (scripts/stringpool.go holds the same addresses).
const (
	// HitLabelActor is DAT_0045D0EC, text at 0x0045D0ED.
	HitLabelActor = "actor"
	// HitLabelProp is DAT_0045D440, text at 0x0045D441.
	HitLabelProp = "prop"
	// HitLabelScene is DAT_0045D438, text at 0x0045D439.
	HitLabelScene = "scene"
	// HitLabelFlat is DAT_0045D430, text at 0x0045D431.
	HitLabelFlat = "flat"
	// HitLabelButton is DAT_0045D4E4, text at 0x0045D4E5.
	HitLabelButton = "button"
	// HitLabelNone is DAT_0045D368, text at 0x0045D369.
	HitLabelNone = "none"
)

// hitTest is FUN_004153E0, hittest 20070 as a value. Verified from the decompile,
// in the order the native runs it:
//
//	FUN_004220A0: one expression; its status passes through
//	not numeric                        -> 0x0E, nothing written
//	FUN_00406C10: the projected world walk (actors, then props); a hit writes
//	              "actor" or "prop" and returns the name
//	set open and visible (DAT_00459A24, DAT_00459A26) and the point inside the
//	  pointer rectangle: writes "scene" and returns the view row at the camera's
//	  cell (FUN_0041BFB0); no such row -> 0x0A after the label is written
//	stage open and visible (DAT_00459A00, DAT_00459A02) and the point inside the
//	  stage view: the current flat's button rows, last row first (FUN_00413330).
//	  A hit writes "button" and returns the button name; no hit writes "flat" and
//	  returns the flat's name
//	otherwise: writes "none" and returns the empty string
//
// The value is a string in every branch that returns one. A surplus argument runs
// the handler and then returns status 2, the dispatcher's missing-')' status.
func (h *GameHost) hitTest(call *ScriptCall) (Record, int, uint16, error) {
	args, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if len(args) == 0 {
		return Record{}, 0, 0, fmt.Errorf("%w: hittest with no argument (FUN_004220A0 on an empty expression is not traced)", ErrHostOpcodeUnimplemented)
	}
	if args[0].Kind != 4 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	name, status, err := h.hitTestPoint(uint32(args[0].Int))
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if len(args) > 1 {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: name})
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	return record, consumed, 0, nil
}

// hitTestPoint runs the branches of FUN_004153E0 on a packed point. It returns the
// name the value carries and writes the label into the result slot on every path
// the native writes it. A gap is returned as ErrHostOpcodeUnimplemented, never as
// a guessed answer.
func (h *GameHost) hitTestPoint(packed uint32) (string, uint16, error) {
	point := PointFromPacked(packed)

	name, found, err := h.worldWalkHit(packed)
	if err != nil {
		return "", 0, err
	}
	if found {
		h.setResultLabel(HitLabelProp)
		return name, 0, nil
	}

	rect, open := h.setRect()
	if PointInSetWillTest(SetState{Active: open, Visible: h.setVisible}) && ContainsPoint(point, rect) {
		return h.hitSetScene()
	}

	if h.stageIsOpen() && h.StageVisible != 0 && ContainsPoint(point, StageViewRect) {
		return h.hitStageFlat(point)
	}

	h.setResultLabel(HitLabelNone)
	return "", 0, nil
}

// worldWalkHit is the world walk of FUN_00406C10, the first thing FUN_004153E0
// does. The native rebuilds a draw list from the actor table (FUN_0040DA40) and
// the prop table (FUN_004216F0), each entry passing FUN_0040DB70 or FUN_00421820,
// then tests the point against each entry's clipped box and frame. A projected
// prop with no further walk is decided here through propPointHit, which is the
// same projection and walker pointinprop uses.
//
// Two parts are not decided here and return a gap instead:
//   - an actor that survives the cull (ProjectedDepth) is in the native list,
//     and its frame walk (FUN_0040DEC0 with FUN_004360C0 or FUN_004396F0) is not
//     ported, so any projected actor is a gap;
//   - two or more props under the point are a gap, because the native order of
//     the list is not verified: FUN_004061A0 sorts it, and its compare reads a
//     different entry from the one it swaps, as the decompile reads. One hit is
//     order-independent.
//
// A prop hit also looks its id up in the actor table (FUN_0040E0C0) before the
// prop table, so any actor in the table makes a prop hit a gap as well.
func (h *GameHost) worldWalkHit(packed uint32) (string, bool, error) {
	actorKeys := h.actorKeys()
	if len(actorKeys) > 0 && h.Env.ProjectedDepth == nil {
		return "", false, fmt.Errorf("%w: hittest with actors in the table and no projection hook (FUN_0040DB70)", ErrHostOpcodeUnimplemented)
	}
	for _, key := range actorKeys {
		if _, projected := h.Env.ProjectedDepth(h.Actors.actors[key]); projected {
			return "", false, fmt.Errorf("%w: hittest with a projected actor (FUN_0040DEC0 frame walk not ported)", ErrHostOpcodeUnimplemented)
		}
	}

	if h.Props == nil {
		return "", false, nil
	}
	keys := h.Props.Names()
	if len(keys) > 0 && h.Env.PropPoint == nil {
		return "", false, fmt.Errorf("%w: hittest with props and no PropPoint hook", ErrHostOpcodeUnimplemented)
	}
	var hits []string
	for _, key := range keys {
		record, found, err := h.Env.PropPoint(key)
		if err != nil {
			return "", false, err
		}
		if !found {
			continue
		}
		hit, err := h.propPointHit(record, packed)
		if err != nil {
			return "", false, err
		}
		if hit {
			hits = append(hits, key)
		}
	}
	switch {
	case len(hits) == 0:
		return "", false, nil
	case len(hits) > 1:
		return "", false, fmt.Errorf("%w: hittest with %d props under the point (native list order not verified)", ErrHostOpcodeUnimplemented, len(hits))
	case len(actorKeys) > 0:
		return "", false, fmt.Errorf("%w: hittest prop hit with actors in the table (FUN_0040E0C0 id lookup not ported)", ErrHostOpcodeUnimplemented)
	}
	prop, _ := h.Props.Lookup(hits[0])
	name := hits[0]
	if prop != nil && prop.Name != "" {
		name = prop.Name
	}
	return name, true, nil
}

// actorKeys is the actor table's keys in its own order, or nil with no table.
func (h *GameHost) actorKeys() []string {
	if h.Actors == nil {
		return nil
	}
	return h.Actors.Names()
}

// hitSetScene is the set branch of FUN_004153E0. FUN_0041BFB0 takes the view row
// by (SceneID, DirectionID) and copies its name at row +0x0C. The native arguments
// are DAT_00459A74 then DAT_00459A72, which FUN_00406570 sets to the camera's
// y and x words shifted right by 8 on every pump. The port's camera hook is the
// camera those words hold (cameraxyz reads it the same way), and that equivalence
// is taken from the port, not re-verified here.
func (h *GameHost) hitSetScene() (string, uint16, error) {
	if h.Env.Camera == nil || h.Env.SceneAtCell == nil {
		return "", 0, fmt.Errorf("%w: hittest in an open set needs the camera and the view rows", ErrHostOpcodeUnimplemented)
	}
	camera := h.Env.Camera()
	row := camera[1] >> 8
	col := camera[0] >> 8
	h.setResultLabel(HitLabelScene)
	name, found := h.Env.SceneAtCell(row, col)
	if !found {
		return "", StatusNameNotFound, nil
	}
	return name, 0, nil
}

// hitStageFlat is the stage branch of FUN_004153E0 for a point inside the stage.
// FUN_00413330 walks the current flat's button block from its last row to its
// first and returns the first row whose rectangle holds the point, which the
// port's FlatButtons list gives in file order, so it is walked backwards here.
// The flat is the current flat by name; the native uses the flat index, which
// the name equals only while flat names are unique (not verified otherwise).
func (h *GameHost) hitStageFlat(point Point) (string, uint16, error) {
	if h.Env.CurrentFlat == nil || h.Env.FlatButtons == nil {
		return "", 0, fmt.Errorf("%w: hittest in the stage needs CurrentFlat and FlatButtons", ErrHostOpcodeUnimplemented)
	}
	flat := h.Env.CurrentFlat()
	buttons, found, err := h.Env.FlatButtons(flat)
	if err != nil {
		return "", 0, err
	}
	if !found {
		return "", 0, fmt.Errorf("%w: hittest with a current flat %q the stage does not list", ErrHostOpcodeUnimplemented, flat)
	}
	for i := len(buttons) - 1; i >= 0; i-- {
		button := buttons[i]
		rect := Rect{Top: button.Rect[0], Left: button.Rect[1], Bottom: button.Rect[2], Right: button.Rect[3]}
		if ContainsPoint(point, rect) {
			h.setResultLabel(HitLabelButton)
			return button.Name, 0, nil
		}
	}
	h.setResultLabel(HitLabelFlat)
	return flat, 0, nil
}

// setResultLabel writes a label into the result slot as its Pascal string.
func (h *GameHost) setResultLabel(label string) {
	h.context.ResultBuffer = PascalFromCString(label)
	h.resultStale = false
}

// setResultText writes a name into the result slot as a Pascal string, the form
// FUN_0042E6C0 copies: the length byte, then the text.
func (h *GameHost) setResultText(name string) {
	h.context.ResultBuffer = PascalFromCString(name)
	h.resultStale = false
}

// actorCastResult is the slot write of FUN_0040D090 on its success path. FUN_0040D940
// finds the cast record whose dword 0 equals the actor's dword +2 (FUN_0040C1F0 stores
// *param_1 there, which castwarm matches against the cast record's dword 0), and the
// cast's Pascal name at record +0x0C is copied into the slot. The port finds the cast by its native name, ActorRecord.Cast, in the
// cast table. A miss is status 0x0A with the slot untouched. Without the cast hooks the
// slot's content is unknown, so resultStale is set.
func (h *GameHost) actorCastResult(actor *ActorRecord) uint16 {
	if h.Env.CastCount == nil || h.Env.CastName == nil {
		h.resultStale = true
		return 0
	}
	for index := 0; index < h.Env.CastCount(); index++ {
		if name, ok := h.Env.CastName(index); ok && strings.EqualFold(name, actor.Cast) {
			h.setResultText(name)
			return 0
		}
	}
	return 0x0a
}

// propShopResult is the slot write of FUN_00421210 on its success path. FUN_004215F0
// finds the shop record whose dword 0 equals the prop's dword +2, the owner shop handle
// FUN_00420370 writes, and the shop's Pascal name at record +0x0C is copied into the
// slot. The port takes the owner from its prop hook, the same owner the sendtoprop chain
// resolves, and matches it against the open shops. A miss is status 0x0A with the slot
// untouched. Without the prop hook, or when the owner is not one the port can name, the
// slot's content is unknown, so resultStale is set.
func (h *GameHost) propShopResult(prop string) uint16 {
	if h.Env.PropScript == nil {
		h.resultStale = true
		return 0
	}
	_, _, shop, found := h.Env.PropScript(prop)
	if !found {
		h.resultStale = true
		return 0
	}
	for _, open := range h.Shops {
		if strings.EqualFold(open, shop) {
			h.setResultText(open)
			return 0
		}
	}
	return 0x0a
}

// resultValue is FUN_00415C20, result 16010 as a value: *status = 0 and a copy of
// the slot. Any argument makes the dispatcher's ')' check fail with status 2, so
// the handler's own result is only returned for the empty call.
func (h *GameHost) resultValue(call *ScriptCall) (Record, int, uint16, error) {
	args, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if len(args) > 0 {
		return Record{}, 0, ScriptStatusMalformed, nil
	}
	if h.resultStale {
		return Record{}, 0, 0, fmt.Errorf("%w: result after indextoactor or indextoprop whose slot content is unknown (a missing cast or prop hook, or a prop with no known owner)", ErrHostOpcodeUnimplemented)
	}
	record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: PascalTextOf(h.context.ResultBuffer)})
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	return record, consumed, 0, nil
}

// resultStatement is FUN_00425CD0, result 16010 as a statement. The expression's
// status passes through. A string is copied into the slot (FUN_00421FC0); any
// other type is 0x0E with the slot untouched. FUN_00421FC0 also frees the string
// register it read; that release is the expression evaluator's job here and is
// not separately traced.
func (h *GameHost) resultStatement(call *ScriptCall) (int, uint16, error) {
	args, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return 0, status, err
	}
	if len(args) == 0 {
		return 0, 0, fmt.Errorf("%w: result with no argument (FUN_004220A0 on an empty expression is not traced)", ErrHostOpcodeUnimplemented)
	}
	if args[0].Kind != 3 {
		return 0, ScriptStatusWrongType, nil
	}
	if len(args[0].Text) > PascalMaxLength {
		return 0, 0, fmt.Errorf("%w: result of %d bytes is longer than the 255-byte Pascal slot (FUN_0042E6C0 overflow not traced)", ErrHostOpcodeUnimplemented, len(args[0].Text))
	}
	h.context.ResultBuffer = append([]byte{byte(len(args[0].Text))}, args[0].Text...)
	h.resultStale = false
	if len(args) > 1 {
		return 0, ScriptStatusMalformed, nil
	}
	return consumed, 0, nil
}
