package scripts

import "strings"

type ActorIdleStep struct {
	Pose          string
	Callback      string
	Remaining     int32
	TurnToCamera  bool
	TurnBy        int16
	Attention     int32
	ClearAttention bool
}

func CastActorMouseDownScene(actorName string) (string, bool) {
	if strings.EqualFold(actorName, "dog") {
		return "Scene G12", true
	}
	return "", false
}

func DogIdleStep(callback string, random *NativeRandom) (ActorIdleStep, bool) {
	if random == nil {
		return ActorIdleStep{}, false
	}
	switch strings.ToLower(callback) {
	case "doright":
		return ActorIdleStep{Pose: "stand", Callback: "lookright", Remaining: int32(random.Inclusive(40) + 10)}, true
	case "lookright":
		return ActorIdleStep{Pose: "alt", Callback: "doleft", Remaining: int32(random.Inclusive(5))}, true
	case "doleft":
		return ActorIdleStep{Pose: "left", Callback: "lookleft", Remaining: int32(random.Inclusive(40) + 10)}, true
	case "lookleft":
		return ActorIdleStep{Pose: "alt", Callback: "doright", Remaining: int32(random.Inclusive(5))}, true
	default:
		return ActorIdleStep{}, false
	}
}

func LeroyIdleStep(callback string, near, starred bool, phase int16, random *NativeRandom) (ActorIdleStep, bool) {
	switch strings.ToLower(callback) {
	case "toidle":
		return ActorIdleStep{Pose: "stand", Callback: "leroyidle", Remaining: 20}, true
	case "leroyidle":
		if random == nil {
			return ActorIdleStep{}, false
		}
		if random.Inclusive(100) < 8 {
			return ActorIdleStep{Pose: "drink", Callback: "toidle", Remaining: 25}, true
		}
		step := ActorIdleStep{Pose: "stand", Callback: "leroyidle", Remaining: 20}
		if near {
			step.TurnToCamera = true
			if starred && phase == 0 {
				step.Attention = 10
			}
		} else {
			step.TurnBy, step.ClearAttention = 2, true
		}
		return step, true
	default:
		return ActorIdleStep{}, false
	}
}
