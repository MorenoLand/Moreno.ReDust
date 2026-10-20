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
//	FUN_00406C10: the projected world walk (actors, then props); a hit gives an id,
//	              which hitIdentity turns into "actor" or "prop" and a name
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
// the handler and then returns status 2, the dispatcher's missing-')' status. No
// argument returns 0x0E with nothing written.
func (h *GameHost) hitTest(call *ScriptCall) (Record, int, uint16, error) {
	args, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if len(args) == 0 {
		// FUN_004153E0 passes FUN_004220A0 the first argument record, which is the ')'
		// here. FUN_00422870 has no case for that token and falls through to
		// FUN_00422040, which returns 0x0E for any token but a name. FUN_004153E0 returns
		// that status unchanged, the dispatcher passes a non-zero status through, and
		// nothing is written.
		return Record{}, 0, ScriptStatusWrongType, nil
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

	id, found, err := h.worldWalkHit(packed)
	if err != nil {
		return "", 0, err
	}
	if found {
		label, name, err := h.hitIdentity(id)
		if err != nil {
			return "", 0, err
		}
		h.setResultLabel(label)
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

// worldWalkHit is the world walk of FUN_00406C10, the first thing FUN_004153E0 does.
// Verified from the decompile:
//
//	FUN_0040DA40 then FUN_004216F0 append each entry FUN_0040DB70 or FUN_00421820 keeps,
//	  in table order, into one list. The prop walk copies each row, so the table is
//	  not written. FUN_004060C0 raises 0x13F8 when the hundredth entry is appended.
//	FUN_004061A0 sorts the list. For i from 0 to count-2, every later k with a larger
//	  key than the entry at i is swapped into i, so the list ends descending by key.
//	  The key is the signed short at entry +0x0C: word 20 on path A, the depth on path B.
//	FUN_00406C10 walks from the last entry down to the first and returns the first entry
//	  whose clipped box holds the point and whose walker answers true.
//
// So when two props are under the point, the one nearest the end of the sorted list wins:
// the lowest key, and among equal keys the later table row. One hit is order-independent.
//
// A negative key runs the unscaled walker FUN_004360C0, which is propFrameHit, with the
// flip propRecordFlip (0), as the walk's entry bytes 10 and 11 give. A non-negative key
// runs FUN_004396F0 and then FUN_004398A0 on the point moved by DAT_00459A54 and
// DAT_00459A56. FUN_004398A0 is not decoded, so a point inside such an entry is a gap.
//
// Still a gap:
//   - a projected actor. FUN_0040DB70 keeps it on the list, and its frame walk (FUN_0040DEC0)
//     and the scaled step above are not ported;
//   - two rows with one name. The port keeps one record per name, and the native keeps a row each.
//
// The walk returns the hit entry's id, the entry's +0x04 dword. For a prop that is the
// prop's stamp (+0x2E, FUN_00421820 copies it), and hitIdentity turns it into a name.
func (h *GameHost) worldWalkHit(packed uint32) (uint32, bool, error) {
	if len(h.actorKeys()) > 0 && h.Env.ActorHitAt == nil {
		return 0, false, fmt.Errorf("%w: hittest with actors in the table and no projected-actor hook", ErrHostOpcodeUnimplemented)
	}
	// The projected actor under the point is one entry of the walk, with its depth as its
	// key (FUN_0040DB70 stores the depth at entry +0x0C). The walk visits entries in
	// ascending key order, so the actor is decided when the walk reaches its key. A tie goes
	// to the actor, which the native order does not show.
	var actor *ActorRecord
	var actorKey int16
	if h.Env.ActorHitAt != nil {
		if name, depth, hit := h.Env.ActorHitAt(packed); hit {
			record, status := h.Actors.Lookup(name)
			if status != 0 {
				return 0, false, fmt.Errorf("%w: projected actor %q is not in the actor table", ErrHostOpcodeUnimplemented, name)
			}
			actor, actorKey = record, depth
		}
	}
	if h.Props == nil {
		if actor != nil {
			return actor.Stamp, true, nil
		}
		return 0, false, nil
	}
	names := h.Props.TableNames()
	if len(names) > 0 && h.Env.PropPoint == nil {
		return 0, false, fmt.Errorf("%w: hittest with props and no PropPoint hook", ErrHostOpcodeUnimplemented)
	}
	list, err := h.propWalkList(names)
	if err != nil {
		return 0, false, err
	}
	sortPropWalk(list)
	point := PointFromPacked(packed)
	for i := len(list) - 1; i >= 0; i-- {
		entry := list[i]
		if actor != nil && actorKey <= entry.key {
			return actor.Stamp, true, nil
		}
		if !ContainsPoint(point, entry.rect) {
			continue
		}
		if entry.key >= 0 {
			return 0, false, fmt.Errorf("%w: hittest on prop %q with a non-negative key (scaled walker FUN_004396F0 and FUN_004398A0 not verified)", ErrHostOpcodeUnimplemented, entry.name)
		}
		hit, err := propFrameHit(entry.frame, entry.anchor, propRecordFlip, packed)
		if err != nil {
			return 0, false, err
		}
		if !hit {
			continue
		}
		return entry.stamp, true, nil
	}
	if actor != nil {
		return actor.Stamp, true, nil
	}
	return 0, false, nil
}

// pointInActor is pointinactor 20039, FUN_0040B8D0. The name is a string and the actor
// must be in the table (unknown is 0x0A); the point must be numeric (0x0E). The result is a
// type-2 boolean that is true when the actor's projected sprite holds the point. The
// native keeps the actor (FUN_0040DB70), tests the box (FUN_0042E340) and runs the frame
// walker. The port answers from the sprite mask drawn this frame, the test the click path uses.
func (h *GameHost) pointInActor(call *ScriptCall) (Record, int, uint16, error) {
	actor, args, consumed, status, err := h.actorArgs(call, 2)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if args[1].Kind != 4 {
		return Record{}, 0, ScriptStatusWrongType, nil
	}
	if h.Env.ActorHitIn == nil {
		return Record{}, 0, 0, fmt.Errorf("%w: pointinactor has no projected-actor hook", ErrHostOpcodeUnimplemented)
	}
	return Record{Kind: 2, Data: boolWord(h.Env.ActorHitIn(actor.Name, uint32(args[1].Int)))}, consumed, 0, nil
}

// hitIdentity is the id lookup FUN_004153E0 runs after the walk, in the native order.
// FUN_0040E0C0 comes first: it scans the whole actor table for the first row whose stamp
// (+0x30) is id, and a match writes "actor" and returns that actor's name (+0x54). Only
// when no actor has the id does FUN_00421D70 scan the prop table for the first row whose
// stamp (+0x2E) is id, writing "prop" and returning that row's name (+0x4E). So a prop hit
// whose id equals an actor's stamp is reported as the actor, whatever the actor's
// visibility or placement, because FUN_0040E0C0 tests the stamp alone.
//
// The stamps come from one counter and never change (stamp.go), so an actor takes a
// prop's id only when two stamps collide. The port's boot path mints distinct values (checked
// on the shipped casts and prop archives). A collision after a load, when restored stamps
// meet the new counter, is not traced. The probes set the stamps directly to exercise both
// outcomes.
//
// Native 0x1194 is a fatal diagnostic when neither table has the id. The walk only
// returns ids the prop table holds, so that case is reported as a gap rather than a guess.
func (h *GameHost) hitIdentity(id uint32) (string, string, error) {
	if actor, found := h.Actors.ByStamp(id); found {
		return HitLabelActor, actor.Name, nil
	}
	if name, found := h.Props.ByStamp(id); found {
		return HitLabelProp, h.propWalkName(name), nil
	}
	return "", "", fmt.Errorf("%w: hittest id %d names neither an actor nor a prop (FUN_004153E0 raises 0x1194)", ErrHostOpcodeUnimplemented, id)
}

// propWalkEntry is one prop entry of the walk list, the fields FUN_00406C10 reads from
// the 36-byte entry: the sort key, the clipped box, the walker's inputs, and the id
// (+0x04), which is the prop's stamp.
type propWalkEntry struct {
	name   string
	key    int16
	rect   Rect
	frame  []byte
	anchor uint32
	stamp  uint32
}

// propWalkCapacity is the most entries the list holds. FUN_004060C0 raises 0x13F8 when the
// count reaches it.
const propWalkCapacity = 100

// propWalkList builds the prop part of the walk list in table order. A row that
// FUN_00421820 does not keep is left out. A name twice in the table is a gap, because the
// port's one record per name cannot stand for two native rows.
func (h *GameHost) propWalkList(names []string) ([]propWalkEntry, error) {
	seen := map[string]bool{}
	var list []propWalkEntry
	for _, name := range names {
		folded := strings.ToLower(name)
		if seen[folded] {
			return nil, fmt.Errorf("%w: hittest with prop %q twice in the prop table (two native rows are not modelled)", ErrHostOpcodeUnimplemented, name)
		}
		seen[folded] = true
		record, found, err := h.Env.PropPoint(name)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: hittest prop %q is in the table but the prop hook does not resolve it", ErrHostOpcodeUnimplemented, name)
		}
		entry, kept, err := h.propWalkEntryFor(name, record)
		if err != nil {
			return nil, err
		}
		if !kept {
			continue
		}
		list = append(list, entry)
		if len(list) >= propWalkCapacity {
			return nil, fmt.Errorf("%w: hittest with %d props on the walk list (FUN_004060C0 raises 0x13F8)", ErrHostOpcodeUnimplemented, propWalkCapacity)
		}
	}
	return list, nil
}

// propWalkEntryFor is FUN_00421820 for one row, as the walk list keeps it. Path A
// (word 9 zero) keeps a visible row with a selected frame whose clipped box is not empty,
// with its word 20 as the key. Path B keeps what projectedPropCull keeps, with the depth
// as the key.
func (h *GameHost) propWalkEntryFor(name string, prop PropPointRecord) (propWalkEntry, bool, error) {
	// FUN_00421820 copies the row's +0x2E into the entry's +0x04 on both paths, so the
	// id is the row's stamp.
	stamp, _ := h.Props.StampOf(name)
	if !prop.Visible {
		return propWalkEntry{}, false, nil
	}
	if !prop.Projected {
		if prop.Frame == nil {
			return propWalkEntry{}, false, nil
		}
		clipped, inside := pathAClip(prop)
		if !inside {
			return propWalkEntry{}, false, nil
		}
		return propWalkEntry{name: name, key: prop.Walker, rect: clipped, frame: prop.Frame.Data, anchor: prop.Anchor, stamp: stamp}, true, nil
	}
	cull, kept, err := h.projectedPropCull(prop)
	if err != nil || !kept {
		return propWalkEntry{}, false, err
	}
	return propWalkEntry{name: name, key: cull.depth, rect: cull.rect, frame: cull.frame.Data, anchor: cull.anchor, stamp: stamp}, true, nil
}

// sortPropWalk is FUN_004061A0 on the walk list. The loop is the native's, with the same
// bounds: the compare reads the entry it may swap (+0x0C), and a swap is only made on a
// strictly smaller key, so equal keys keep their order except where an earlier swap moved them.
func sortPropWalk(list []propWalkEntry) {
	for i := 0; i < len(list)-1; i++ {
		for k := i + 1; k < len(list); k++ {
			if list[i].key < list[k].key {
				list[i], list[k] = list[k], list[i]
			}
		}
	}
}

// propWalkName is the name FUN_00421D70 returns for a prop row: the record's own name, when
// it has one. hitIdentity passes it the row that holds the id.
func (h *GameHost) propWalkName(name string) string {
	if prop, ok := h.Props.Lookup(name); ok && prop.Name != "" {
		return prop.Name
	}
	return name
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
