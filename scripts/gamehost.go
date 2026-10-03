package scripts

import (
	"errors"
	"fmt"
	"strings"
)

// ErrHostOpcodeUnimplemented reports an opcode the game host has not been
// given native semantics for yet. It is an explicit evidence gap: the script
// call fails instead of guessing.
var ErrHostOpcodeUnimplemented = errors.New("script opcode is not implemented by the game host")

// GameHostEnv is what the host needs from the running game.
type GameHostEnv struct {
	// CurrentSet is DAT_00459A82, the open set's name; "" when none is open.
	CurrentSet func() string
	// ResolveStar is FUN_0041B850: a named location in a set.
	ResolveStar func(set, star string) ([3]int16, bool)
	// Player and Camera are DAT_00459A6C and DAT_00459A78.
	Player func() [3]int16
	Camera func() [3]int16
	// ProjectedDepth is FUN_0040DB70 for one actor: its depth and whether it
	// survives the cull.
	ProjectedDepth func(actor *ActorRecord) (int16, bool)
	// Frame is the frame() counter and FrameRate the boot framerate.
	Frame     func() int32
	FrameRate func() int32
	OptionKey func() bool
	ShiftKey  func() bool
	// Heading is FUN_004113F0, the bearing from one point to another, in the
	// convention the renderer draws headings with.
	Heading func(from, to [3]int16) int16
	// Chain returns an actor's [actor script, cast script] frames
	// (FUN_0040CB60).
	Chain func(actor *ActorRecord) ([]ScriptFrame, error)
	// Fallback handles opcodes this host does not; nil makes them gaps.
	Fallback ScriptHost
	// Log receives diagnostics when non-nil.
	Log func(format string, args ...any)
}

// GameHost executes script commands against the actor table, the loop
// scheduler and the running game.
type GameHost struct {
	Env    GameHostEnv
	Actors *ScriptActors
	Loops  *LoopScheduler
	Random *NativeRandom
}

func (h *GameHost) logf(format string, args ...any) {
	if h.Env.Log != nil {
		h.Env.Log(format, args...)
	}
}

func (h *GameHost) currentSet() string {
	if h.Env.CurrentSet == nil {
		return ""
	}
	return h.Env.CurrentSet()
}

func opcodeName(call *ScriptCall) string {
	return CommandHandlerName(call.Program.Records[call.Start].Kind)
}

func (h *GameHost) gap(call *ScriptCall) error {
	return fmt.Errorf("%w: %s (%d)", ErrHostOpcodeUnimplemented, opcodeName(call), call.Program.Records[call.Start].Kind)
}

// actorArgs evaluates the arguments and resolves the first as an actor,
// in the order the native handlers do: evaluate, convert the name, resolve.
func (h *GameHost) actorArgs(call *ScriptCall, count int) (*ActorRecord, []ScriptValue, int, uint16, error) {
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
	actor, status := h.Actors.Lookup(args[0].Text)
	if status != 0 {
		return nil, nil, 0, status, nil
	}
	return actor, args, consumed, 0, nil
}

// placeAtStar re-resolves the actor's star when its set is the open one, as
// actorset/actorstar do through FUN_0041B850.
func (h *GameHost) placeAtStar(actor *ActorRecord) {
	if h.Env.ResolveStar == nil || !strings.EqualFold(actor.Set, h.currentSet()) {
		return
	}
	if position, ok := h.Env.ResolveStar(actor.Set, actor.Star); ok {
		actor.Position = position
	}
}

// stopJobs is FUN_0040F6A0: "all" cancels every job.
func (h *GameHost) stopJobs(name string) {
	if strings.EqualFold(name, "all") {
		for _, key := range h.Actors.Names() {
			h.Actors.actors[key].StopJob()
		}
		return
	}
	if actor, status := h.Actors.Lookup(name); status == 0 {
		actor.StopJob()
	}
}

// Command implements ScriptHost.
func (h *GameHost) Command(call *ScriptCall) (int, uint16, error) {
	name := opcodeName(call)
	switch name {
	case "actorvisible":
		actor, args, consumed, status, err := h.actorArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, err
		}
		if args[1].Kind != 2 {
			return 0, ScriptStatusWrongType, nil
		}
		actor.Visible = args[1].Int != 0
		return consumed, 0, nil
	case "actorset", "actorstar", "actorpose", "actorowner":
		actor, args, consumed, status, err := h.actorArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, err
		}
		if args[1].Kind != 3 {
			return 0, ScriptStatusWrongType, nil
		}
		if status := nativeName(args[1].Text); status != 0 {
			return 0, status, nil
		}
		switch name {
		case "actorset":
			h.stopJobs(actor.Name)
			actor.Set = args[1].Text
			h.placeAtStar(actor)
			actor.Placed = true
		case "actorstar":
			h.stopJobs(actor.Name)
			actor.Star = args[1].Text
			h.placeAtStar(actor)
			actor.Placed = true
		case "actorpose":
			if !actor.HasPose(args[1].Text) {
				return 0, 0x0a, nil
			}
			actor.Pose, actor.Frame = args[1].Text, 0
		case "actorowner":
			actor.Owner = args[1].Text
		}
		return consumed, 0, nil
	case "actordeg", "actorspeed", "actorscale", "actorturn", "actorzclip", "actorvalue":
		actor, args, consumed, status, err := h.actorArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, err
		}
		if args[1].Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
		value := args[1].Int
		switch name {
		case "actordeg":
			actor.Heading = int16(value & 0xff)
		case "actorspeed":
			speed := int16(value)
			if speed < 1 {
				speed = 1
			}
			if speed > 0x100 {
				speed = 0x100
			}
			actor.Speed = speed
		case "actorscale":
			if value < 0 {
				value = 0
			}
			actor.Scale = value
		case "actorturn":
			if value < 1 {
				value = 1
			}
			actor.TurnRate = int16(value)
		case "actorzclip":
			actor.ZClip = int16(value)
		case "actorvalue":
			actor.Value = value
		}
		return consumed, 0, nil
	case "actorhitbox":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 3 || args[0].Kind != 3 {
			return 0, ScriptStatusMalformed, nil
		}
		if args[1].Kind != 4 || args[2].Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
		if strings.EqualFold(args[0].Text, "player") {
			// The player's box lives in DAT_00459A28/2A; the port does not
			// consume it yet.
			return consumed, 0, nil
		}
		actor, status := h.Actors.Lookup(args[0].Text)
		if status != 0 {
			return 0, status, nil
		}
		actor.HitboxWidth = int16(args[1].Int)
		return consumed, 0, nil
	case "actorxyz":
		actor, args, consumed, status, err := h.actorArgs(call, 4)
		if err != nil || status != 0 {
			return 0, status, err
		}
		for _, arg := range args[1:] {
			if arg.Kind != 4 {
				return 0, ScriptStatusWrongType, nil
			}
		}
		h.stopJobs(actor.Name)
		actor.Position = [3]int16{int16(args[1].Int), int16(args[2].Int), int16(args[3].Int)}
		actor.Placed = true
		return consumed, 0, nil
	case "makeloop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 4 || args[0].Kind != 3 || args[1].Kind != 3 || args[2].Kind != 3 {
			return 0, ScriptStatusMalformed, nil
		}
		kind, ok := LoopKindByName(args[0].Text)
		if !ok {
			return 0, 0x0a, nil
		}
		if nativeName(args[1].Text) != 0 || nativeName(args[2].Text) != 0 {
			return 0, 0x1a, nil
		}
		if args[3].Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
		// FUN_0040F5B0 first stops the owner's loop of that kind (FUN_0040F740),
		// so makeloop replaces rather than stacks.
		h.Loops.Stop(kind, args[1].Text)
		return consumed, h.Loops.Register(ScriptLoop{Kind: kind, Owner: strings.ToLower(args[1].Text), Callback: args[2].Text, Remaining: args[3].Int}), nil
	case "stoploop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 3 {
			return 0, ScriptStatusMalformed, nil
		}
		kind, ok := LoopKindByName(args[0].Text)
		if !ok {
			return 0, 0x0a, nil
		}
		h.Loops.Stop(kind, args[1].Text)
		return consumed, 0, nil
	case "stopwalk":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusMalformed, nil
		}
		h.stopJobs(args[0].Text)
		return consumed, 0, nil
	case "turntodeg":
		actor, args, consumed, status, err := h.actorArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, err
		}
		if args[1].Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
		actor.Job = &ActorJob{Mode: ActorJobTurn, Heading: int16(args[1].Int & 0xff)}
		return consumed, 0, nil
	case "walktostar":
		actor, args, consumed, status, err := h.actorArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, err
		}
		if args[1].Kind != 3 {
			return 0, ScriptStatusWrongType, nil
		}
		if status := nativeName(args[1].Text); status != 0 {
			return 0, status, nil
		}
		status, err = h.walkToStar(actor, args[1].Text)
		if err != nil || status != 0 {
			return 0, status, err
		}
		return consumed, 0, nil
	case "sendtoactor":
		return h.sendToActor(call)
	}
	if h.Env.Fallback != nil {
		return h.Env.Fallback.Command(call)
	}
	return 0, 0, h.gap(call)
}

// walkToStar is FUN_0040FF40. An actor outside the open set gets a
// walknodata job that finishes once its set opens; otherwise the star is
// resolved and a straight walk is queued from the current position.
func (h *GameHost) walkToStar(actor *ActorRecord, star string) (uint16, error) {
	if !strings.EqualFold(actor.Set, h.currentSet()) {
		actor.Job = &ActorJob{Mode: ActorJobNoData, Target: star, Heading: actor.Heading}
		return 0, nil
	}
	if h.Env.ResolveStar == nil {
		return 0, fmt.Errorf("walktostar has no star resolver")
	}
	destination, ok := h.Env.ResolveStar(actor.Set, star)
	if !ok {
		return 0x0a, nil
	}
	if h.Env.Heading == nil {
		return 0, fmt.Errorf("walktostar has no bearing function")
	}
	heading := h.Env.Heading(actor.Position, destination)
	walk := NewNativeActorWalkJob(actor.Position, destination, heading, actor.Speed)
	actor.Job = &ActorJob{Mode: ActorJobWalk, Target: star, Heading: heading, Walk: &walk}
	return 0, nil
}

// sendToActor is FUN_0040CB60: `sendtoactor(name, message(args))` runs the
// message against [actor script, cast script] with the caller's frame and
// locals evaluating the arguments.
func (h *GameHost) sendToActor(call *ScriptCall) (int, uint16, error) {
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
	actor, status := h.Actors.Lookup(value.Text)
	if status != 0 {
		return 0, status, nil
	}
	if h.Env.Chain == nil {
		return 0, 0, fmt.Errorf("sendtoactor has no chain provider")
	}
	chain, err := h.Env.Chain(actor)
	if err != nil {
		return 0, 0, err
	}
	used, status, err := call.Interpreter.Call(chain, call.Chain, call.FrameIndex, call.Locals, call.Program, message)
	if err != nil || status != 0 {
		return 0, status, err
	}
	if call.Kind(message+used) != opClose {
		return 0, ScriptStatusMalformed, nil
	}
	return message + used + 1 - call.Start, 0, nil
}

func packPoint(position [3]int16) int32 {
	return int32(uint32(uint16(position[0]))<<16 | uint32(uint16(position[1])))
}

func unpackPoint(value int32) [3]int16 {
	return [3]int16{int16(uint32(value) >> 16), int16(uint32(value))}
}

// pointComponent is the selector shared by actorxyz, cameraxyz and
// playerxyz: 1 x, 2 y, 3 z, 4 the packed x:y pair; anything else is 0x0E.
func pointComponent(position [3]int16, selector int32) (int32, bool) {
	switch selector {
	case 1:
		return int32(position[0]), true
	case 2:
		return int32(position[1]), true
	case 3:
		return int32(position[2]), true
	case 4:
		return packPoint(position), true
	}
	return 0, false
}

// Value implements ScriptHost.
func (h *GameHost) Value(call *ScriptCall) (Record, int, uint16, error) {
	name := opcodeName(call)
	number := func(value int32, consumed int) (Record, int, uint16, error) {
		return Record{Kind: 4, Data: uint32(value)}, consumed, 0, nil
	}
	boolean := func(value bool, consumed int) (Record, int, uint16, error) {
		return Record{Kind: 2, Data: boolWord(value)}, consumed, 0, nil
	}
	text := func(value string, consumed int) (Record, int, uint16, error) {
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: value})
		return record, consumed, status, err
	}
	switch name {
	case "actorset", "actorstar", "actorpose", "actorowner", "actordeg", "actorvisible", "actorvalue", "actorscale", "actorspeed", "actordist":
		actor, _, consumed, status, err := h.actorArgs(call, 1)
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		switch name {
		case "actorset":
			return text(actor.Set, consumed)
		case "actorstar":
			return text(actor.Star, consumed)
		case "actorpose":
			return text(actor.Pose, consumed)
		case "actorowner":
			return text(actor.Owner, consumed)
		case "actordeg":
			return number(int32(actor.Heading), consumed)
		case "actorvisible":
			return boolean(actor.Visible, consumed)
		case "actorvalue":
			return number(actor.Value, consumed)
		case "actorscale":
			return number(actor.Scale, consumed)
		case "actorspeed":
			return number(int32(actor.Speed), consumed)
		case "actordist":
			// FUN_0040B070: a placed actor is projected now and is 32000 when
			// culled; otherwise the depth cached by the last projection.
			if !actor.Placed {
				return number(int32(actor.Depth), consumed)
			}
			if h.Env.ProjectedDepth != nil {
				if depth, ok := h.Env.ProjectedDepth(actor); ok {
					return number(int32(depth), consumed)
				}
			}
			return number(32000, consumed)
		}
	case "iswalk":
		// FUN_004162B0 -> FUN_0040F840: true while any walk or turn slot
		// carries the name; the name is not resolved, so an unknown one is
		// simply false.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		actor, missing := h.Actors.Lookup(args[0].Text)
		return boolean(missing == 0 && actor.Job != nil, consumed)
	case "actorxyz":
		actor, args, consumed, status, err := h.actorArgs(call, 2)
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		value, ok := pointComponent(actor.Position, args[1].Int)
		if !ok {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		return number(value, consumed)
	case "cameraxyz", "playerxyz":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if h.currentSet() == "" {
			return Record{}, 0, 0x28, nil
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		source := h.Env.Player
		if name == "cameraxyz" {
			source = h.Env.Camera
		}
		if source == nil {
			return Record{}, 0, 0, fmt.Errorf("%s has no position source", name)
		}
		value, ok := pointComponent(source(), args[0].Int)
		if !ok {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		return number(value, consumed)
	case "calcdeg", "calcdist":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 4 || args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		from, to := unpackPoint(args[0].Int), unpackPoint(args[1].Int)
		if name == "calcdeg" {
			// FUN_004151E0 hands both unpacked points to FUN_004113F0.
			if h.Env.Heading == nil {
				return Record{}, 0, 0, fmt.Errorf("calcdeg has no bearing function")
			}
			return number(int32(h.Env.Heading(from, to)), consumed)
		}
		// FUN_00415290: integer square root of dx^2+dy^2, low 16 bits.
		dx, dy := int64(to[0])-int64(from[0]), int64(to[1])-int64(from[1])
		return number(int32(NativeIntegerSqrt(uint64(dx*dx+dy*dy))&0xffff), consumed)
	case "random":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		if h.Random == nil {
			return Record{}, 0, 0, fmt.Errorf("random has no generator")
		}
		return number(int32(h.Random.Inclusive(uint32(args[0].Int))), consumed)
	case "currentset":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		return text(CurrentSetName(h.currentSet() != "", h.currentSet()), consumed)
	case "optionkey", "shiftkey":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		key := h.Env.OptionKey
		if name == "shiftkey" {
			key = h.Env.ShiftKey
		}
		return boolean(key != nil && key(), consumed)
	case "frame", "framerate":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		source := h.Env.Frame
		if name == "framerate" {
			source = h.Env.FrameRate
		}
		if source == nil {
			return Record{}, 0, 0, fmt.Errorf("%s has no source", name)
		}
		return number(source(), consumed)
	}
	if h.Env.Fallback != nil {
		return h.Env.Fallback.Value(call)
	}
	return Record{}, 0, 0, h.gap(call)
}

// ActorEvent is a completed job the caller must deliver to the actor.
type ActorEvent struct {
	Actor   string
	Message string
}

// Pass advances every actor job once, as the native walk pass FUN_00410290
// does, and returns the endwalk/endturn events to deliver. moved reports
// whether any actor in the open set changed position or heading.
func (h *GameHost) Pass() (events []ActorEvent, moved bool) {
	current := h.currentSet()
	for _, key := range h.Actors.Names() {
		actor := h.Actors.actors[key]
		job := actor.Job
		if job == nil || job.Paused {
			continue
		}
		inSet := strings.EqualFold(actor.Set, current)
		switch job.Mode {
		case ActorJobTurn:
			if !inSet {
				actor.Heading = job.Heading
			} else {
				actor.Heading = NativeTurnStep(actor.Heading, job.Heading, actor.TurnRate)
				moved = true
			}
			if actor.Heading == job.Heading {
				actor.Job = nil
				events = append(events, ActorEvent{Actor: actor.Name, Message: "endturn()"})
			}
		case ActorJobNoData:
			if !inSet {
				continue
			}
			if h.Env.ResolveStar != nil {
				if position, ok := h.Env.ResolveStar(actor.Set, job.Target); ok {
					actor.Position = position
				}
			}
			actor.Star, actor.Job = job.Target, nil
			moved = true
			events = append(events, ActorEvent{Actor: actor.Name, Message: "endwalk()"})
		case ActorJobWalk:
			position, heading, walking := job.Walk.Pass(actor.Position, actor.Heading, actor.TurnRate)
			if inSet && (position != actor.Position || heading != actor.Heading) {
				moved = true
			}
			actor.Position, actor.Heading = position, heading
			if !walking {
				actor.Star, actor.Job = job.Target, nil
				events = append(events, ActorEvent{Actor: actor.Name, Message: "endwalk()"})
			}
		}
	}
	return events, moved
}

// LoopEvent is FUN_0040FB00's dispatch for a fired actor loop: it builds
// `"<owner>", <callback>()` and hands it to the actor sendto path.
func LoopEvent(loop ScriptLoop) (ActorEvent, bool) {
	if loop.Kind != LoopKindActor {
		return ActorEvent{}, false
	}
	return ActorEvent{Actor: loop.Owner, Message: loop.Callback + "()"}, true
}

// Deliver raises one event as the native code does, by compiling
// `sendtoactor("<actor>", <message>)` from a System frame.
func (h *GameHost) Deliver(in *Interpreter, event ActorEvent) (uint16, error) {
	return in.RunSource(nil, fmt.Sprintf("sendtoactor(%q,%s)", event.Actor, event.Message))
}

// OpenSet is FUN_0040D9A0, run when a set opens (FUN_00419520): every placed
// actor whose set is the newly current one is moved to its star's location.
func (h *GameHost) OpenSet() {
	current := h.currentSet()
	for _, key := range h.Actors.Names() {
		actor := h.Actors.actors[key]
		if actor.Placed && strings.EqualFold(actor.Set, current) {
			h.placeAtStar(actor)
		}
	}
}
