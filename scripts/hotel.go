package scripts

import (
	"fmt"
	"image"
	"strings"

	"redust/assets"
)

type HotelStoryState struct {
	Day, Clock                                int
	Phase, FearPhase, LaurelPhase, BuickPhase int16
	Debugging                                 bool
	InventoryOwners                           map[string]string
	SavedScene, SavedDirection                string
}

type HotelActionKind uint8

const (
	HotelActionDoor HotelActionKind = iota + 1
	HotelActionKnock
	HotelActionSleep
	HotelActionMovie
	HotelActionFearVoices
	HotelActionTransition
	HotelActionPuppet
	HotelActionBreakfast
	HotelActionHotplateChoice
	HotelActionHotplatePaper
	HotelActionHotplateSound
	HotelActionHotplateExit
)

type HotelVoiceStep struct {
	Cue, DoorOwner string
	DelayFrames    int
	ClearDoor      bool
}
type HotelAction struct {
	Kind                                                                                                                               HotelActionKind
	Movie, SetName, ViewName, Direction, DoorOwner, Sound, GiveItem, AddInventory, ActorSetup, ActorSelector, CloseTrack, ReturnTarget string
	SaveRoomReturn, PutDownBlood, CloseStage                                                                                           bool
	FadeOutFrames, DelayFrames                                                                                                         int
	FearPhase                                                                                                                          int16
	VoiceSteps                                                                                                                         []HotelVoiceStep
	StageName, SoundBank, Puppet, PutDownActor                                                                                         string
	ActorSetups                                                                                                                        []HotelActorSetup
	SetPhase, ResetFearPhase, ResetBuickPhase, SetLaurelPhase                                                                          bool
	Phase, LaurelPhase                                                                                                                 int16
	FlatTarget                                                                                                                         int
	Clut, VisualEffect                                                                                                                 string
	FadeInFrames                                                                                                                       int
	BlackScreen                                                                                                                        bool
}

type HotelActorSetup struct{ Name, Selector string }

type HotelSceneTriggerStepKind uint8

const (
	HotelTriggerSetPhase HotelSceneTriggerStepKind = iota + 1
	HotelTriggerSetLockEvents
	HotelTriggerRunPuppet
	HotelTriggerDelay
	HotelTriggerMoveActor
)

type HotelSceneTriggerStep struct {
	Kind                       HotelSceneTriggerStepKind
	Phase                      int16
	LockEvents                 bool
	Actor, Puppet, Destination string
	DelayTicks                 int
}

type HotelSceneOpenAction struct {
	Direction           int16
	LockEvents          bool
	LoopName, LoopScene string
	LoopCode            string
	LoopTicks           int
	TriggerSteps        []HotelSceneTriggerStep
}

func NativeDay3Bedtime(phase int16, owners map[string]string) bool {
	return owners["tbird"] == "stranger" && owners["tstone"] == "stranger" && phase > 3
}
func NativeCanSleep(state HotelStoryState) bool {
	return state.Day == 1 || state.Day == 2 && state.Clock == 3 || state.Day == 3 && state.Clock == 3 && NativeDay3Bedtime(state.Phase, state.InventoryOwners)
}

func HotelRoomDoorLocked(state HotelStoryState) bool {
	if state.Debugging || state.Day == 1 && state.Phase == 5 {
		return false
	}
	return state.Day == 1 && state.Phase < 7 || state.InventoryOwners["hrkey"] != "stranger"
}

func HotelMouseAction(set string, resource uint32, direction int16, point uint32, state HotelStoryState) (HotelAction, bool) {
	position := image.Pt(int(int16(point>>16)), int(int16(point)))
	if !position.In(image.Rect(0, 0, 512, 264)) {
		return HotelAction{}, false
	}
	in := func(left, top, right, bottom int) bool {
		return position.X > left && position.Y > top && position.X < right && position.Y < bottom
	}
	if set == "hotlower" && resource == 40 && (state.Day == 2 || state.Day == 3) && state.Clock == 1 && state.Phase == 1 && (direction == assets.SetDirectionSouth && in(149, 203, 378, 261) || direction == assets.SetDirectionWest && in(162, 197, 372, 264)) {
		if state.Day == 2 && (state.LaurelPhase == 0 || state.LaurelPhase == -1) {
			return HotelAction{Kind: HotelActionPuppet, Puppet: "PUPPETS/LAUREL.PUP", SetLaurelPhase: true, LaurelPhase: -1}, true
		}
		if state.Day == 3 && state.BuickPhase == 0 {
			return HotelAction{Kind: HotelActionPuppet, Puppet: "PUPPETS/BUICK.PUP"}, true
		}
		action := HotelAction{Kind: HotelActionBreakfast, StageName: "DATA/HOTPLATE.FLT", SoundBank: "DATA/HOTPLATE.SND", FadeOutFrames: 30, FadeInFrames: 30, VisualEffect: "irisopen", SetPhase: true, Phase: 2, ResetFearPhase: true}
		if state.Day == 2 {
			action.PutDownActor, action.ActorSetups = "laurel", []HotelActorSetup{{"jones", "daychores"}, {"trotter", "day2street"}, {"marie", "day2street"}, {"mwife", "day2street"}, {"blood", "day2street"}}
		} else {
			action.ResetBuickPhase, action.ActorSetups = true, []HotelActorSetup{{"buick", "daychores"}, {"mwife", "day3AM"}, {"laurel", "day3AM"}, {"jones", "day3AM"}, {"marie", "day2PM"}}
		}
		return action, true
	}
	if set == "hotupper" && resource == 45 && direction == assets.SetDirectionWest && in(168, 50, 329, 263) {
		if HotelRoomDoorLocked(state) {
			return HotelAction{Kind: HotelActionKnock, Sound: "knock1"}, true
		}
		if state.Day == 1 && state.Phase == 5 {
			action := HotelAction{Kind: HotelActionFearVoices, Sound: "knock1", SoundBank: "DATA/FEARWITT.SND", FearPhase: state.FearPhase}
			switch {
			case state.FearPhase < 3:
				action.FearPhase, action.VoiceSteps = 3, []HotelVoiceStep{{Cue: "fear.44"}, {Cue: "fear.45"}}
			case state.FearPhase == 3:
				action.FearPhase, action.VoiceSteps = 4, []HotelVoiceStep{{Cue: "fear.46"}, {DelayFrames: 90}, {DoorOwner: "playroom"}, {Cue: "fear.47"}, {Cue: "fear.48"}, {Cue: "fear.49"}, {ClearDoor: true}}
			case state.FearPhase == 4:
				action.VoiceSteps = []HotelVoiceStep{{DoorOwner: "playroom"}, {Cue: "fear.51"}, {ClearDoor: true}}
			default:
				return HotelAction{Kind: HotelActionDoor, DoorOwner: "playroom", GiveItem: "hrkey", DelayFrames: 30}, true
			}
			return action, true
		}
		return HotelAction{Kind: HotelActionDoor, DoorOwner: "playroom", GiveItem: "hrkey", DelayFrames: 30}, true
	}
	if set == "hotroom" && resource == 36 && direction == assets.SetDirectionEast && in(176, 62, 339, 263) {
		return HotelAction{Kind: HotelActionDoor, DoorOwner: "inside"}, true
	}
	if set == "hotroom" && resource == 34 {
		if direction == assets.SetDirectionWest && in(153, 210, 512, 264) && NativeCanSleep(state) {
			return HotelAction{Kind: HotelActionSleep, Movie: "MOVIES/HOTBED.MOV", FadeOutFrames: 10}, true
		}
		if direction == assets.SetDirectionWest && in(223, 64, 319, 215) {
			movie := "MOVIES/HWIN.MOV"
			if state.Clock == 3 {
				movie = "MOVIES/NITEHWIN.MOV"
			}
			return HotelAction{Kind: HotelActionMovie, Movie: movie}, true
		}
		if direction == assets.SetDirectionNorth {
			return HotelAction{Kind: HotelActionMovie, Movie: "MOVIES/HOTLSTAN.MOV"}, true
		}
	}
	if set == "mayroom" && resource == 37 && direction == assets.SetDirectionNorth && state.Day == 1 && in(58, 146, 401, 263) {
		return HotelAction{Kind: HotelActionSleep, Movie: "MOVIES/MAYBED.MOV", FadeOutFrames: 10}, true
	}
	return HotelAction{}, false
}

func HotelForwardAction(set string, resource uint32, direction int16, doorOwner string, state HotelStoryState) (HotelAction, bool) {
	if set == "hotlower" && resource == 48 && direction == assets.SetDirectionNorth {
		return HotelAction{Kind: HotelActionTransition, Movie: "MOVIES/HOTUP.MOV", SetName: "hotupper.set", FadeOutFrames: 10, PutDownBlood: state.Day == 1 && state.Phase > 4 && state.Phase < 8}, true
	}
	if set == "hotupper" && resource == 46 && direction == assets.SetDirectionSouth {
		return HotelAction{Kind: HotelActionTransition, Movie: "MOVIES/HOTDN.MOV", SetName: "hotlower.set", ViewName: "Scene D3", Direction: "south", FadeOutFrames: 10}, true
	}
	if set == "hotupper" && resource == 45 && direction == assets.SetDirectionWest && doorOwner == "playroom" {
		return HotelAction{Kind: HotelActionTransition, SetName: "hotroom.set", SaveRoomReturn: true}, true
	}
	if set == "hotroom" && resource == 36 && direction == assets.SetDirectionEast && doorOwner == "inside" {
		action := HotelAction{Kind: HotelActionTransition, SetName: "hotupper.set", ViewName: state.SavedScene, Direction: state.SavedDirection}
		if state.Clock == 1 && state.Phase == 0 {
			if state.Day == 2 {
				action.ActorSetup, action.ActorSelector = "jones", "hallway"
			}
			if state.Day == 3 {
				action.ActorSetup, action.ActorSelector = "buick", "hallway"
			}
		}
		return action, true
	}
	return HotelAction{}, false
}

func HotelHotplateMouseAction(resource uint32, state HotelStoryState) (HotelAction, bool) {
	switch resource {
	case 17:
		return HotelAction{Kind: HotelActionHotplateChoice, FlatTarget: 4, AddInventory: "biscuits", Sound: "takemuffin"}, true
	case 18:
		return HotelAction{Kind: HotelActionHotplateChoice, FlatTarget: 5, AddInventory: "biscuits", Sound: "takemuffin"}, true
	case 19:
		return HotelAction{Kind: HotelActionHotplateChoice, FlatTarget: 3, AddInventory: "sugarcubes", Sound: "takesugar"}, true
	case 20:
		return HotelAction{Kind: HotelActionHotplateChoice, FlatTarget: 5, AddInventory: "sugarcubes", Sound: "takesugar"}, true
	case 21, 22, 23, 24:
		if state.Day < 1 || state.Day > 4 {
			return HotelAction{}, false
		}
		return HotelAction{Kind: HotelActionHotplatePaper, Movie: fmt.Sprintf("MOVIES/PAPER%d.MOV", state.Day), Sound: "takepaper", FadeOutFrames: 30, FadeInFrames: 30, Clut: "black", VisualEffect: "plain", BlackScreen: true}, true
	case 25, 26, 27, 28, 29, 30, 31, 32:
		return HotelAction{Kind: HotelActionHotplateSound, Sound: "silverware"}, true
	case 33, 34, 35, 36:
		return HotelAction{Kind: HotelActionHotplateSound, Sound: "cup"}, true
	default:
		return HotelAction{}, false
	}
}

func HotelHotplateBackgroundAction(flatName string) (HotelAction, bool) {
	if !strings.EqualFold(flatName, "Flat 4") {
		return HotelAction{}, false
	}
	return HotelAction{Kind: HotelActionHotplateExit, StageName: "DATA/NEW.FLT", CloseStage: true, CloseTrack: "hotplate", ReturnTarget: "set", FadeOutFrames: 30, FadeInFrames: 30, VisualEffect: "plain"}, true
}

func HotelOpenSceneAction(set string, resource uint32, state HotelStoryState) (HotelSceneOpenAction, bool) {
	if set != "hotupper" || resource != 45 || state.Clock != 1 || state.Phase != 0 {
		return HotelSceneOpenAction{}, false
	}
	actor, puppet := "", ""
	switch state.Day {
	case 2:
		actor, puppet = "jones", "jones.pup"
	case 3:
		actor, puppet = "buick", "buick.pup"
	default:
		return HotelSceneOpenAction{}, false
	}
	return HotelSceneOpenAction{
		Direction:  assets.SetDirectionNorth,
		LockEvents: true,
		LoopName:   "scene",
		LoopScene:  "Scene C4",
		LoopCode:   "trigger",
		LoopTicks:  20,
		TriggerSteps: []HotelSceneTriggerStep{
			{Kind: HotelTriggerSetPhase, Phase: 1},
			{Kind: HotelTriggerSetLockEvents, LockEvents: false},
			{Kind: HotelTriggerRunPuppet, Actor: actor, Puppet: puppet},
			{Kind: HotelTriggerDelay, DelayTicks: 30},
			{Kind: HotelTriggerMoveActor, Actor: actor, Destination: "hotupper.jones2"},
		},
	}, true
}
