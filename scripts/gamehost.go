package scripts

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"redust/assets"
)

// ErrHostOpcodeUnimplemented reports an opcode the game host has not been
// given native semantics for yet. It is an explicit evidence gap: the script
// call fails instead of guessing.
var ErrHostOpcodeUnimplemented = errors.New("script opcode is not implemented by the game host")

// GameHostEnv is what the host needs from the running game.
type GameHostEnv struct {
	// CurrentSet is DAT_00459A82, the open set's name; "" when none is open.
	CurrentSet func() string
	// SetRect reports whether a set is open (DAT_00459A24) and returns the set's
	// pointer rectangle DAT_00442340..46, which FUN_004057D0 builds when the set
	// opens. A nil hook means no set is open.
	SetRect func() (Rect, bool)
	// ResolveStar is FUN_0041B850: a named location in a set.
	ResolveStar func(set, star string) ([3]int16, bool)
	// Player and Camera are DAT_00459A6C and DAT_00459A78.
	Player func() [3]int16
	Camera func() [3]int16
	// ProjectedDepth is FUN_0040DB70 for one actor: its depth and whether it
	// survives the cull.
	ProjectedDepth func(actor *ActorRecord) (int16, bool)
	// PropDepth is the projected depth of a prop the world draws, for propdist
	// (FUN_0041F400 projects the prop with FUN_00421820); false when it is not drawn.
	PropDepth func(name string) (int16, bool)
	// BallBlocked reports whether a ball at cell (x, y) meets a backdrop: the set's
	// view there has Flags set (FUN_00406B70 reads SetView.Flags at +8).
	BallBlocked func(x, y int) bool
	// PlayerCenter is the player's box centre: the cell centres and the camera
	// height (DAT_00459A6C, DAT_00459A6E, DAT_00459A70).
	PlayerCenter func() (x, y, z int32)
	// EndBall sends a prop endball(message) the way a loop callback is sent.
	EndBall func(name, message string) error
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
	// Presenter shows conversations; PuppetFile reads a PUP's script table.
	Presenter  PuppetPresenter
	PuppetFile func(file string) (assets.PuppetFile, error)
	// FileExists reports whether a script file name is present in the asset
	// root. findfile answers from it: the reference probes eight prefixed
	// candidates on disk, and the port has one asset root in their place. Nil
	// makes findfile a gap.
	FileExists func(name string) bool
	// Program loads a script resource from a container.
	Program func(file string, resource uint32) (*Program, error)
	// Shop resolves an open shop's script (FUN_00421520).
	Shop       func(name string) (file string, resource uint32, ok bool)
	PropScript func(name string) (file string, resource uint32, shop string, ok bool)
	// PropInstance registers an instance of a prop under a new name and returns
	// the status propinstance reports.
	PropInstance func(source, name string) (status uint16, added bool)
	// PropDegree is a prop's degree (propdeg).
	PropDegree func(name string) (int16, bool)
	// Ticks is the native time-unit clock delay and the fades wait on.
	Ticks func() uint32
	// PlayerHeading is DAT_00459A6A; Vector is FUN_00406B10/FUN_00406B40.
	PlayerHeading func() int16
	Vector        func(heading, length int16) (dx, dy int16)
	// Managed reports whether an actor runs from its shipped scripts. Actors
	// that do not still have hand-written behavior in the game, so messages and
	// jobs addressed to them are accepted and ignored.
	Managed func(name string) bool
	// CastScript resolves sendtocast's target by the cast's own name.
	CastScript func(name string) (*Program, bool, error)
	// CastCount is DAT_004599E4, the number of casts in the cast table, which
	// countcasts returns.
	CastCount func() int
	// CastName is the Pascal name field of the table record at a zero-based
	// index, which indextocast returns; false when the index is out of range.
	CastName func(index int) (string, bool)
	// OpenSetFile opens a set by file name ("hotlower.set", "nite.set") at its
	// start view; StageScript returns the script a stage message runs against;
	// AdvanceDay runs the day advance and Busy reports engine work (movies,
	// fades, transitions) a blocking command should wait out.
	OpenSetFile func(name string) error
	StageScript func(name string) (*Program, error)
	BootScript  func() (*Program, error)
	// Theme is the playing theme's name, "" when none.
	Theme      func() string
	// ThemeName is the playing theme's own name (currenttheme(2)).
	ThemeName func() string
	// ThemeVolume is themevol's effect (FUN_0040E9E0): level 0..255 for every
	// track of the named theme. Nil leaves the volume alone.
	ThemeVolume func(name string, level int)
	// SoundVolume is soundvol's effect on a named sound; nil leaves it alone.
	SoundVolume func(name string, level int)
	// HaltTheme and PlayTheme are halttheme (FUN_0040E8B0) and playtheme
	// (FUN_0040E6F0, which starts a named theme that has tracks).
	HaltTheme func()
	PlayTheme func(name string)
	// ActionFrame is actionframe(n) (FUN_00415840): whether the last movie
	// latched action marker n (1 or 2). ok is false for an index the port does
	// not track.
	ActionFrame func(n int) (latched, ok bool)
	// SceneMove starts a player movement (1 left, 2 right, 3 straight,
	// 4 backwards) as currentscene("strait") and its siblings do.
	SceneMove func(code int)
	// Instanced is told when actorinstance adds an actor, so the caller can
	// give it the source's cast data.
	Instanced func(source, name string)
	// Diagnose receives debug notes about script calls that failed.
	Diagnose func(message string)
	// SceneAtCell and SceneBuild are rowcoltoscene and scenebuild: the view at
	// a (row, column) and a view's building flag.
	SceneAtCell func(row, col int16) (name string, found bool)
	SceneBuild  func(name string) (building, found bool)
	// SceneCell is the cell centre of a named scene in the open set.
	SceneCell func(name string) (x, y int16, found bool)
	// ResolvePath is FUN_0041BA70: the stored path a walk to the star takes
	// from the actor's current star ("resume" joins any path ending there at
	// the actor's position), or false when the set has none.
	ResolvePath func(set, star, actorStar string, position [3]int16) (*assets.Path, bool)
	// SceneChain is sendtoscene's [scene script, set script] chain for the
	// named scene; found is false when the set has no such scene.
	SceneChain func(name string) (chain []ScriptFrame, found bool, err error)
	// SetScript is the open set's script (the u32 at SET metadata +0x1B78, DAT_00459A48)
	// that sendtosetfx and sendtoset run a message against.
	SetScript func() (*Program, error)
	// FloorScript is the area script the set's table at +0x1B8C holds for a floor index
	// (0..31), which sendtofloorfx runs. found is false for an index past the table.
	FloorScript func(index int) (program *Program, found bool, err error)

	AdvanceDay func() error
	Busy       func() bool
	// View is the player's scene name and facing ("north".."west");
	// SetView moves the player to a scene and facing (currentscene,
	// currentdir).
	View    func() (scene, direction string)
	SetView func(scene, direction string) error
	// Sync publishes the engine's globals to the interpreter, for blocking
	// commands that resume after the player changed engine state.
	Sync func() error
	// SetSize is the open set's metadata words that setwidth (offset 0x26,
	// FUN_0041B450) and setheight (offset 0x28, FUN_0041B4A0) read. open is
	// false when no set is loaded.
	SetSize func() (width, height int16, open bool)
	// CursorVisible receives the cursor's visibility after showcursor or
	// hidecursor changes the display count. Nil leaves the cursor alone.
	CursorVisible func(visible bool)
	// Sound plays a UNILIB sound by name (voicesound, singlesound, ...).
	Sound func(name string) error
	// CastManaged reports whether any of a cast's actors run from their
	// scripts; a cast with none keeps its hand-written behavior.
	CastManaged func(name string) bool
	// CurrentFlat is the open flat's name (FUN_004125C0).
	CurrentFlat func() string
	// FlatCount is the open stage's flat count (the dword at +0x834 of its block,
	// FUN_00411FC0). The bool is false when no stage is open.
	FlatCount func() (int, bool)
	// FlatName is the open stage's 0-based flat name (record +0x0C, FUN_00412010).
	// The bool is false when the index is outside the table.
	FlatName func(index int) (string, bool)
	// FlatIndex resolves a flat name to its 0-based index in the open stage (FUN_00413590,
	// via FUN_00413530). The bool is false when no flat has the name.
	FlatIndex func(name string) (int, bool)
	// FlatButtonCount is a flat's button count, dword 0 of its button resource
	// (FUN_00412110). FlatButtonName is a button's name, row +0x10 (FUN_00412210).
	FlatButtonCount func(flat int) (int, error)
	FlatButtonName  func(flat, index int) (string, error)
	// FlatScript is the program of the flat at index, its record's script
	// resource (dword 0 of the record, read by FUN_00412EE0). FlatButtonScript is
	// the program of a button row, the row's script resource at +0x0C (FUN_00412A90).
	// Both report ok false when the port cannot run that script, which leaves the
	// flat message to Fallback (see gamehost_flats.go).
	FlatScript       func(flat int) (program *Program, ok bool, err error)
	FlatButtonScript func(flat, index int) (program *Program, ok bool, err error)
	// FlatHandWritten reports a flat whose messages the port answers by hand
	// (mainpanel and death, see mainScriptFallback in main.go). A value send to one
	// of them has no port-side value, so sendtoflatfx and sendtobuttonfx report a gap.
	FlatHandWritten func(name string) bool
	// SceneCount is the open set's scene count (dword at +0x1C0C, FUN_0041B4F0). The
	// bool is false when no set is open.
	SceneCount func() (int, bool)
	// SceneName is the open set's 0-based scene name (row +0x0C, FUN_0041B540).
	SceneName func(index int) (string, bool)
	// Voice plays voicesound on the voice channel currentvoice reports.
	Voice func(name string) error
	// Tracks is the open track files and sound channels.
	Tracks TrackHost
	// StageOpen reports whether a stage is open (DAT_00459A00). pointinbutton
	// returns 0x0B without one, before it evaluates its arguments.
	StageOpen func() bool
	// FlatButtons is the button block of the named flat in the open stage
	// (FUN_00413530 by name, then the block the flat's record names). found is
	// false for a flat the stage does not have.
	FlatButtons func(flat string) (buttons []SceneButton, found bool, err error)
	// PropPoint resolves the prop pointinprop names, from the prop table and store
	// the draw reads, with the frame FUN_00421B70 selects for it. found is false for
	// a name the prop table does not have (status 0x0A). A nil hook makes
	// pointinprop a gap.
	PropPoint func(name string) (record PropPointRecord, found bool, err error)
	// Fallback handles opcodes this host does not; nil makes them gaps.
	Fallback ScriptHost
	// Log receives diagnostics when non-nil.
	Log func(format string, args ...any)
	// Dialogs shows the boxes notedialog and questiondialog raise; the script
	// waits in them until the player closes one.
	Dialogs DialogPresenter
}

// GameHost executes script commands against the actor table, the loop
// scheduler and the running game.
type GameHost struct {
	Env    GameHostEnv
	Actors *ScriptActors
	Loops  *LoopScheduler
	Random *NativeRandom
	// Shops are the shop files open for the whole game, in openshopfile order
	// (FUN_00420110 appends each record). BOOTFILE opens house.prp, then inven.prp.
	Shops []string
	// PuppetGrab and PuppetBase are DAT_004599AC and the puppetbase name.
	PuppetGrab bool
	// PuppetParams are puppetparam's eight 16-bit slots (DAT_0045999C..AA).
	PuppetParams [8]int16
	PuppetBase   string
	// KeyAborts is DAT_00459996, the keyaborts flag. keyaborts(bool) stores the
	// low word of its argument and keyaborts() reads it back sign-extended.
	KeyAborts int16
	// StageVisible is DAT_00459A02, the word stagevisible 16008 writes. Nothing
	// in the port reads it yet, since pointinstage 20042 is not wired.
	StageVisible int16
	// Props is the prop table scripts read and write; the game keeps its own
	// copies of owner and degree in step around script runs.
	Props      *ScriptProps
	task       *ScriptTask
	puppet     *puppetSession
	passWanted bool
	// setVisible is DAT_00459A26, the flag setvisible writes and pointinset gates
	// on. FUN_004195F0 sets it to 1 when a set opens, which OpenSet mirrors. The
	// zero value matches the native zero held before any set has opened.
	setVisible bool
	// cursorCount is the Win32 display count that showcursor and hidecursor
	// move; the cursor shows at zero or more.
	cursorCount int
}

// setRect reads the set gate and the pointer rectangle through the host hook.
func (h *GameHost) setRect() (Rect, bool) {
	if h.Env.SetRect == nil {
		return Rect{}, false
	}
	return h.Env.SetRect()
}

func (h *GameHost) managed(name string) bool {
	return h.Env.Managed == nil || h.Env.Managed(name)
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

// openFlats is the open stage's flat count. The bool is false when no stage is
// open, which FUN_00411FC0 and FUN_00412010 test before anything else.
func (h *GameHost) openFlats() (int, bool) {
	if h.Env.FlatCount == nil || h.Env.FlatName == nil {
		return 0, false
	}
	return h.Env.FlatCount()
}

// openScenes is the open set's scene count. The bool is false when no set is open,
// which FUN_0041B4F0 and FUN_0041B540 test first (status 0x28).
func (h *GameHost) openScenes() (int, bool) {
	if h.Env.SceneCount == nil || h.Env.SceneName == nil {
		return 0, false
	}
	return h.Env.SceneCount()
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
		if h.Env.Diagnose != nil {
			h.Env.Diagnose(fmt.Sprintf("unknown actor %q (status %#x)", args[0].Text, status))
		}
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
	consumed, status, err := h.command(call)
	if status != 0 && h.Env.Diagnose != nil {
		h.Env.Diagnose(fmt.Sprintf("%s returned status %#x", opcodeName(call), status))
	}
	return consumed, status, err
}

func (h *GameHost) command(call *ScriptCall) (int, uint16, error) {
	name := opcodeName(call)
	switch name {
	case "path":
		return h.pathStatement(call)
	case "notedialog":
		return h.noteDialog(call)
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
				if h.Env.Diagnose != nil {
					h.Env.Diagnose(fmt.Sprintf("actor %q has no pose %q (poses %v)", actor.Name, args[1].Text, actor.Poses))
				}
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
			// The player's box is DAT_00459A28 (z) and DAT_00459A2A (x and y).
			if h.Props != nil {
				h.Props.PlayerBox = [2]int16{int16(args[1].Int), int16(args[2].Int)}
			}
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
	case "variable":
		// FUN_00426900: variable(name, value) assigns the variable the string
		// names, looked up in the locals and then the globals.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, nil
		}
		table, id, status, err := call.Interpreter.variableByName(call.Locals, args[0].Text)
		if err != nil || status != 0 {
			return 0, status, err
		}
		record, status, err := call.Interpreter.Record(args[1])
		if err != nil || status != 0 {
			return 0, status, err
		}
		var encoded ExpressionValue
		binary.LittleEndian.PutUint16(encoded[:2], record.Kind)
		binary.LittleEndian.PutUint32(encoded[2:6], record.Data)
		status, err = table.WriteValue(id, encoded, call.Interpreter.Strings)
		if err != nil || status != 0 {
			return 0, status, err
		}
		return consumed, 0, nil
	case "actorinstance":
		// FUN_0040A860: a new actor that starts as a copy of the source's
		// record; an existing name is left alone.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 3 {
			return 0, ScriptStatusWrongType, nil
		}
		source, status := h.Actors.Lookup(args[0].Text)
		if status != 0 {
			return 0, status, nil
		}
		if len(args[1].Text) > 15 {
			return 0, 0x1a, nil
		}
		if _, exists := h.Actors.Lookup(args[1].Text); exists != 0 {
			copied := *source
			copied.Name, copied.Job = args[1].Text, nil
			h.Actors.Add(&copied)
			if h.Env.Instanced != nil {
				h.Env.Instanced(source.Name, copied.Name)
			}
		}
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
		if kind == LoopKindActor && !h.managed(args[1].Text) {
			return consumed, 0, nil
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
		if kind == LoopKindActor && !strings.EqualFold(args[1].Text, "all") && !h.managed(args[1].Text) {
			return consumed, 0, nil
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
	case "debugger":
		return debuggerCommand(call)
	case "keyaborts":
		return h.keyAbortsCommand(call)
	case "halttheme":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if h.Env.HaltTheme != nil {
			h.Env.HaltTheme()
		}
		return consumed, 0, nil
	case "playtheme":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusMalformed, nil
		}
		if h.Env.PlayTheme != nil {
			h.Env.PlayTheme(args[0].Text)
		}
		return consumed, 0, nil
	case "soundvol":
		// FUN_0040E910: soundvol(name, level) sets one sound's volume, 0..255.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 {
			return 0, ScriptStatusMalformed, nil
		}
		if args[1].Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
		level := int(args[1].Int)
		if level < 0 {
			level = 0
		} else if level > 0xff {
			level = 0xff
		}
		if h.Env.SoundVolume != nil {
			h.Env.SoundVolume(args[0].Text, level)
		}
		return consumed, 0, nil
	case "themevol":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 {
			return 0, ScriptStatusMalformed, nil
		}
		if args[1].Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
		level := int(args[1].Int)
		if level < 0 {
			level = 0
		} else if level > 0xff {
			level = 0xff
		}
		if h.Env.ThemeVolume != nil {
			h.Env.ThemeVolume(args[0].Text, level)
		}
		return consumed, 0, nil
	case "turntodeg":
		actor, args, consumed, status, err := h.actorArgs(call, 2)
		if err != nil || status != 0 {
			return 0, status, err
		}
		if args[1].Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
		actor.Job = &ActorJob{Mode: ActorJobTurn, Target: actor.Star, Heading: int16(args[1].Int & 0xff)}
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
	case "sendtoset":
		return h.sendToSetFx(call, nil)
	case "sendtofloor":
		return h.sendToFloorFx(call, nil)
	case "stagevisible":
		return h.stageVisibleCommand(call)
	case "sendtoactor":
		return h.sendToActor(call)
	case "sendtoscene":
		return h.sendToScene(call)
	case "sendtoflat":
		return h.sendToFlat(call)
	case "sendtobutton":
		return h.sendToButton(call)
	case "sendtocast":
		if target, _, skipped, status, err := call.SendParts(); err == nil && status == 0 && h.Env.CastManaged != nil && !h.Env.CastManaged(target.Text) {
			return skipped, 0, nil
		}
		consumed, status, err := h.sendToScript(call, "Cast Script: ", func(name string) (*Program, string, uint16, error) {
			if h.Env.CastScript == nil {
				return nil, "", 0, fmt.Errorf("sendtocast has no cast table")
			}
			program, ok, err := h.Env.CastScript(name)
			if err != nil || !ok {
				return nil, "", 0x0a, err
			}
			return program, strings.ToLower(name), 0, nil
		}, nil)
		return consumed, status, err
	}
	if consumed, status, handled, err := h.hostCommand(name, call); handled {
		return consumed, status, err
	}
	if consumed, status, handled, err := h.soundCommand(name, call); handled {
		return consumed, status, err
	}
	if consumed, status, handled, err := h.puppetCommand(name, call); handled {
		return consumed, status, err
	}
	if consumed, status, handled, err := h.propCommand(name, call); handled {
		return consumed, status, err
	}
	if consumed, status, handled, err := h.worldCommand(name, call); handled {
		return consumed, status, err
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
	// FUN_0041BA70 looks for a stored path between the actor's star and the
	// destination first; only without one does the actor walk straight.
	if _, literal := parseCoordinateStar(star); !literal && h.Env.ResolvePath != nil && h.Env.Heading != nil {
		if path, found := h.Env.ResolvePath(actor.Set, star, actor.Star, actor.Position); found && len(path.Points) >= 2 {
			heading := h.Env.Heading(path.Points[0], path.Points[1])
			actor.Job = &ActorJob{Mode: ActorJobWalk, Target: star, Heading: heading, Path: NewNativePathWalk(path, actor.Speed)}
			return 0, nil
		}
	}
	// FUN_0041B960 accepts a literal "x,y,z" destination, after which the
	// job's star is "custom"; otherwise the star is a named location.
	destination, ok := parseCoordinateStar(star)
	if ok {
		star = "custom"
	} else if destination, ok = h.Env.ResolveStar(actor.Set, star); !ok {
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
	if status != 0 && h.managed(value.Text) {
		return 0, status, nil
	}
	// An actor the port does not track (the instances actorinstance makes,
	// such as horse2) is as unmanaged as a hand-written one.
	if status != 0 || !h.managed(actor.Name) {
		// Hand-written actors keep their own behavior; skip the message.
		end, status, err := ParenthesizedBlockScan(call.Program.Records, call.Start+1)
		if err != nil || status != 0 {
			return 0, status, err
		}
		return end - call.Start, 0, nil
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

// packPoint packs x and y into a point dword the way the native reads it: x is the
// high word and y the low word (pointx reads the high word, pointy the low).
func packPoint(position [3]int16) int32 {
	return int32(uint32(uint16(position[0]))<<16 | uint32(uint16(position[1])))
}

// unpackPoint is the inverse of packPoint for the two components a point has. It
// returns [x, y, 0]: index 0 is the high word (x) and index 1 the low word (y), the
// same convention as scripts.Point. The third slot is not part of a packed point.
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
	record, consumed, status, err := h.value(call)
	if status != 0 && h.Env.Diagnose != nil {
		h.Env.Diagnose(fmt.Sprintf("value %s returned status %#x", opcodeName(call), status))
	}
	return record, consumed, status, err
}

func (h *GameHost) value(call *ScriptCall) (Record, int, uint16, error) {
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
	case "pointinset":
		// FUN_0041B330: the argument must be numeric (0x0E) and gives no result
		// otherwise. The result is a type-2 boolean, false unless a set is open
		// (DAT_00459A24) and DAT_00459A26 is set, and otherwise the hit test of
		// the packed point on the set's pointer rectangle.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		rect, open := h.setRect()
		result, status := PointInSetValue(args[0].Kind, uint32(args[0].Int), SetState{Active: open, Visible: h.setVisible}, rect)
		if status != 0 {
			return Record{}, 0, status, nil
		}
		return result.Record(), consumed, 0, nil
	case "questiondialog":
		return h.questionDialog(call)
	case "textdialog":
		return h.textDialog(call)
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
	case "keyaborts":
		return h.keyAbortsValue(call)
	case "findfile":
		return h.findFileValue(call)
	case "countactors":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		return number(int32(h.Actors.Count()), consumed)
	case "indextoactor":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		actor, ok := h.Actors.At(int(args[0].Int))
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		return text(actor.Name, consumed)
	case "propdist":
		prop, _, consumed, status, err := h.propArgs(call, 1)
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		// A prop that is not drawn reports 32000, as FUN_0041F400 does.
		distance := int32(32000)
		if h.Env.PropDepth != nil {
			if depth, ok := h.Env.PropDepth(prop.Name); ok {
				distance = int32(depth)
			}
		}
		return number(distance, consumed)
	case "sqrt":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		return number(int32(integerSquareRoot(uint32(args[0].Int))), consumed)
	case "countflats":
		count, open := h.openFlats()
		if !open {
			return Record{}, 0, StatusNoActiveStage, nil
		}
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		return number(int32(count), consumed)
	case "indextoflat":
		count, open := h.openFlats()
		if !open {
			return Record{}, 0, StatusNoActiveStage, nil
		}
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		index := int(args[0].Int)
		if index < 1 || index > count {
			return Record{}, 0, 0x0a, nil
		}
		name, ok := h.Env.FlatName(index - 1)
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		return text(name, consumed)
	case "countbuttons":
		if _, open := h.openFlats(); !open {
			return Record{}, 0, StatusNoActiveStage, nil
		}
		if h.Env.FlatIndex == nil || h.Env.FlatButtonCount == nil {
			return Record{}, 0, 0, h.gap(call)
		}
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		flat, found := h.Env.FlatIndex(args[0].Text)
		if !found {
			return Record{}, 0, 0x0a, nil
		}
		count, err := h.Env.FlatButtonCount(flat)
		if err != nil {
			return Record{}, 0, 0, err
		}
		return number(int32(count), consumed)
	case "indextobutton":
		if _, open := h.openFlats(); !open {
			return Record{}, 0, StatusNoActiveStage, nil
		}
		if h.Env.FlatIndex == nil || h.Env.FlatButtonCount == nil || h.Env.FlatButtonName == nil {
			return Record{}, 0, 0, h.gap(call)
		}
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		flat, found := h.Env.FlatIndex(args[0].Text)
		if !found {
			return Record{}, 0, 0x0a, nil
		}
		if args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		count, err := h.Env.FlatButtonCount(flat)
		if err != nil {
			return Record{}, 0, 0, err
		}
		index := int(args[1].Int)
		if index < 1 || index > count {
			return Record{}, 0, 0x0a, nil
		}
		name, err := h.Env.FlatButtonName(flat, index-1)
		if err != nil {
			return Record{}, 0, 0, err
		}
		return text(name, consumed)
	case "countglobals":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		var globals *VariableTable
		if call.Interpreter != nil {
			globals = call.Interpreter.Global
		}
		return number(int32(globals.Count()), consumed)
	case "indextoglobal":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		var globals *VariableTable
		if call.Interpreter != nil {
			globals = call.Interpreter.Global
		}
		pascal, ok := globals.Name(int(args[0].Int) - 1)
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		return text(string(pascal[1:]), consumed)
	case "countscenes":
		count, open := h.openScenes()
		if !open {
			return Record{}, 0, StatusNoActiveSet, nil
		}
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		return number(int32(count), consumed)
	case "indextoscene":
		count, open := h.openScenes()
		if !open {
			return Record{}, 0, StatusNoActiveSet, nil
		}
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		index := int(args[0].Int)
		if index < 1 || index > count {
			return Record{}, 0, 0x0a, nil
		}
		name, ok := h.Env.SceneName(index - 1)
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		return text(name, consumed)
	case "countshops":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		return number(int32(len(h.Shops)), consumed)
	case "indextoshop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		index := int(args[0].Int)
		if index < 1 || index > len(h.Shops) {
			return Record{}, 0, 0x0a, nil
		}
		return text(h.Shops[index-1], consumed)
	case "countwalks":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		return number(int32(h.Actors.ActiveWalkCount()), consumed)
	case "indextowalk":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		name, ok := h.Actors.ActiveWalkName(int(args[0].Int))
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		return text(name, consumed)
	case "countloops":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		kind, ok := LoopKindByName(args[0].Text)
		if !ok {
			return number(0, consumed)
		}
		return number(int32(h.Loops.LoopCount(kind)), consumed)
	case "indextoloop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		kind, ok := LoopKindByName(args[0].Text)
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		owner, found := h.Loops.LoopOwner(kind, int(args[1].Int))
		if !found {
			return Record{}, 0, 0x0a, nil
		}
		return text(owner, consumed)
	case "isloop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		kind, ok := LoopKindByName(args[0].Text)
		return boolean(ok && h.Loops.LoopExists(kind, args[1].Text), consumed)
	case "countballs":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if h.Props == nil {
			return Record{}, 0, 0, fmt.Errorf("props are unavailable")
		}
		return number(int32(h.Props.ActiveBallCount()), consumed)
	case "indextoball":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		if h.Props == nil {
			return Record{}, 0, 0, fmt.Errorf("props are unavailable")
		}
		name, ok := h.Props.ActiveBallName(int(args[0].Int))
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		return text(name, consumed)
	case "isball":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		if h.Props == nil {
			return Record{}, 0, 0, fmt.Errorf("props are unavailable")
		}
		return boolean(h.Props.BallActive(args[0].Text), consumed)
	case "countprops":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if h.Props == nil {
			return Record{}, 0, 0, fmt.Errorf("props are unavailable")
		}
		return number(int32(h.Props.TableCount()), consumed)
	case "indextoprop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		if h.Props == nil {
			return Record{}, 0, 0, fmt.Errorf("props are unavailable")
		}
		name, ok := h.Props.TableName(int(args[0].Int))
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		return text(name, consumed)
	case "countcasts":
		// FUN_0040D060 (opcode 20029) takes no arguments and returns DAT_004599E4
		// as a number. Like countprops, the empty parentheses are parsed through
		// call.Args(), and a nil hook is a gap.
		if h.Env.CastCount == nil {
			return Record{}, 0, 0, h.gap(call)
		}
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		return number(int32(h.Env.CastCount()), consumed)
	case "indextocast":
		// FUN_0040D190 (opcode 20030) evaluates one argument. A non-number is
		// status 0x0E and an index outside 1..count is 0x0A. Otherwise the
		// 28-byte record at index-1 yields its 16-byte Pascal name field (record
		// +0x0C) as a string, through FUN_00421F60.
		if h.Env.CastCount == nil || h.Env.CastName == nil {
			return Record{}, 0, 0, h.gap(call)
		}
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		index := int(args[0].Int)
		if index < 1 || index > h.Env.CastCount() {
			return Record{}, 0, 0x0a, nil
		}
		name, ok := h.Env.CastName(index - 1)
		if !ok {
			return Record{}, 0, 0x0a, nil
		}
		return text(name, consumed)
	case "actionframe":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 4 || args[0].Int != 1 && args[0].Int != 2 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		if h.Env.ActionFrame == nil {
			return Record{}, 0, 0, fmt.Errorf("%w: actionframe", ErrHostOpcodeUnimplemented)
		}
		latched, ok := h.Env.ActionFrame(int(args[0].Int))
		if !ok {
			return Record{}, 0, 0, fmt.Errorf("%w: actionframe(%d)", ErrHostOpcodeUnimplemented, args[0].Int)
		}
		return boolean(latched, consumed)
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
	case "variable":
		// FUN_00416570: the value of the variable the string names.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		table, id, status, err := call.Interpreter.variableByName(call.Locals, args[0].Text)
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		record, status, err := table.ReadValue(id, call.Interpreter.Strings)
		return record, consumed, status, err
	case "scenerow", "scenecol":
		// FUN_0041AFD0: the scene's cell row (its scene id) or column (its
		// direction id).
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		if h.currentSet() == "" {
			return Record{}, 0, 0x28, nil
		}
		if h.Env.SceneCell == nil {
			return Record{}, 0, 0, fmt.Errorf("%w: %s", ErrHostOpcodeUnimplemented, name)
		}
		x, y, found := h.Env.SceneCell(args[0].Text)
		if !found {
			return Record{}, 0, 0x0a, nil
		}
		if name == "scenecol" {
			return number(int32((x-128)/256), consumed)
		}
		return number(int32((y-128)/256), consumed)
	case "scenexyz":
		// FUN_004150C0: the named scene's cell as a world point, the cell
		// centre (256 a cell, plus 128), with z 0.
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, err
		}
		if len(args) != 2 || args[0].Kind != 3 || args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, nil
		}
		if h.currentSet() == "" {
			return Record{}, 0, 0x28, nil
		}
		if h.Env.SceneCell == nil {
			return Record{}, 0, 0, fmt.Errorf("%w: scenexyz", ErrHostOpcodeUnimplemented)
		}
		x, y, found := h.Env.SceneCell(args[0].Text)
		if !found {
			return Record{}, 0, 0x0a, nil
		}
		value, ok := pointComponent([3]int16{x, y, 0}, args[1].Int)
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
	case "pointinbutton":
		return h.pointInButton(call)
	case "pointinprop":
		return h.pointInProp(call)
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
	if value, consumed, status, handled, err := h.sendToValue(name, call); handled {
		return value, consumed, status, err
	}
	if value, consumed, status, handled, err := h.hostValue(name, call); handled {
		return value, consumed, status, err
	}
	if value, consumed, status, handled, err := h.soundValue(name, call); handled {
		return value, consumed, status, err
	}
	if value, consumed, status, handled, err := h.puppetValue(name, call); handled {
		return value, consumed, status, err
	}
	if value, consumed, status, handled, err := h.propValue(name, call); handled {
		return value, consumed, status, err
	}
	if value, consumed, status, handled, err := h.worldValue(name, call); handled {
		return value, consumed, status, err
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

// actorFrameWrap bounds the free-running frame counter; the pose lookup takes
// it modulo the pose's sequence length, and 27720 is divisible by every
// plausible length.
const actorFrameWrap = 27720

// Pass advances every actor job once, as the native walk pass FUN_00410290
// does, and returns the endwalk/endturn events to deliver. moved reports
// whether any actor in the open set changed position or heading.
func (h *GameHost) Pass() (events []ActorEvent, moved bool) {
	current := h.currentSet()
	for _, key := range h.Actors.Names() {
		actor := h.Actors.actors[key]
		job := actor.Job
		inSet := strings.EqualFold(actor.Set, current)
		if inSet && actor.Visible {
			// FUN_0040DEC0 steps the pose's frame sequence on every draw;
			// the pump draws once per pass. Poses other than "stand" are the
			// ones whose sequences animate, so only they force a redraw.
			actor.Frame = (actor.Frame + 1) % actorFrameWrap
			if !strings.EqualFold(actor.Pose, "stand") {
				moved = true
			}
		}
		if job == nil || job.Paused {
			continue
		}
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
			if !job.TurnDone {
				// FUN_00410290 first turns toward the route bearing and
				// raises endturn() when it arrives, before any step is taken;
				// the script's endturn() is what switches to the walk pose.
				if inSet {
					actor.Heading = NativeTurnStep(actor.Heading, job.Heading, actor.TurnRate)
					moved = true
				} else {
					actor.Heading = job.Heading
				}
				if actor.Heading == job.Heading {
					job.TurnDone = true
					events = append(events, ActorEvent{Actor: actor.Name, Message: "endturn()"})
				}
				continue
			}
			var position [3]int16
			var heading int16
			var walking bool
			if job.Path != nil {
				position, heading, walking = job.Path.Pass(h.Env.Heading)
			} else {
				position, heading, walking = job.Walk.Pass(actor.Position, actor.Heading, actor.TurnRate)
			}
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
	// FUN_004195F0 sets DAT_00459A26 to 1 as the set opens.
	h.setVisible = true
	current := h.currentSet()
	for _, key := range h.Actors.Names() {
		actor := h.Actors.actors[key]
		if actor.Placed && strings.EqualFold(actor.Set, current) {
			h.placeAtStar(actor)
		}
	}
}

// Run compiles and runs a source statement as the System frame does, for
// engine-raised calls such as sendtocast("gang", initactors()).
func (h *GameHost) Run(in *Interpreter, source string) (uint16, error) {
	return in.RunSource(nil, source)
}

// AbortPuppet closes a conversation a script left open, as happens when a
// script stops on an error between openpuppetfile and closepuppetfile.
func (h *GameHost) AbortPuppet() error {
	if h.puppet == nil {
		return nil
	}
	h.puppet = nil
	if h.Env.Presenter != nil {
		return h.Env.Presenter.ClosePuppet()
	}
	return nil
}
