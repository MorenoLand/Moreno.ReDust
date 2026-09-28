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
)

type TrotterEffect struct {
	Kind   TrotterEffectKind
	Target string
	Value  string
	Phase  int16
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
	Kind      TrotterContinuationKind
	Condition string
	Compare   int32
	OnMatch   TrotterOutcome
	Otherwise TrotterOutcome
	Resource  uint32
	Code      string
	EventID   int32
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
	Day          int16
	Clock        int16
	TrotterPhase int16
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
	if phase > 6 {
		base.Speech, base.Finish = []string{"trotter.11", "trotter.12"}, true
		return base, nil
	}
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
	default:
		return TrotterStep{}, TrotterUnsupportedError{Route: fmt.Sprintf("day1/runyoself phase %d", phase)}
	}
	base.Finish = base.Continuation == nil
	return base, nil
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
		return TrotterDay1Response(state.TrotterPhase)
	case 2:
		switch state.Clock {
		case 1:
			if state.TrotterPhase == 1 {
				return c.runCode(trotterBootRoutes[1], "brushoff")
			}
			return c.runCode(TrotterRoute{Day: 2, Clock: 1, Page: "day2", Resource: trotterDay2Resource, Code: "day2am"}, "day2am")
		case 2:
			return c.runCode(TrotterRoute{Day: 2, Clock: 2, Page: "day2", Resource: trotterDay2Resource, Code: "day2pm"}, "day2pm")
		case 3:
			return c.runCode(TrotterRoute{Day: 2, Clock: 3, Page: "day2", Resource: trotterDay2Resource, Code: "twonite"}, "twonite")
		}
	case 3:
		if state.TrotterPhase == 1 {
			return c.runCode(trotterBootRoutes[1], "brushoff")
		}
		switch state.Clock {
		case 2:
			return c.runCode(TrotterRoute{Day: 3, Clock: 2, Page: "day3", Resource: trotterDay3Resource, Code: "threepm"}, "threepm")
		case 3:
			return c.runCode(TrotterRoute{Day: 3, Clock: 3, Page: "day3", Resource: trotterDay3Resource, Code: "threenite"}, "threenite")
		case 1:
			return TrotterStep{}, TrotterUnsupportedError{Route: "day3 clock1 runs native error handler"}
		}
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
