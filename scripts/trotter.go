package scripts

import (
	"fmt"
	"strings"

	"redust/assets"
)

type TrotterStep struct {
	Kind                 TrotterActionKind
	Route                TrotterRoute
	Speech               []string
	SetTrotterPhase      int16
	SetTrotterPhaseValid bool
	Finish               bool
	Choices              [][]PuppetChoice
	EventID              int32
	Continuation         *TrotterContinuation
	Effects              []TrotterEffect
	ScrambleChoices      bool
	SpeechStages         []TrotterSpeechStage
	ChoiceTimeoutFrames  uint32
}

type TrotterSpeechStage struct {
	Speech      []string
	DelayBefore uint32
}

func TrotterPuppetResponse(phase int16) (TrotterStep, error) {
	return TrotterDay4Response(phase)
}

type TrotterRoute struct {
	Day      int16
	Clock    int16
	Page     string
	Resource uint32
	Code     string
}

type TrotterActionKind uint8

const (
	TrotterActionRunCode TrotterActionKind = iota + 1
	TrotterActionDialogue
	TrotterActionChoiceTable
	TrotterActionEventContinuation
	TrotterActionSallowerDispatch
)

type TrotterEffectKind uint8

const (
	TrotterEffectSetPhase TrotterEffectKind = iota + 1
	TrotterEffectMoveActor
	TrotterEffectSetScene
	TrotterEffectSetDirection
	TrotterEffectRunPuppet
	TrotterEffectMessage
	TrotterEffectAdjustCash
	TrotterEffectPlayerDeath
	TrotterEffectSelectHand
	TrotterEffectGiveInventory
	TrotterEffectSetCounter
	TrotterEffectAddInventory
	TrotterEffectSetStoryValue
	TrotterEffectHideActor
	TrotterEffectSetActorHeading
)

type TrotterEffect struct {
	Kind    TrotterEffectKind
	Target  string
	Value   string
	Phase   int16
	Amount  int32
	Heading int16
}

type TrotterOutcome struct {
	SetPhase      int16
	SetPhaseValid bool
	Finish        bool
}

type TrotterContinuationKind uint8

const (
	TrotterContinuationCondition TrotterContinuationKind = iota + 1
	TrotterContinuationNativeEvent
)

type TrotterContinuation struct {
	Kind          TrotterContinuationKind
	Condition     string
	Compare       int32
	OnMatch       TrotterOutcome
	Otherwise     TrotterOutcome
	Resource      uint32
	Code          string
	EventID       int32
	OriginPhase   int16
	AskedTown     bool
	ReturnsToGift bool
	Resume        *TrotterContinuation
	Flags         uint8
}

type TrotterUnsupportedError struct {
	Route string
}

func (e TrotterUnsupportedError) Error() string { return "unsupported Trotter route: " + e.Route }

type TrotterScriptController struct {
	puppetPrograms map[uint32]Program
	sallower       Program
}

type TrotterStoryState struct {
	Day               int16
	Clock             int16
	TrotterPhase      int16
	Phase             int16
	PlayerCash        int32
	HandChoice        *PuppetChoice
	Counter           int32
	TrotterActorValue int32
	BloodPhase        int16
	GunOwner          string
	SugarcubesOwner   string
	ThunderbirdOwner  string
	TrotterActorSet   string
	DocPhase          int16
}

type TrotterActorSetup struct {
	Set, Star, Pose                string
	Position                       [3]int16
	HasPosition                    bool
	Heading                        int16
	SetHeading                     bool
	Speed, TurnSpeed, Scale, ZClip int16
	HitBox                         [2]int16
	Visible, Idle, Delayed         bool
	Callback                       string
	LoopTicks                      int32
}

func NativeTrotterActorSetup(where string) (TrotterActorSetup, bool) {
	setup := TrotterActorSetup{Pose: "stand", Visible: true, ZClip: 32, HitBox: [2]int16{100, 50}}
	switch strings.ToLower(where) {
	case "bar":
		setup.Set, setup.Star, setup.Heading, setup.SetHeading = "sallower", "sal.trotter1", 0, true
	case "day4am":
		setup.Set, setup.Star, setup.Heading, setup.SetHeading, setup.Idle = "sallower", "sal.trotter9", 128, true, true
	case "inner":
		setup.Set, setup.Star, setup.Heading, setup.SetHeading, setup.Idle = "doctor2", "doctor2.trot", 175, true, true
	case "piss":
		setup.Set, setup.Star, setup.ZClip, setup.Idle = "town", "town.trot1", 0, true
	case "horse":
		setup.Set, setup.Star, setup.Heading, setup.SetHeading, setup.Idle = "town", "town.horse2", 12, true, true
	case "day2street":
		setup.Set, setup.Star, setup.Heading, setup.SetHeading, setup.Idle = "town", "town.jones5", 12, true, true
	case "delayed":
		setup.Set, setup.Star, setup.Delayed = "town", "town.jones1", true
	case "finale":
		setup.Set, setup.Position, setup.HasPosition, setup.Heading, setup.SetHeading = "town", [3]int16{1755, 3393, 0}, true, 138, true
	default:
		return TrotterActorSetup{}, false
	}
	switch setup.Set {
	case "town":
		setup.Speed, setup.TurnSpeed, setup.Scale = 3, 7, 1450
	case "sallower":
		setup.Speed, setup.TurnSpeed, setup.Scale = 4, 8, 4500
	case "doctor2":
		setup.Speed, setup.TurnSpeed, setup.Scale = 5, 10, 2400
	}
	if setup.Idle {
		setup.Callback, setup.LoopTicks = "trotteridle", 18
	}
	if setup.Delayed {
		setup.Callback, setup.LoopTicks = "delayloop", 10
	}
	return setup, true
}

type TrotterActorTransition struct {
	Setup             string
	Hide              bool
	Direction         string
	SetGamePhase      int16
	SetGamePhaseValid bool
}

func NativeTrotterSetTransition(state TrotterStoryState, set string, opening bool) TrotterActorTransition {
	transition := TrotterActorTransition{}
	switch strings.ToLower(set) {
	case "sallower":
		if opening {
			if state.Day == 1 && state.TrotterPhase < 6 && state.Phase < 7 || state.Day == 2 && state.Clock == 2 && state.TrotterPhase == 3 {
				transition.Setup = "bar"
			}
			if state.Day == 3 && state.Clock == 3 && state.Phase == 3 {
				transition.Hide, transition.SetGamePhase, transition.SetGamePhaseValid = true, 4, true
			}
			if state.Day == 4 {
				transition.Setup = "day4am"
			}
		} else {
			transition.Hide = state.Day == 4 || state.Clock > 1 && !(state.Day == 1 && state.Phase != 7) && strings.EqualFold(state.TrotterActorSet, "sallower")
		}
	case "doctor2":
		if opening && state.Day == 3 && state.Clock == 2 && state.DocPhase == 8 {
			transition.Setup, transition.Direction = "inner", "south"
		}
		if !opening && strings.EqualFold(state.TrotterActorSet, "doctor2") {
			transition.Hide = true
		}
	}
	return transition
}

type TrotterSallowerState struct {
	Day          int16
	Clock        int16
	TrotterPhase int16
	FlowersOwner string
}

const (
	trotterBootResource       uint32 = 7
	trotterDay1Resource       uint32 = 68
	trotterDay2Resource       uint32 = 76
	trotterDay3Resource       uint32 = 75
	trotterDay4Resource       uint32 = 74
	trotterPhaseGateResource  uint32 = 79
	sallowerOpenSceneResource uint32 = 48
)

var trotterBootRoutes = map[int16]TrotterRoute{
	1: {Day: 1, Page: "day1", Resource: trotterDay1Resource, Code: "runyoself"},
	2: {Day: 2, Page: "day2", Resource: trotterDay2Resource, Code: "runyoself"},
	3: {Day: 3, Page: "day3", Resource: trotterDay3Resource, Code: "runyoself"},
	4: {Day: 4, Page: "day4", Resource: trotterDay4Resource, Code: "runyoself"},
}

func NewTrotterScriptController(workspace assets.Workspace) (*TrotterScriptController, error) {
	puppetPrograms, err := loadTrotterPrograms(workspace, "PUPPETS/TROTTER.PUP", []uint32{trotterBootResource, trotterDay1Resource, trotterDay2Resource, trotterDay3Resource, trotterDay4Resource, trotterPhaseGateResource})
	if err != nil {
		return nil, err
	}
	sallowerPrograms, err := loadTrotterPrograms(workspace, "DATA/SALLOWER.SET", []uint32{sallowerOpenSceneResource})
	if err != nil {
		return nil, err
	}
	for _, route := range trotterBootRoutes {
		if _, err := FindCode(puppetPrograms[route.Resource], route.Code); err != nil {
			return nil, fmt.Errorf("TROTTER.PUP resource %d boot page %s: %w", route.Resource, route.Page, err)
		}
	}
	if _, err := FindCode(puppetPrograms[trotterBootResource], "runyoself"); err != nil {
		return nil, fmt.Errorf("TROTTER.PUP boot resource %d: %w", trotterBootResource, err)
	}
	if _, err := FindCode(puppetPrograms[trotterDay1Resource], "brushoff"); err != nil {
		return nil, fmt.Errorf("TROTTER.PUP day1 brushoff: %w", err)
	}
	if _, err := FindCode(sallowerPrograms[sallowerOpenSceneResource], "openscene"); err != nil {
		return nil, fmt.Errorf("SALLOWER.SET resource %d openscene: %w", sallowerOpenSceneResource, err)
	}
	return &TrotterScriptController{puppetPrograms: puppetPrograms, sallower: sallowerPrograms[sallowerOpenSceneResource]}, nil
}

func loadTrotterPrograms(workspace assets.Workspace, asset string, resources []uint32) (map[uint32]Program, error) {
	cache, err := workspace.OpenResourceCache(asset)
	if err != nil {
		return nil, err
	}
	defer cache.Close()
	header, err := cache.Header()
	if err != nil {
		return nil, err
	}
	programs := make(map[uint32]Program, len(resources))
	for _, resource := range resources {
		if resource >= header.CountB {
			return nil, fmt.Errorf("%s resource %d exceeds %d entries", asset, resource, header.CountB)
		}
		lease, err := cache.Acquire(resource)
		if err != nil {
			return nil, fmt.Errorf("read %s resource %d: %w", asset, resource, err)
		}
		data, readErr := lease.Bytes()
		closeErr := lease.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s resource %d: %w", asset, resource, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s resource %d: %w", asset, resource, closeErr)
		}
		program, err := ParseProgram(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s resource %d: %w", asset, resource, err)
		}
		programs[resource] = program
	}
	return programs, nil
}

func TrotterDay4Response(phase int16) (TrotterStep, error) {
	base := TrotterStep{Kind: TrotterActionDialogue, Route: trotterBootRoutes[4]}
	switch phase {
	case 0:
		base.Speech, base.SetTrotterPhase, base.SetTrotterPhaseValid = []string{"trotter.101", "trotter.102"}, 1, true
	case 1:
		base.Speech, base.SetTrotterPhase, base.SetTrotterPhaseValid = []string{"trotter.103", "trotter.104", "trotter.105"}, 2, true
	case 2:
		base.Speech, base.SetTrotterPhase, base.SetTrotterPhaseValid = []string{"trotter.106", "trotter.107"}, 3, true
	case 3:
		base.Speech = []string{"trotter.108"}
	default:
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("day4/runyoself phase %d", phase)}
	}
	base.Finish = true
	return base, nil
}

func TrotterDay1Response(phase int16) (TrotterStep, error) {
	base := TrotterStep{Kind: TrotterActionDialogue, Route: trotterBootRoutes[1]}
	switch phase {
	case 0:
		base.Speech, base.SetTrotterPhase, base.SetTrotterPhaseValid = []string{"trotter.13"}, 1, true
	case 1:
		base.Speech, base.SetTrotterPhase, base.SetTrotterPhaseValid = []string{"trotter.14", "trotter.15"}, 2, true
	case 2:
		base.Speech = []string{"trotter.16", "trotter.17"}
		base.Continuation = &TrotterContinuation{Kind: TrotterContinuationCondition, Condition: "mightdie", Compare: 1, OnMatch: TrotterOutcome{Finish: true}, Otherwise: TrotterOutcome{SetPhase: 4, SetPhaseValid: true}}
	case 3:
		base.Continuation = &TrotterContinuation{Kind: TrotterContinuationCondition, Condition: "alternate", Compare: 1, OnMatch: TrotterOutcome{Finish: true}, Otherwise: TrotterOutcome{SetPhase: 4, SetPhaseValid: true}}
	case 4:
		base.Speech, base.SetTrotterPhase, base.SetTrotterPhaseValid = []string{"trotter.18", "trotter.19"}, 5, true
	case 5:
		base.Speech, base.SetTrotterPhase, base.SetTrotterPhaseValid = []string{"trotter.20", "trotter.21"}, 6, true
	case 6:
		base.Speech, base.SetTrotterPhase, base.SetTrotterPhaseValid = []string{"trotter.22", "trotter.23"}, 7, true
		base.Effects = []TrotterEffect{{Kind: TrotterEffectMoveActor, Target: "TROTTER", Value: "sal.trotter2"}, {Kind: TrotterEffectSetScene, Value: "d3"}, {Kind: TrotterEffectSetDirection, Value: "west"}}
	case 7:
	default:
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("day1/runyoself phase %d", phase)}
	}
	base.Finish = base.Continuation == nil
	return base, nil
}

func (c *TrotterScriptController) BeginDay1(state TrotterStoryState) (TrotterStep, error) {
	if c == nil || c.puppetPrograms[trotterDay1Resource].Records == nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP day1 resource is unavailable")
	}
	if state.Phase > 6 {
		return TrotterStep{Kind: TrotterActionDialogue, Route: trotterBootRoutes[1], Speech: []string{"trotter.11", "trotter.12"}, Finish: true}, nil
	}
	step, err := TrotterDay1Response(state.TrotterPhase)
	if err != nil || step.Continuation == nil {
		return step, err
	}
	code := step.Continuation.Condition
	choiceStep, err := c.day1Choices(state, code, state.TrotterPhase, false)
	if err != nil {
		return TrotterStep{}, err
	}
	choiceStep.Speech = step.Speech
	return choiceStep, nil
}

func (c *TrotterScriptController) day1Choices(state TrotterStoryState, code string, originPhase int16, askedTown bool) (TrotterStep, error) {
	choices, err := PuppetBevelChoices(c.puppetPrograms[trotterDay1Resource], code)
	if err != nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP day1 %s choices: %w", code, err)
	}
	if code == "mainloop" {
		filtered := make([]PuppetChoice, 0, len(choices)+1)
		for _, choice := range choices {
			if askedTown && choice.EventID == 101 {
				continue
			}
			if choice.EventID == 104 && state.HandChoice != nil {
				filtered = append(filtered, *state.HandChoice)
			}
			filtered = append(filtered, choice)
		}
		choices = filtered
	}
	route := trotterBootRoutes[1]
	route.Code = code
	return TrotterStep{Kind: TrotterActionDialogue, Route: route, Choices: [][]PuppetChoice{choices}, ScrambleChoices: code == "mightdie", Continuation: &TrotterContinuation{Kind: TrotterContinuationNativeEvent, Resource: trotterDay1Resource, Code: code, OriginPhase: originPhase, AskedTown: askedTown}}, nil
}

func (c *TrotterScriptController) ContinueDay1(state TrotterStoryState, continuation *TrotterContinuation, eventID int32) (TrotterStep, error) {
	if c == nil || c.puppetPrograms[trotterDay1Resource].Records == nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP day1 resource is unavailable")
	}
	if continuation == nil || continuation.Kind != TrotterContinuationNativeEvent || continuation.Resource != trotterDay1Resource || (!continuation.ReturnsToGift && continuation.OriginPhase != 2 && continuation.OriginPhase != 3) {
		return TrotterStep{}, TrotterUnsupportedError{Route: "day1 continuation"}
	}
	code := continuation.Code
	if code != "mightdie" && code != "alternate" && code != "greet" && code != "mainloop" {
		return TrotterStep{}, TrotterUnsupportedError{Route: "day1 continuation " + code}
	}
	step := TrotterStep{Kind: TrotterActionDialogue, Route: trotterBootRoutes[1], EventID: eventID, Finish: true}
	step.Route.Code = code
	if eventID == -1 {
		return c.finishDay1Call(state, continuation, step, false)
	}
	choices, err := c.day1Choices(state, code, continuation.OriginPhase, continuation.AskedTown)
	if err != nil {
		return TrotterStep{}, err
	}
	found := false
	for _, choice := range choices.Choices[0] {
		if choice.EventID == eventID {
			found = true
			break
		}
	}
	if !found {
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("day1/%s event %d", code, eventID)}
	}
	if code == "mainloop" && eventID == 55555 {
		choices.EventID = eventID
		choices.Effects = []TrotterEffect{{Kind: TrotterEffectSelectHand}}
		choices.Continuation.ReturnsToGift, choices.Continuation.Resume = continuation.ReturnsToGift, continuation.Resume
		return choices, nil
	}
	step.Speech, err = PuppetEventSpeechCalls(c.puppetPrograms[trotterDay1Resource], code, eventID)
	if err != nil {
		return TrotterStep{}, err
	}
	nextCode, askedTown := "", continuation.AskedTown
	switch code {
	case "mightdie":
		if eventID == 102 {
			step.Effects = []TrotterEffect{{Kind: TrotterEffectPlayerDeath, Value: "by trotter"}}
		} else {
			nextCode = "greet"
		}
	case "alternate":
		nextCode = "greet"
	case "greet":
		nextCode = "mainloop"
	case "mainloop":
		if eventID == 101 {
			nextCode, askedTown = "mainloop", true
		}
	}
	if (code == "mightdie" || code == "alternate") && eventID == 101 && state.PlayerCash > 0 {
		step.Effects = append(step.Effects, TrotterEffect{Kind: TrotterEffectAdjustCash, Amount: -1})
	}
	if nextCode == "" {
		return c.finishDay1Call(state, continuation, step, true)
	}
	choices, err = c.day1Choices(state, nextCode, continuation.OriginPhase, askedTown)
	if err != nil {
		return TrotterStep{}, err
	}
	choices.Speech, choices.Effects, choices.EventID = step.Speech, step.Effects, eventID
	choices.Continuation.ReturnsToGift, choices.Continuation.Resume = continuation.ReturnsToGift, continuation.Resume
	return choices, nil
}

func (c *TrotterScriptController) ResumeDay1(state TrotterStoryState, continuation *TrotterContinuation) (TrotterStep, error) {
	if c == nil || continuation == nil || continuation.Kind != TrotterContinuationNativeEvent || continuation.Resource != trotterDay1Resource {
		return TrotterStep{}, TrotterUnsupportedError{Route: "day1 resume"}
	}
	step, err := c.day1Choices(state, continuation.Code, continuation.OriginPhase, continuation.AskedTown)
	if err != nil {
		return TrotterStep{}, err
	}
	step.Continuation.ReturnsToGift, step.Continuation.Resume = continuation.ReturnsToGift, continuation.Resume
	return step, nil
}

func (c *TrotterScriptController) finishDay1Call(state TrotterStoryState, continuation *TrotterContinuation, step TrotterStep, completed bool) (TrotterStep, error) {
	if continuation.ReturnsToGift {
		if continuation.Resume == nil {
			return step, nil
		}
		resumed, err := c.Resume(state, continuation.Resume)
		if err != nil {
			return TrotterStep{}, err
		}
		resumed.Speech, resumed.Effects, resumed.EventID = step.Speech, step.Effects, step.EventID
		return resumed, nil
	}
	if completed {
		step.SetTrotterPhase, step.SetTrotterPhaseValid = 4, true
	}
	return step, nil
}

func (c *TrotterScriptController) BeginGift(state TrotterStoryState, what string, resume *TrotterContinuation) (TrotterStep, error) {
	if c == nil || c.puppetPrograms[trotterBootResource].Records == nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP boot resource is unavailable")
	}
	step := TrotterStep{Kind: TrotterActionDialogue, Route: TrotterRoute{Day: state.Day, Clock: state.Clock, Page: "boot script", Resource: trotterBootResource, Code: "gift"}, Finish: true}
	switch strings.ToLower(what) {
	case "cigar":
		step.Speech = []string{"trotter.1"}
		step.Effects = []TrotterEffect{{Kind: TrotterEffectGiveInventory, Target: "TROTTER", Value: what}}
		if state.TrotterPhase < 4 && state.Day == 1 && state.Phase < 7 {
			alternate, err := c.day1Choices(state, "alternate", state.TrotterPhase, false)
			if err != nil {
				return TrotterStep{}, err
			}
			alternate.Speech, alternate.Effects = step.Speech, step.Effects
			alternate.Continuation.ReturnsToGift, alternate.Continuation.Resume = true, resume
			return alternate, nil
		}
	case "jug":
		step.Speech = []string{"trotter.2"}
	case "sugarcubes":
		step.Speech = []string{"trotter.3", "trotter.5"}
		if state.Clock == 2 && state.Day == 2 && state.TrotterPhase == 3 {
			step.Speech = []string{"trotter.3", "trotter.4"}
			step.SetTrotterPhase, step.SetTrotterPhaseValid = 5, true
			step.Effects = []TrotterEffect{{Kind: TrotterEffectMoveActor, Target: "TROTTER", Value: "sal.trotter2"}, {Kind: TrotterEffectGiveInventory, Target: "TROTTER", Value: what}}
		}
	case "tstone", "blade", "mask", "flute", "tbird":
		step.Speech = []string{"trotter.6"}
	default:
		switch state.Counter {
		case 0:
			step.Speech, step.Effects = []string{"trotter.7", "trotter.8"}, []TrotterEffect{{Kind: TrotterEffectSetCounter, Amount: 1}}
		case 1:
			step.Speech, step.Effects = []string{"trotter.9"}, []TrotterEffect{{Kind: TrotterEffectSetCounter, Amount: 2}}
		case 2:
			step.Speech, step.Effects = []string{"trotter.8", "trotter.10"}, []TrotterEffect{{Kind: TrotterEffectSetCounter, Amount: 0}}
		}
	}
	if resume != nil {
		if step.SetTrotterPhaseValid && step.SetTrotterPhase == 5 && resume.Resource == trotterDay2Resource && resume.Code == "hesdrunk" {
			return step, nil
		}
		resumed, err := c.Resume(state, resume)
		if err != nil {
			return TrotterStep{}, err
		}
		resumed.Speech, resumed.Effects = step.Speech, step.Effects
		resumed.SetTrotterPhase, resumed.SetTrotterPhaseValid = step.SetTrotterPhase, step.SetTrotterPhaseValid
		return resumed, nil
	}
	return step, nil
}

func (c *TrotterScriptController) laterChoices(state TrotterStoryState, resource uint32, code string, flags uint8, resume *TrotterContinuation) (TrotterStep, error) {
	if c == nil || c.puppetPrograms[resource].Records == nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP resource %d is unavailable", resource)
	}
	choices, err := PuppetBevelChoices(c.puppetPrograms[resource], code)
	if err != nil {
		return TrotterStep{}, err
	}
	filtered := make([]PuppetChoice, 0, len(choices)+1)
	for _, choice := range choices {
		if code == "day2am" {
			if choice.EventID == 101 && flags&1 != 0 || choice.EventID == 102 && flags&2 != 0 || choice.EventID == 500 && (flags&4 != 0 || strings.EqualFold(state.GunOwner, "stranger")) {
				continue
			}
		} else if code == "twonite" || code == "threenite" {
			if choice.EventID == 101 && flags&1 != 0 || choice.EventID == 102 && flags&2 != 0 {
				continue
			}
		}
		if choice.EventID == 103 && (code == "day2am" || code == "hesdrunk" || code == "twonite" || code == "threenite") && state.HandChoice != nil && len(filtered) <= 3 {
			filtered = append(filtered, *state.HandChoice)
		}
		filtered = append(filtered, choice)
	}
	page := "day2"
	if resource == trotterDay3Resource {
		page = "day3"
	}
	step := TrotterStep{Kind: TrotterActionDialogue, Route: TrotterRoute{Day: state.Day, Clock: state.Clock, Page: page, Resource: resource, Code: code}, Choices: [][]PuppetChoice{filtered}, Continuation: &TrotterContinuation{Kind: TrotterContinuationNativeEvent, Resource: resource, Code: code, Flags: flags, Resume: resume}}
	if code == "threepm" {
		step.ChoiceTimeoutFrames = 240
	}
	return step, nil
}

func (c *TrotterScriptController) Resume(state TrotterStoryState, continuation *TrotterContinuation) (TrotterStep, error) {
	if continuation == nil || continuation.Kind != TrotterContinuationNativeEvent {
		return TrotterStep{}, TrotterUnsupportedError{Route: "resume"}
	}
	if continuation.Resource == trotterDay1Resource {
		return c.ResumeDay1(state, continuation)
	}
	if continuation.Resource != trotterDay2Resource && continuation.Resource != trotterDay3Resource {
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("resume resource%d", continuation.Resource)}
	}
	if continuation.Resource == trotterDay2Resource && continuation.Code == "hesdrunk" && continuation.EventID == 55555 && strings.EqualFold(state.SugarcubesOwner, "TROTTER") {
		return TrotterStep{Kind: TrotterActionDialogue, Route: TrotterRoute{Day: state.Day, Clock: state.Clock, Page: "day2", Resource: continuation.Resource, Code: continuation.Code}, Finish: true}, nil
	}
	return c.laterChoices(state, continuation.Resource, continuation.Code, continuation.Flags, continuation.Resume)
}

func (c *TrotterScriptController) Brushoff(state TrotterStoryState) (TrotterStep, error) {
	step := TrotterStep{Kind: TrotterActionDialogue, Route: TrotterRoute{Day: state.Day, Clock: state.Clock, Page: "day1", Resource: trotterDay1Resource, Code: "brushoff"}, Finish: true}
	if c == nil || c.puppetPrograms[trotterDay1Resource].Records == nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP day1 resource is unavailable")
	}
	if state.Counter < 0 || state.Counter > 2 {
		return step, nil
	}
	var err error
	step.Speech, err = PuppetEventSpeechCalls(c.puppetPrograms[trotterDay1Resource], "brushoff", state.Counter)
	if err != nil {
		return TrotterStep{}, err
	}
	step.Effects = []TrotterEffect{{Kind: TrotterEffectSetCounter, Amount: (state.Counter + 1) % 3}}
	return step, nil
}

func (c *TrotterScriptController) BeginDay2(state TrotterStoryState) (TrotterStep, error) {
	if c == nil || c.puppetPrograms[trotterDay2Resource].Records == nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP day2 resource is unavailable")
	}
	step := TrotterStep{Kind: TrotterActionDialogue, Route: TrotterRoute{Day: state.Day, Clock: state.Clock, Page: "day2", Resource: trotterDay2Resource, Code: "runyoself"}, Finish: true}
	if state.TrotterPhase == 1 {
		return c.Brushoff(state)
	}
	var err error
	switch state.Clock {
	case 1:
		step, err = c.laterChoices(state, trotterDay2Resource, "day2am", 0, nil)
		if err != nil {
			return TrotterStep{}, err
		}
		step.Speech = []string{"trotter.43", "trotter.46", "trotter.26"}
		if state.TrotterActorValue > 0 {
			step.Speech = []string{"trotter.43", "trotter.44", "trotter.45"}
			step.SpeechStages = []TrotterSpeechStage{{Speech: []string{"trotter.43", "trotter.44"}}, {Speech: []string{"trotter.45"}, DelayBefore: 120}}
		}
	case 2:
		switch state.TrotterPhase {
		case 0:
			step, err = c.laterChoices(state, trotterDay2Resource, "inruby", 0, nil)
			step.Speech = []string{"trotter.62"}
		case 3:
			step, err = c.laterChoices(state, trotterDay2Resource, "hesdrunk", 0, nil)
			step.Speech = []string{"trotter.59"}
		case 4:
			step.Speech = []string{"trotter.54", "trotter.55", "trotter.56", "trotter.57"}
			step.Effects = []TrotterEffect{{Kind: TrotterEffectPlayerDeath, Value: "by trotter"}}
		case 5:
			step.Speech = []string{"trotter.58"}
		}
	case 3:
		step, err = c.laterChoices(state, trotterDay2Resource, "twonite", 0, nil)
		step.Speech = []string{"trotter.65", "trotter.66", "trotter.65"}
	default:
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("day2 clock%d", state.Clock)}
	}
	return step, err
}

func (c *TrotterScriptController) BeginDay3(state TrotterStoryState) (TrotterStep, error) {
	if c == nil || c.puppetPrograms[trotterDay3Resource].Records == nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP day3 resource is unavailable")
	}
	if state.TrotterPhase == 1 {
		return c.Brushoff(state)
	}
	var step TrotterStep
	var err error
	switch state.Clock {
	case 1:
		return TrotterStep{}, TrotterUnsupportedError{Route: "day3 clock1 runs native error handler"}
	case 2:
		step, err = c.laterChoices(state, trotterDay3Resource, "threepm", 0, nil)
		step.Speech = []string{"trotter.77", "trotter.78", "trotter.79", "trotter.80", "trotter.81"}
		step.Effects = []TrotterEffect{{Kind: TrotterEffectAddInventory, Value: "flute"}, {Kind: TrotterEffectSetStoryValue, Target: "docphase", Amount: 9}}
	case 3:
		step, err = c.laterChoices(state, trotterDay3Resource, "threenite", 0, nil)
		step.Speech = []string{"trotter.43", "trotter.84", "trotter.85", "trotter.26"}
	default:
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("day3 clock%d", state.Clock)}
	}
	return step, err
}

func (c *TrotterScriptController) Continue(state TrotterStoryState, continuation *TrotterContinuation, eventID int32) (TrotterStep, error) {
	if continuation == nil || continuation.Kind != TrotterContinuationNativeEvent {
		return TrotterStep{}, TrotterUnsupportedError{Route: "continuation"}
	}
	if continuation.Resource == trotterDay1Resource {
		return c.ContinueDay1(state, continuation, eventID)
	}
	resource, code := continuation.Resource, continuation.Code
	if resource != trotterDay2Resource && resource != trotterDay3Resource {
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("continuation resource%d", resource)}
	}
	step, err := c.laterChoices(state, resource, code, continuation.Flags, continuation.Resume)
	if err != nil {
		return TrotterStep{}, err
	}
	step.EventID = eventID
	if code == "threepm" && (eventID == -1 || eventID == -2 || eventID == 101) {
		step.Speech = []string{"trotter.82", "trotter.83"}
		step.Effects = []TrotterEffect{{Kind: TrotterEffectHideActor, Target: "TROTTER"}}
		step.Choices, step.Continuation, step.ChoiceTimeoutFrames = nil, nil, 0
		step.Finish, step.SetTrotterPhase, step.SetTrotterPhaseValid = true, 1, true
		return step, nil
	}
	if eventID == -1 {
		step.Choices, step.Continuation = nil, nil
		step.Finish = true
		return step, nil
	}
	if len(step.Choices) == 0 {
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("%s event%d", code, eventID)}
	}
	found := false
	for _, choice := range step.Choices[0] {
		if choice.EventID == eventID {
			found = true
			break
		}
	}
	if !found {
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("%s event%d", code, eventID)}
	}
	if eventID == 55555 {
		step.Continuation.EventID = eventID
		step.Effects = []TrotterEffect{{Kind: TrotterEffectSelectHand}}
		return step, nil
	}
	step.Speech, err = PuppetEventSpeechCalls(c.puppetPrograms[resource], code, eventID)
	if err != nil {
		return TrotterStep{}, err
	}
	finish, setPhase := false, false
	switch code {
	case "day2am":
		switch eventID {
		case 101:
			step.Continuation.Flags |= 1
			if state.BloodPhase == 1 {
				step.Speech = []string{"trotter.47"}
			}
		case 102:
			step.Continuation.Flags |= 2
			step.SpeechStages = []TrotterSpeechStage{{Speech: []string{"trotter.50"}}, {Speech: []string{"trotter.51"}, DelayBefore: 90}, {Speech: []string{"trotter.52"}, DelayBefore: 60}}
		case 500:
			step.Continuation.Flags |= 4
		case 103:
			finish, setPhase = true, true
		}
	case "inruby":
		finish, setPhase = true, true
	case "hesdrunk":
		finish = true
	case "twonite":
		switch eventID {
		case 101:
			step.Continuation.Flags |= 1
			outer := *step.Continuation
			drinks, err := c.laterChoices(state, resource, "drinks", 0, &outer)
			if err != nil {
				return TrotterStep{}, err
			}
			drinks.Speech, drinks.EventID = step.Speech, eventID
			return drinks, nil
		case 102:
			step.Continuation.Flags |= 2
		case 103:
			finish, setPhase = true, true
		}
	case "drinks":
		if continuation.Resume == nil {
			return TrotterStep{}, TrotterUnsupportedError{Route: "drinks without outer loop"}
		}
		resumed, err := c.Resume(state, continuation.Resume)
		if err != nil {
			return TrotterStep{}, err
		}
		resumed.Speech, resumed.EventID = step.Speech, eventID
		return resumed, nil
	case "threenite":
		switch eventID {
		case 101:
			step.Continuation.Flags |= 1
		case 102:
			step.Continuation.Flags |= 2
		case 103:
			if strings.EqualFold(state.ThunderbirdOwner, "stranger") {
				step.Speech = []string{"trotter.92", "trotter.93"}
			}
			step.Effects = []TrotterEffect{{Kind: TrotterEffectSetActorHeading, Target: "horse1", Heading: 0}, {Kind: TrotterEffectSetPhase, Phase: 1}, {Kind: TrotterEffectSetStoryValue, Target: "fighton", Amount: 0}}
			finish, setPhase = true, true
		}
	default:
		return TrotterStep{}, TrotterUnsupportedError{Route: code}
	}
	if finish {
		step.Choices, step.Continuation = nil, nil
		step.Finish = true
		step.SetTrotterPhase, step.SetTrotterPhaseValid = 1, setPhase
		return step, nil
	}
	resumed, err := c.Resume(state, step.Continuation)
	if err != nil {
		return TrotterStep{}, err
	}
	resumed.Speech, resumed.SpeechStages, resumed.EventID = step.Speech, step.SpeechStages, eventID
	return resumed, nil
}

func (c *TrotterScriptController) Boot(day int16) (TrotterStep, error) {
	route, ok := trotterBootRoutes[day]
	if !ok {
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("boot day %d", day)}
	}
	if c == nil || c.puppetPrograms[trotterBootResource].Records == nil {
		return TrotterStep{}, fmt.Errorf("Trotter script controller has no boot resource")
	}
	return TrotterStep{Kind: TrotterActionRunCode, Route: route}, nil
}

func (c *TrotterScriptController) Dispatch(state TrotterStoryState) (TrotterStep, error) {
	if c == nil {
		return TrotterStep{}, fmt.Errorf("Trotter script controller is nil")
	}
	switch state.Day {
	case 1:
		return c.BeginDay1(state)
	case 2:
		return c.BeginDay2(state)
	case 3:
		return c.BeginDay3(state)
	case 4:
		return TrotterDay4Response(state.TrotterPhase)
	}
	return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("day %d clock %d phase %d", state.Day, state.Clock, state.TrotterPhase)}
}

func (c *TrotterScriptController) runCode(route TrotterRoute, code string) (TrotterStep, error) {
	program, ok := c.puppetPrograms[route.Resource]
	if !ok {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP resource %d is unavailable", route.Resource)
	}
	if _, err := FindCode(program, code); err != nil {
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("%s/%s", route.Page, code)}
	}
	route.Code = code
	return TrotterStep{Kind: TrotterActionRunCode, Route: route}, nil
}

func (c *TrotterScriptController) ChoiceTable(day, clock int16) (TrotterStep, error) {
	var route TrotterRoute
	switch {
	case day == 2 && clock == 1:
		route = TrotterRoute{Day: day, Clock: clock, Page: "day2", Resource: trotterDay2Resource, Code: "day2am"}
	case day == 3 && clock == 2:
		route = TrotterRoute{Day: day, Clock: clock, Page: "day3", Resource: trotterDay3Resource, Code: "threepm"}
	default:
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("choice table day %d clock %d", day, clock)}
	}
	if c == nil {
		return TrotterStep{}, fmt.Errorf("Trotter script controller is nil")
	}
	program := c.puppetPrograms[route.Resource]
	groups, err := PuppetBevelChoiceGroups(program, route.Code)
	if err != nil {
		return TrotterStep{}, fmt.Errorf("read TROTTER.PUP choice table %s: %w", route.Code, err)
	}
	return TrotterStep{Kind: TrotterActionChoiceTable, Route: route, Choices: groups}, nil
}

func (c *TrotterScriptController) ContinueChoice(day, clock int16, eventID int32) (TrotterStep, error) {
	table, err := c.ChoiceTable(day, clock)
	if err != nil {
		return TrotterStep{}, err
	}
	for _, group := range table.Choices {
		for _, choice := range group {
			if choice.EventID == eventID {
				table.Kind, table.EventID = TrotterActionEventContinuation, eventID
				table.Choices = nil
				table.Continuation = &TrotterContinuation{Kind: TrotterContinuationNativeEvent, Resource: table.Route.Resource, Code: table.Route.Code, EventID: eventID}
				return table, nil
			}
		}
	}
	return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("%s event %d", table.Route.Code, eventID)}
}

func (c *TrotterScriptController) Resource79Phase(phase int16) (TrotterStep, error) {
	if c == nil || c.puppetPrograms[trotterPhaseGateResource].Records == nil {
		return TrotterStep{}, fmt.Errorf("TROTTER.PUP resource %d is unavailable", trotterPhaseGateResource)
	}
	step := TrotterStep{Kind: TrotterActionDialogue, Route: TrotterRoute{Page: "resource79", Resource: trotterPhaseGateResource, Code: "runyoself"}, Finish: true}
	if phase == 1 {
		step.Speech = []string{"trotter.112"}
		return step, nil
	}
	step.Speech, step.SetTrotterPhase, step.SetTrotterPhaseValid = []string{"trotter.109", "trotter.110", "trotter.111"}, 1, true
	return step, nil
}

func (c *TrotterScriptController) SallowerFlowerDeath(state TrotterSallowerState) (TrotterStep, error) {
	if c == nil || c.sallower.Records == nil {
		return TrotterStep{}, fmt.Errorf("SALLOWER.SET resource %d is unavailable", sallowerOpenSceneResource)
	}
	if state.Day != 2 || state.Clock != 2 || state.TrotterPhase != 3 || !strings.EqualFold(state.FlowersOwner, "limbo2") {
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("SALLOWER openscene day %d clock %d phase %d flowers=%s", state.Day, state.Clock, state.TrotterPhase, state.FlowersOwner)}
	}
	return TrotterStep{Kind: TrotterActionSallowerDispatch, Route: TrotterRoute{Day: 2, Clock: 2, Page: "sallower", Resource: sallowerOpenSceneResource, Code: "openscene"}, SetTrotterPhase: 4, SetTrotterPhaseValid: true, Finish: true, Effects: []TrotterEffect{{Kind: TrotterEffectSetPhase, Phase: 4}, {Kind: TrotterEffectRunPuppet, Target: "gang", Value: "trotter.pup"}, {Kind: TrotterEffectMessage, Value: "dead"}}}, nil
}
