package scripts

import "strings"

type ActorIdleStep struct {
	Pose           string
	Callback       string
	Remaining      int32
	TurnToCamera   bool
	TurnBy         int16
	Attention      int32
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

func HelpIdleStep(callback string, near, dogVisible bool, day int, phase int16) (ActorIdleStep, bool) {
	if !strings.EqualFold(callback, "helpidle") {
		return ActorIdleStep{}, false
	}
	step := ActorIdleStep{Pose: "stand", Callback: "helpidle", Remaining: 19}
	if near {
		step.TurnToCamera = true
		if day == 1 && !dogVisible && phase < 3 {
			step.Attention = 5
		}
	} else {
		step.ClearAttention = true
	}
	return step, true
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

func NativeTurnStep(current, target, speed int16) int16 {
	const maximum = 255
	position, destination, rate := int(current), int(target), int(speed)
	if position < destination {
		if destination-position < maximum-destination+position {
			position += rate
			if destination < position {
				return target
			}
			return int16(position)
		}
		position -= rate
		if position < 0 {
			position += maximum
			if position < destination {
				return target
			}
		}
		return int16(position)
	}
	if position-destination < maximum-position+destination {
		position -= rate
		if position < destination {
			return target
		}
		return int16(position)
	}
	position += rate
	if maximum <= position {
		position -= maximum
		if destination < position {
			return target
		}
	}
	return int16(position)
}

type NativeActorWalkJob struct {
	start, target [3]int16
	distance      int
	progress      int
	speed         int16
	heading       int16
}

func LeroyMouseDownAction(day, distance, hotDistance int) bool {
	return day != 5 && distance < hotDistance
}

func NativeActorDistance2D(first, second [3]int16) int {
	dx, dy := int64(first[0])-int64(second[0]), int64(first[1])-int64(second[1])
	return int(NativeIntegerSqrt(uint64(dx*dx + dy*dy)))
}

func NativeWalktopuppetAxisAligned(actor, player [3]int16) bool {
	return int(player[0])/256-int(actor[0])/256 == 0 || int(player[1])/256-int(actor[1])/256 == 0
}

func NewNativeActorWalkJob(start, target [3]int16, heading, speed int16) NativeActorWalkJob {
	dx, dy, dz := int64(start[0])-int64(target[0]), int64(start[1])-int64(target[1]), int64(start[2])-int64(target[2])
	distance := int(NativeIntegerSqrt(uint64(dx*dx + dy*dy + dz*dz)))
	return NativeActorWalkJob{start: start, target: target, distance: distance, speed: speed, heading: heading}
}

func (job *NativeActorWalkJob) Pass(position [3]int16, heading, turnSpeed int16) ([3]int16, int16, bool) {
	if heading != job.heading {
		heading = NativeTurnStep(heading, job.heading, turnSpeed)
		return position, heading, true
	}
	if job.distance == 0 {
		return job.target, heading, false
	}
	job.progress += int(job.speed)
	if job.progress > job.distance {
		job.progress = job.distance
	}
	for axis := range job.start {
		delta := int64(job.start[axis]) - int64(job.target[axis])
		position[axis] = int16(int64(job.start[axis]) - delta*int64(job.progress)/int64(job.distance))
	}
	return position, heading, job.progress < job.distance
}

func NativeIntegerSqrt(value uint64) uint64 {
	result, bit := uint64(0), uint64(1)<<62
	for bit > value {
		bit >>= 2
	}
	for bit != 0 {
		if value >= result+bit {
			value -= result + bit
			result = (result >> 1) + bit
		} else {
			result >>= 1
		}
		bit >>= 2
	}
	return result
}
