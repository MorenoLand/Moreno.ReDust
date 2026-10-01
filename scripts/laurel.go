package scripts

import (
	"fmt"
	"strings"

	"redust/assets"
)

type LaurelStoryState struct {
	Day, Clock, LaurelPhase, LaurelGood, HankerchiefDegree int16
	LaurelActorValue, Counter                              int32
	JonesOwner, HankerchiefOwner                           string
	HandChoice                                             *PuppetChoice
}

type LaurelEffectKind uint8

const (
	LaurelEffectSetStoryValue LaurelEffectKind = iota + 1
	LaurelEffectAddInventory
	LaurelEffectGiveInventory
	LaurelEffectMoveActor
	LaurelEffectHideActor
	LaurelEffectPlayerDeath
	LaurelEffectSelectHand
)

type LaurelEffect struct {
	Kind          LaurelEffectKind
	Target, Value string
	Amount        int32
}

type LaurelContinuation struct {
	Code         string
	Stage, Flags uint8
	Resume       *LaurelContinuation
}

type LaurelStep struct {
	Speech              []string
	Choices             []PuppetChoice
	Continuation        *LaurelContinuation
	Effects             []LaurelEffect
	Finish              bool
	ChoiceTimeoutFrames uint32
}

type LaurelScriptController struct {
	programs map[uint32]Program
}

type LaurelActorSetup struct {
	Set, Star, Pose, WalkStar, Callback     string
	Position                                [3]int16
	HasPosition, SetHeading, Visible        bool
	Heading, Speed, TurnSpeed, Scale, ZClip int16
	HitBox                                  [2]int16
	LoopTicks                               int32
}

func NativeLaurelActorSetup(where string) (LaurelActorSetup, bool) {
	setup := LaurelActorSetup{Pose: "stand", Visible: true, ZClip: 32, HitBox: [2]int16{100, 50}}
	switch strings.ToLower(where) {
	case "hotel":
		setup.Set, setup.Star, setup.Scale, setup.Speed, setup.TurnSpeed, setup.Callback, setup.LoopTicks = "hotlower", "hotlower.laurel", 3220, 4, 8, "laurelidle", 18
	case "day3am":
		setup.Set, setup.Star, setup.WalkStar, setup.Scale, setup.Speed, setup.TurnSpeed = "town", "town.mwife1", "town.mwife2", 1250, 3, 7
	case "day3nite":
		setup.Set, setup.Star, setup.Scale, setup.Speed, setup.TurnSpeed, setup.Callback, setup.LoopTicks = "town", "town.jones1", 1250, 3, 7, "laurelidle", 18
	case "finale":
		setup.Set, setup.Position, setup.HasPosition, setup.Heading, setup.SetHeading, setup.Scale, setup.Speed, setup.TurnSpeed = "town", [3]int16{1835, 1284, 5}, true, 158, true, 1450, 3, 7
	default:
		return LaurelActorSetup{}, false
	}
	return setup, true
}

func NewLaurelScriptController(workspace assets.Workspace) (*LaurelScriptController, error) {
	programs, err := loadTrotterPrograms(workspace, "PUPPETS/LAUREL.PUP", []uint32{7, 36, 53})
	if err != nil {
		return nil, err
	}
	for resource, codes := range map[uint32][]string{7: {"gift"}, 36: {"brushoff"}, 53: {"runyoself", "breakfast", "apologize", "silicon", "bye"}} {
		for _, code := range codes {
			if _, err := FindCode(programs[resource], code); err != nil {
				return nil, err
			}
		}
	}
	return &LaurelScriptController{programs: programs}, nil
}

func (c *LaurelScriptController) choices(state LaurelStoryState, continuation LaurelContinuation) (LaurelStep, error) {
	if c == nil || c.programs[53].Records == nil {
		return LaurelStep{}, fmt.Errorf("LAUREL.PUP day2 resource is unavailable")
	}
	groups, err := PuppetBevelChoiceGroups(c.programs[53], continuation.Code)
	if err != nil {
		return LaurelStep{}, err
	}
	if int(continuation.Stage) >= len(groups) {
		return LaurelStep{}, fmt.Errorf("Laurel %s stage%d is unavailable", continuation.Code, continuation.Stage)
	}
	choices := append([]PuppetChoice(nil), groups[continuation.Stage]...)
	if continuation.Code == "breakfast" {
		if continuation.Stage == 0 {
			if len(choices) != 2 {
				return LaurelStep{}, fmt.Errorf("Laurel breakfast has %d conditional greetings, want2", len(choices))
			}
			index := 0
			if state.LaurelActorValue > 0 {
				index = 1
			}
			choices = choices[index : index+1]
		} else {
			filtered := make([]PuppetChoice, 0, len(choices)+1)
			for _, choice := range choices {
				if (choice.EventID == 102 || choice.EventID == 103) && (!strings.EqualFold(state.JonesOwner, "message") || continuation.Flags&1 != 0) || choice.EventID == 201 && continuation.Flags&2 != 0 {
					continue
				}
				if choice.EventID == 301 && state.HandChoice != nil && len(filtered) <= 3 {
					filtered = append(filtered, *state.HandChoice)
				}
				filtered = append(filtered, choice)
			}
			choices = filtered
		}
	}
	step := LaurelStep{Choices: choices, Continuation: &continuation}
	if continuation.Code == "bye" || continuation.Code == "silicon" && continuation.Stage == 0 {
		step.ChoiceTimeoutFrames = 240
	}
	return step, nil
}

func (c *LaurelScriptController) Resume(state LaurelStoryState, continuation *LaurelContinuation) (LaurelStep, error) {
	if continuation == nil {
		return LaurelStep{}, fmt.Errorf("Laurel continuation is unavailable")
	}
	return c.choices(state, *continuation)
}

func (c *LaurelScriptController) BeginBreakfast(state LaurelStoryState, random *NativeRandom) (LaurelStep, error) {
	if c == nil || c.programs[53].Records == nil {
		return LaurelStep{}, fmt.Errorf("LAUREL.PUP day2 resource is unavailable")
	}
	if state.Day != 2 || state.Clock != 1 {
		return LaurelStep{}, fmt.Errorf("Laurel breakfast route requires day2 clock1, gotday%d clock%d", state.Day, state.Clock)
	}
	if state.LaurelPhase == 1 {
		if random == nil && state.LaurelGood >= -1 && state.LaurelGood <= 1 {
			return LaurelStep{}, fmt.Errorf("Laurel brushoff requires native random state")
		}
		pick := uint32(0)
		if state.LaurelGood >= -1 && state.LaurelGood <= 1 {
			pick = random.Inclusive(2)
		}
		step := LaurelStep{Finish: true, Effects: []LaurelEffect{{Kind: LaurelEffectHideActor, Target: "laurel"}}}
		switch state.LaurelGood {
		case -1:
			step.Speech = []string{"laurel.24"}
			if pick == 1 {
				step.Speech = []string{"laurel.42", "laurel.43", "laurel.44"}
			}
		case 0:
			step.Speech = []string{"laurel.47"}
			if pick == 1 {
				step.Speech = []string{"laurel.45", "laurel.46"}
			}
		case 1:
			step.Speech = []string{"laurel.47"}
			if pick == 1 {
				step.Speech = []string{"laurel.48", "laurel.46"}
			}
		}
		return step, nil
	}
	step, err := c.choices(state, LaurelContinuation{Code: "breakfast"})
	if err != nil {
		return LaurelStep{}, err
	}
	if state.LaurelPhase == -1 {
		step.Speech = []string{"laurel.49"}
		step.Effects = []LaurelEffect{{Kind: LaurelEffectSetStoryValue, Target: "laurelphase", Amount: 0}}
	}
	return step, nil
}

func (c *LaurelScriptController) Continue(state LaurelStoryState, continuation *LaurelContinuation, eventID int32, random *NativeRandom) (LaurelStep, error) {
	if continuation == nil {
		return LaurelStep{}, fmt.Errorf("Laurel continuation is unavailable")
	}
	if eventID == -1 {
		return LaurelStep{Finish: true}, nil
	}
	step, err := c.Resume(state, continuation)
	if err != nil {
		return LaurelStep{}, err
	}
	found := eventID == -2 && step.ChoiceTimeoutFrames > 0
	for _, choice := range step.Choices {
		if choice.EventID == eventID {
			found = true
			break
		}
	}
	if !found {
		return LaurelStep{}, fmt.Errorf("Laurel %s stage%d has no event%d", continuation.Code, continuation.Stage, eventID)
	}
	if eventID == 55555 {
		step.Effects = []LaurelEffect{{Kind: LaurelEffectSelectHand}}
		return step, nil
	}
	code := continuation.Code
	switch code {
	case "breakfast":
		if continuation.Stage == 0 {
			loop := *continuation
			loop.Stage = 1
			step, err = c.choices(state, loop)
			step.Speech = []string{"laurel.52"}
			if state.LaurelGood < 0 {
				step.Speech = []string{"laurel.50", "laurel.51"}
			}
			if strings.EqualFold(state.JonesOwner, "message") {
				step.Speech = append(step.Speech, "laurel.53")
			}
			return step, err
		}
		outer := *continuation
		switch eventID {
		case 102, 103:
			outer.Flags |= 1
			step, err = c.choices(state, LaurelContinuation{Code: "apologize", Resume: &outer})
			if eventID == 102 {
				step.Speech = []string{"laurel.54", "laurel.55"}
				step.Effects = []LaurelEffect{{Kind: LaurelEffectSetStoryValue, Target: "laurelgood", Amount: -1}}
			} else {
				step.Speech = []string{"laurel.56"}
				step.Effects = []LaurelEffect{{Kind: LaurelEffectSetStoryValue, Target: "laurelgood", Amount: 0}}
			}
		case 201:
			outer.Flags |= 2
			step, err = c.choices(state, LaurelContinuation{Code: "silicon", Resume: &outer})
			step.Speech = []string{"laurel.57"}
		case 301:
			step, err = c.choices(state, LaurelContinuation{Code: "bye"})
			step.Speech = []string{"laurel.64"}
		}
	case "apologize":
		if continuation.Resume == nil {
			return LaurelStep{}, fmt.Errorf("Laurel apology has no breakfast caller")
		}
		step, err = c.Resume(state, continuation.Resume)
		if eventID == 101 {
			step.Speech = []string{"laurel.58"}
			step.Effects = []LaurelEffect{{Kind: LaurelEffectSetStoryValue, Target: "laurelgood", Amount: 1}}
		} else {
			step.Speech = []string{"laurel.59"}
			step.Effects = []LaurelEffect{{Kind: LaurelEffectSetStoryValue, Target: "laurelgood", Amount: -1}}
		}
	case "silicon":
		if continuation.Stage == 0 {
			next := *continuation
			next.Stage = 1
			step, err = c.choices(state, next)
			step.Speech = []string{"laurel.60", "laurel.61", "laurel.62"}
		} else {
			if continuation.Resume == nil {
				return LaurelStep{}, fmt.Errorf("Laurel Kid story has no breakfast caller")
			}
			step, err = c.Resume(state, continuation.Resume)
			step.Speech = []string{"laurel.63"}
			step.Effects = []LaurelEffect{{Kind: LaurelEffectSetStoryValue, Target: "oonakidstory", Amount: 1}}
		}
	case "bye":
		step = LaurelStep{Finish: true, Effects: []LaurelEffect{{Kind: LaurelEffectSetStoryValue, Target: "laurelphase", Amount: 1}, {Kind: LaurelEffectMoveActor, Target: "laurel", Value: "hotlower.blood1"}}}
		switch state.LaurelGood {
		case -1:
			step.Speech = []string{"laurel.65"}
		case 0:
			step.Speech = []string{"laurel.66"}
		case 1:
			step.Speech = []string{"laurel.67"}
		}
	default:
		return LaurelStep{}, fmt.Errorf("Laurel continuation %q is unsupported", code)
	}
	return step, err
}

func (c *LaurelScriptController) BeginGift(state LaurelStoryState, what string, resume *LaurelContinuation, random *NativeRandom) (LaurelStep, error) {
	if c == nil || c.programs[7].Records == nil {
		return LaurelStep{}, fmt.Errorf("LAUREL.PUP boot resource is unavailable")
	}
	step := LaurelStep{Finish: true}
	switch strings.ToLower(what) {
	case "hankerchief":
		if state.Day == 2 && state.Clock == 2 && (state.HankerchiefDegree == 2 || state.HankerchiefDegree == 4) {
			step.Effects = []LaurelEffect{{Kind: LaurelEffectGiveInventory, Target: "laurel", Value: what}}
			if state.HankerchiefDegree == 2 {
				step.Speech = []string{"laurel.2", "laurel.3", "laurel.4"}
				step.Effects = append(step.Effects, LaurelEffect{Kind: LaurelEffectPlayerDeath, Value: "by laurel"})
				return step, nil
			}
			step.Speech = []string{"laurel.5", "laurel.2", "laurel.6"}
			step.Effects = append(step.Effects, LaurelEffect{Kind: LaurelEffectAddInventory, Value: "bullets"}, LaurelEffect{Kind: LaurelEffectSetStoryValue, Target: "laurelgood", Amount: 1})
		}
	case "jug":
		step.Speech = []string{"laurel.7", "laurel.8"}
	case "cigar":
		step.Speech = []string{"laurel.9", "laurel.10"}
		step.Effects = []LaurelEffect{{Kind: LaurelEffectGiveInventory, Target: "laurel", Value: what}}
	case "ring":
		step.Speech = []string{"laurel.11"}
	}
	if len(step.Speech) == 0 {
		switch state.Counter {
		case 0:
			step.Speech = []string{"laurel.12", "laurel.13"}
		case 1:
			step.Speech = []string{"laurel.7"}
		case 2:
			step.Speech = []string{"laurel.14", "laurel.12"}
		}
		if state.Counter >= 0 && state.Counter <= 2 {
			step.Effects = []LaurelEffect{{Kind: LaurelEffectSetStoryValue, Target: "counter", Amount: (state.Counter + 1) % 3}}
		}
	}
	if resume != nil {
		resumed, err := c.Resume(state, resume)
		if err != nil {
			return LaurelStep{}, err
		}
		resumed.Speech, resumed.Effects = step.Speech, step.Effects
		return resumed, nil
	}
	return step, nil
}
