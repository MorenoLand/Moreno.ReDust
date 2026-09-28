package scripts

import (
	"fmt"
	"redust/assets"
)

func NiteDoorAt(viewResource uint32, direction int16, point uint32) (string, bool) {
	x, y := int16(point>>16), int16(point)
	switch viewResource {
	case 59:
		if direction == assets.SetDirectionNorth && x > 288 && y > 77 && x < 380 && y < 256 {
			return "store", true
		}
	case 131:
		if direction == assets.SetDirectionWest && x > 241 && y > 92 && x < 307 && y < 201 {
			return "saloon", true
		}
	case 132:
		if direction == assets.SetDirectionEast && x > 218 && y > 94 && x < 286 && y < 205 {
			return "apoth", true
		}
	case 133:
		if direction == assets.SetDirectionEast && x > 222 && y > 96 && x < 287 && y < 211 {
			return "store", true
		}
	case 174:
		if direction == assets.SetDirectionEast && x > 204 && y > 82 && x < 293 && y < 235 {
			return "livery", true
		}
	}
	return "", false
}

func NiteInteriorTarget(viewResource uint32, direction int16, owner string) (string, bool) {
	switch {
	case viewResource == 131 && direction == assets.SetDirectionWest && owner == "saloon":
		return "sallower.set", true
	case viewResource == 132 && direction == assets.SetDirectionEast && owner == "apoth":
		return "apoth.set", true
	case viewResource == 133 && direction == assets.SetDirectionEast && owner == "store":
		return "store.set", true
	case viewResource == 174 && direction == assets.SetDirectionEast && owner == "livery":
		return "livery.set", true
	default:
		return "", false
	}
}

func NiteDoorLocked(owner string, day, clock int, phase int16, debugging, fightOn bool, random *NativeRandom) (bool, error) {
	if debugging {
		return false, nil
	}
	switch owner {
	case "saloon":
		return clock == 1 && day != 4 || day == 1 && phase != 7 || fightOn, nil
	case "apoth":
		return clock == 3 || day == 2 && clock == 1 && phase < 2 || day == 3 && clock == 1 && phase < 2 || day == 4 || fightOn, nil
	case "store":
		if clock < 3 || day == 1 || fightOn || day == 4 {
			return true, nil
		}
		if random == nil {
			return false, fmt.Errorf("store lock requires native random state")
		}
		return random.Inclusive(100) < 50, nil
	case "livery":
		return clock == 3 || day == 2 && clock == 1 && phase < 2 || day == 3 && clock == 1 && phase < 2 || day == 4 || fightOn, nil
	default:
		return false, nil
	}
}

func SallowerDoorAt(viewResource uint32, direction int16, point uint32) (string, bool) {
	x, y := int16(point>>16), int16(point)
	if viewResource == 52 && direction == assets.SetDirectionEast && x > 144 && y > 7 && x < 387 && y < 264 {
		return "salout", true
	}
	return "", false
}

func SallowerExitToTown(direction int16, owner string) bool {
	return direction == assets.SetDirectionEast && owner == "salout"
}
