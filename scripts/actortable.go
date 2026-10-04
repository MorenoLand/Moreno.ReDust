package scripts

import (
	"fmt"
	"sort"
	"strings"
)

// ActorRecord is the script-visible part of one native actor block, the
// record FUN_0040D730 resolves by name and FUN_0040D800 stores back. Field
// offsets are the native block's.
type ActorRecord struct {
	// Name is the actor's resolver key.
	Name string
	// Cast is the cast file the actor was loaded from.
	Cast string
	// Visible is +0x00, written by actorvisible (FUN_0040B580).
	Visible bool
	// Placed is +0x12: actorset, actorstar and actorxyz set it so the next
	// pass reprojects the actor; actordist reads it (FUN_0040B070).
	Placed bool
	// Heading is +0x18, actordeg's value masked to a byte (FUN_0040AF20).
	Heading int16
	// Position is +0x1A..+0x1E (FUN_0040B6B0, FUN_0040B7B0).
	Position [3]int16
	// Pose is the pose name; actorpose resets Frame (+0x26) to 0.
	Pose  string
	Frame int16
	// Speed is +0x28, clamped to 1..256 (FUN_0040B1C0).
	Speed int16
	// Depth is +0x2A, the depth cached by the last projection.
	Depth int16
	// Scale is +0x2C, clamped to at least 0 (FUN_0040B280).
	Scale int32
	// TurnRate is actorturn's value, clamped to at least 1 (FUN_0040D240).
	TurnRate int16
	// ZClip is +0x4E, stored unclamped (FUN_0040D4F0).
	ZClip int16
	// HitboxWidth is +0x50; actorhitbox stores only its first number for an
	// actor (FUN_0040BC40).
	HitboxWidth int16
	// Value is actorvalue (FUN_0040D450).
	Value int32
	// Set, Star and Owner are the 16-byte name fields at +0x64, +0x74 and
	// the owner field (FUN_0040AA00, FUN_0040ABD0, FUN_0040D380).
	Set, Star, Owner string
	// Poses lists the pose names the cast record defines (FUN_0040E050).
	Poses []string
	// Job is the actor's pending walk or turn, one of the sixteen native
	// slots at DAT_00442E68.
	Job *ActorJob
}

// ActorJobMode distinguishes the native job kinds.
type ActorJobMode uint8

const (
	// ActorJobWalk is "walktostar": a straight walk in the current set.
	ActorJobWalk ActorJobMode = iota + 1
	// ActorJobNoData is "walknodata": a walk queued while the actor's set is
	// not current; it completes once that set is open.
	ActorJobNoData
	// ActorJobTurn is turntodeg's turn job.
	ActorJobTurn
)

// ActorJob is one native walk/turn slot.
type ActorJob struct {
	Mode    ActorJobMode
	Target  string
	Heading int16
	Walk    *NativeActorWalkJob
	Paused  bool
}

// ScriptActors holds every loaded actor, keyed case-insensitively as
// FUN_0040D730 resolves them.
type ScriptActors struct {
	actors map[string]*ActorRecord
	order  []string
}

func NewScriptActors() *ScriptActors {
	return &ScriptActors{actors: map[string]*ActorRecord{}}
}

// Add registers an actor. A later cast with the same name replaces it.
func (t *ScriptActors) Add(actor *ActorRecord) {
	key := strings.ToLower(actor.Name)
	if _, exists := t.actors[key]; !exists {
		t.order = append(t.order, key)
	}
	t.actors[key] = actor
}

// Count and At are countactors and indextoactor's table (FUN_0040D030,
// FUN_0040D090): actors in load order, indexed from 1.
func (t *ScriptActors) Count() int { return len(t.order) }

func (t *ScriptActors) At(index int) (*ActorRecord, bool) {
	if index < 1 || index > len(t.order) {
		return nil, false
	}
	return t.actors[t.order[index-1]], true
}

// Lookup is the resolver FUN_0040D730; a miss is status 0x0A.
func (t *ScriptActors) Lookup(name string) (*ActorRecord, uint16) {
	if t == nil {
		return nil, 0x0a
	}
	actor, ok := t.actors[strings.ToLower(name)]
	if !ok {
		return nil, 0x0a
	}
	return actor, 0
}

// Names returns the actor names in a stable order.
func (t *ScriptActors) Names() []string {
	names := make([]string, 0, len(t.actors))
	for name := range t.actors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewCastActorRecord is the actor constructor FUN_0040C1F0: hidden, heading
// 0, turn rate 16, speed 4, depth -1, scale 1000, owner "None", set, star and
// pose from the cast record. The caller places it when its set is open.
func NewCastActorRecord(cast, name, set, star, pose string, poses []string) *ActorRecord {
	return &ActorRecord{Name: name, Cast: cast, Set: set, Star: star, Pose: pose, Poses: poses, TurnRate: 16, Speed: 4, Depth: -1, Scale: 1000, Owner: "None"}
}

// HasPose is FUN_0040E050's pose existence test.
func (a *ActorRecord) HasPose(name string) bool {
	for _, pose := range a.Poses {
		if strings.EqualFold(pose, name) {
			return true
		}
	}
	return false
}

// StopJob is FUN_0040F6A0 for one actor: cancel its walk or turn.
func (a *ActorRecord) StopJob() {
	a.Job = nil
}

// nativeName checks the 15-byte limit every name setter enforces with
// status 0x1A.
func nativeName(value string) uint16 {
	if len(value) > 15 {
		return 0x1a
	}
	return 0
}

func (a *ActorRecord) String() string {
	return fmt.Sprintf("%s set=%s star=%s pos=%v deg=%d pose=%s visible=%t", a.Name, a.Set, a.Star, a.Position, a.Heading, a.Pose, a.Visible)
}

// ActorRecordState is the persisted form of an ActorRecord.
type ActorRecordState struct {
	Visible     bool                  `json:"visible"`
	Placed      bool                  `json:"placed"`
	Heading     int16                 `json:"heading"`
	Position    [3]int16              `json:"position"`
	Pose        string                `json:"pose"`
	Frame       int16                 `json:"frame"`
	Speed       int16                 `json:"speed"`
	Depth       int16                 `json:"depth"`
	Scale       int32                 `json:"scale"`
	TurnRate    int16                 `json:"turnRate"`
	ZClip       int16                 `json:"zclip"`
	HitboxWidth int16                 `json:"hitboxWidth"`
	Value       int32                 `json:"value"`
	Set         string                `json:"set"`
	Star        string                `json:"star"`
	Owner       string                `json:"owner"`
	JobMode     ActorJobMode          `json:"jobMode,omitempty"`
	JobTarget   string                `json:"jobTarget,omitempty"`
	JobHeading  int16                 `json:"jobHeading,omitempty"`
	JobWalk     *NativeActorWalkState `json:"jobWalk,omitempty"`
}

// Snapshot returns the persisted state of the named actors.
func (t *ScriptActors) Snapshot(names []string) map[string]ActorRecordState {
	states := map[string]ActorRecordState{}
	for _, name := range names {
		actor, status := t.Lookup(name)
		if status != 0 {
			continue
		}
		state := ActorRecordState{Visible: actor.Visible, Placed: actor.Placed, Heading: actor.Heading, Position: actor.Position, Pose: actor.Pose, Frame: actor.Frame, Speed: actor.Speed, Depth: actor.Depth, Scale: actor.Scale, TurnRate: actor.TurnRate, ZClip: actor.ZClip, HitboxWidth: actor.HitboxWidth, Value: actor.Value, Set: actor.Set, Star: actor.Star, Owner: actor.Owner}
		if job := actor.Job; job != nil {
			state.JobMode, state.JobTarget, state.JobHeading = job.Mode, job.Target, job.Heading
			if job.Walk != nil {
				walk := job.Walk.Snapshot()
				state.JobWalk = &walk
			}
		}
		states[strings.ToLower(name)] = state
	}
	return states
}

// Restore applies persisted states to existing actors.
func (t *ScriptActors) Restore(states map[string]ActorRecordState) error {
	for name, state := range states {
		actor, status := t.Lookup(name)
		if status != 0 {
			return fmt.Errorf("saved script actor %q is not loaded", name)
		}
		actor.Visible, actor.Placed, actor.Heading, actor.Position = state.Visible, state.Placed, state.Heading, state.Position
		actor.Pose, actor.Frame, actor.Speed, actor.Depth, actor.Scale = state.Pose, state.Frame, state.Speed, state.Depth, state.Scale
		actor.TurnRate, actor.ZClip, actor.HitboxWidth, actor.Value = state.TurnRate, state.ZClip, state.HitboxWidth, state.Value
		actor.Set, actor.Star, actor.Owner, actor.Job = state.Set, state.Star, state.Owner, nil
		if state.JobMode != 0 {
			job := &ActorJob{Mode: state.JobMode, Target: state.JobTarget, Heading: state.JobHeading}
			if state.JobWalk != nil {
				walk, err := RestoreNativeActorWalkJob(*state.JobWalk)
				if err != nil {
					return fmt.Errorf("saved script actor %q walk: %w", name, err)
				}
				job.Walk = walk
			}
			actor.Job = job
		}
	}
	return nil
}
