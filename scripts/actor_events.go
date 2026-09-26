package scripts

import "strings"

type ActorIdleStep struct {
	Pose      string
	Callback  string
	Remaining int32
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
