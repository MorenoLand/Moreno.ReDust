package scripts

import "fmt"

// The sendto family's argument shapes and its child-frame chain. These are what the
// inner handlers were doing behind the comma requirement that sendto.go already
// records, and the shapes turned out to differ per member rather than being uniform.

// SendToArity is how many arguments a sendto member takes.
type SendToArity uint8

const (
	// ArityNone takes no argument at all. Only sendtostage does this, and it is the
	// reason its inner handler looked argument-free and was wrongly recorded as
	// unguarded.
	ArityNone SendToArity = iota
	// ArityOne takes one target name, followed by the mandatory comma.
	ArityOne
	// ArityTwo takes `name , value`, parsed by the shared two-argument evaluator.
	ArityTwo
)

// SendToShape records one member's argument shape and how it parses, verified from its
// inner handler.
type SendToShape struct {
	// Target is the member.
	Target SendToTarget
	// Name is the script keyword.
	Name string
	// Arity is how many arguments it takes.
	Arity SendToArity
	// Parser names the native call that does the parsing, which differs by arity.
	Parser string
	// Resolver is the name lookup the member performs, for the members that have one.
	// sendtostage resolves nothing: it addresses the current stage directly.
	Resolver string
	// ChildFrames is how many nested child contexts it builds.
	ChildFrames int
	// Label is the label its innermost frame uses.
	Label string
}

// The verified shapes, from the four inner handlers decompiled in full.
//
//	sendtostage  FUN_00413230  no argument at all; the shared 0x0B guard, then it
//	                           builds one frame from "Stage Script: " and the stage's
//	                           own name, passed **twice**.
//	sendtoactor  FUN_0040CB60  one name, the mandatory comma after it (0x1C), then
//	                           the actor resolver and the actor's own script
//	                           resolver; it builds **two** frames, labelled
//	                           "Cast Script: " then "Actor Script: ".
//	sendtoflat   FUN_00412EE0  one name and the comma, the flat resolver; **two**
//	                           frames, "Stage Script: " then "Flat Script: ".
//	sendtobutton FUN_00412A90  **two** arguments via the shared two-argument form,
//	                           the comma (0x1C), the flat resolver and a button
//	                           resolver; **four** frames.
//
// Two things are worth stating plainly. **The members disagree about arity**, from none
// through one to two, so a uniform argument shape would be wrong for at least two of
// the four. And **they disagree about how many child contexts they build**, from one to
// four, so a port that entered exactly one would silently drop the inner ones.
var sendToShapes = []SendToShape{
	{
		SendToStage, "sendtostage", ArityNone, "FUN_00413230", "",
		1, PoolLabelStageScript,
	},
	{
		SendToActor, "sendtoactor", ArityOne, "FUN_004220A0",
		ActorVisibleResolver, 2, PoolLabelCastScript,
	},
	{
		SendToFlat, "sendtoflat", ArityOne, "FUN_004220A0",
		"FUN_00413530", 2, PoolLabelFlatScript,
	},
	{
		SendToButton, "sendtobutton", ArityTwo, TwoArgFormHandler,
		"FUN_00413630", 4, PoolLabelButtonScript,
	},
}

// SendToShapeFor returns the verified shape for a target.
func SendToShapeFor(target SendToTarget) (SendToShape, bool) {
	for _, shape := range sendToShapes {
		if shape.Target == target {
			return shape, true
		}
	}
	return SendToShape{}, false
}

// CheckSendToArguments validates a member's argument head against its verified shape and
// returns where to resume and any status.
//
// The rule per arity, all from the reference:
//   - none: nothing is parsed and nothing may be checked, so any record is accepted and
//     resume is the list's own start.
//   - one: the record after the name must be the comma, else the general 0x1C.
//   - two: the shared two-argument form, whose own comma check yields the same 0x1C.
func CheckSendToArguments(shape SendToShape, kinds []uint16) (resume int, status uint16, err error) {
	switch shape.Arity {
	case ArityNone:
		// No argument, so no parse and no comma to require. A sendtostage with
		// records after it is not malformed here; it simply addresses the stage.
		return 0, 0, nil
	case ArityOne:
		return ParseSendToTarget(kinds)
	case ArityTwo:
		return ParseTwoArgForm(kinds)
	}
	return 0, 0, fmt.Errorf("%s has an unknown arity %d", shape.Name, shape.Arity)
}

// ChildFrame is one link of a sendto member's child-context chain. Every inner handler
// builds its chain the same way, and the repeated sequence is what makes it worth
// factoring:
//
//	handle  = FUN_004186D0(context, index);        // the entity resource
//	version = context[0x34D];                       // the version field
//	locked  = GlobalLock(handle);
//	label   = FUN_00427FC0(<the label>);
//	copy(label -> slotA);                          // the label
//	copy(targetName -> slotB);                     // the target's own name
//	copy(parentName -> slotC);                     // the caller's name
//	counter = <0 or 1>;
//	... chain the next frame ...
//	GlobalUnlock(handle); FUN_004188C0(handle);    // release
//
// So each link carries a **label, the target's name and the caller's name**, and
// sendtostage is the degenerate case where the target's name and the caller's name are
// the same string, passed twice. Every link acquires its own resource and releases it
// after the chain, which is why the release count is one per link.
type ChildFrame struct {
	// Label is the context label this frame carries.
	Label string
	// TargetName is the entity's own name.
	TargetName string
	// CallerName is the name of the context that sent the event.
	CallerName string
	// Counter is the frame counter, 0 or 1 as the reference writes them.
	Counter uint16
	// ContextHandle and Index are the entity resource cache key this link acquires.
	ContextHandle uint32
	Index         int32
}

// SameTargetAndCaller reports whether a link's two names are the same string, which is
// how sendtostage's single link is built and how a caller can recognise the degenerate
// case without special-casing the member.
func (f ChildFrame) SameTargetAndCaller() bool {
	return f.TargetName == f.CallerName
}

// SendToChain is a member's child-context chain, innermost first as the handlers build
// it: the counters run 0 for the inner links and 1 for the outermost.
type SendToChain struct {
	// Target is the member this chain belongs to.
	Target SendToTarget
	// Frames are the links, in the order the handler builds them.
	Frames []ChildFrame
}

// BuildSendToChain assembles a chain for a member, taking the per-link labels and names.
// The counters are assigned by position, so the outermost link carries 1 and the rest
// carry 0, which is what the verified handlers write.
func BuildSendToChain(target SendToTarget, labels []string, targetName, callerName string, context uint32, index int32) (SendToChain, error) {
	if len(labels) == 0 {
		return SendToChain{}, fmt.Errorf("a chain needs at least one link")
	}
	chain := SendToChain{Target: target, Frames: make([]ChildFrame, len(labels))}
	for i, label := range labels {
		// The outermost link is the last, and it carries counter 1.
		counter := uint16(0)
		if i == len(labels)-1 {
			counter = 1
		}
		chain.Frames[i] = ChildFrame{
			Label:         label,
			TargetName:    targetName,
			CallerName:    callerName,
			Counter:       counter,
			ContextHandle: context,
			Index:         index,
		}
	}
	return chain, nil
}

// ChainLabelCount reports how many labels a member's chain needs, which must equal the
// member's verified ChildFrames. A mismatch means the chain was built from the wrong
// number of labels.
func ChainLabelCount(chain SendToChain) (int, bool) {
	shape, ok := SendToShapeFor(chain.Target)
	if !ok {
		return 0, false
	}
	return shape.ChildFrames, len(chain.Frames) == shape.ChildFrames
}

// ResourceAcquisitions reports how many resource handles the chain takes and releases.
// Each link acquires one through the entity resource cache and releases it after the
// chain, so both counts equal the number of links.
func ResourceAcquisitions(chain SendToChain) (acquired int, released int) {
	return len(chain.Frames), len(chain.Frames)
}
