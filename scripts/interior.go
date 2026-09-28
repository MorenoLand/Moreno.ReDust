package scripts

import (
	"fmt"
	"redust/assets"
)

func NiteDoorAt(viewResource uint32, direction int16, point uint32) (string, bool) {
	x, y := int16(point>>16), int16(point)
	switch viewResource {
	case 40:
		if direction == assets.SetDirectionSouth && x > 206 && y > 74 && x < 298 && y < 221 {
			return "undertak", true
		}
	case 59:
		if direction == assets.SetDirectionNorth && x > 288 && y > 77 && x < 380 && y < 256 {
			return "storeback", true
		}
	case 86:
		if direction == assets.SetDirectionWest && x > 213 && y > 98 && x < 282 && y < 211 {
			return "paper", true
		}
	case 88:
		if direction == assets.SetDirectionEast && x > 3 && y > 83 && x < 91 && y < 234 {
			return "back", true
		}
	case 127:
		if direction == assets.SetDirectionNorth && x > 160 && y > 22 && x < 338 && y < 214 {
			return "court", true
		}
	case 128:
		if direction == assets.SetDirectionWest && x > 215 && y > 85 && x < 299 && y < 225 {
			return "doctor", true
		}
		if direction == assets.SetDirectionEast && x > 200 && y > 91 && x < 305 && y < 203 {
			return "hotel", true
		}
	case 129:
		if direction == assets.SetDirectionWest && x > 200 && y > 81 && x < 306 && y < 232 {
			return "bank", true
		}
	case 131:
		if direction == assets.SetDirectionWest && x > 241 && y > 92 && x < 307 && y < 201 {
			return "saloon", true
		}
		if direction == assets.SetDirectionEast && x > 220 && y > 98 && x < 285 && y < 209 {
			return "stage", true
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
	case 135:
		if direction == assets.SetDirectionWest && x > 122 && y > 77 && x < 218 && y < 230 {
			return "jail", true
		}
		if direction == assets.SetDirectionEast && x > 218 && y > 100 && x < 278 && y < 204 {
			return "chin", true
		}
	case 177:
		if direction == assets.SetDirectionEast && x > 174 && y > 82 && x < 335 && y < 228 {
			return "mayor", true
		}
	}
	return "", false
}

func NiteInteriorTarget(viewResource uint32, direction int16, owner string) (string, bool) {
	switch {
	case viewResource == 40 && direction == assets.SetDirectionSouth && owner == "undertak":
		return "undertak.set", true
	case viewResource == 86 && direction == assets.SetDirectionWest && owner == "paper":
		return "paper.set", true
	case viewResource == 88 && direction == assets.SetDirectionEast && owner == "back":
		return "sallower.set", true
	case viewResource == 127 && direction == assets.SetDirectionNorth && owner == "court":
		return "court.set", true
	case viewResource == 128 && direction == assets.SetDirectionWest && owner == "doctor":
		return "doctor1.set", true
	case viewResource == 131 && direction == assets.SetDirectionWest && owner == "saloon":
		return "sallower.set", true
	case viewResource == 131 && direction == assets.SetDirectionEast && owner == "stage":
		return "stage.set", true
	case viewResource == 132 && direction == assets.SetDirectionEast && owner == "apoth":
		return "apoth.set", true
	case viewResource == 128 && direction == assets.SetDirectionEast && owner == "hotel":
		return "hotlower.set", true
	case viewResource == 133 && direction == assets.SetDirectionEast && owner == "store":
		return "store.set", true
	case viewResource == 174 && direction == assets.SetDirectionEast && owner == "livery":
		return "livery.set", true
	case viewResource == 129 && direction == assets.SetDirectionWest && owner == "bank":
		return "bank.set", true
	case viewResource == 135 && direction == assets.SetDirectionWest && owner == "jail":
		return "jail.set", true
	case viewResource == 135 && direction == assets.SetDirectionEast && owner == "chin":
		return "chin.set", true
	case viewResource == 177 && direction == assets.SetDirectionEast && owner == "mayor":
		return "mayhall.set", true
	default:
		return "", false
	}
}

func NiteInteriorTargetForState(viewResource uint32, direction int16, owner string, clock int) (string, string, string, bool) {
	if viewResource == 88 && direction == assets.SetDirectionEast && owner == "back" {
		return "sallower.set", "Scene B4", "east", true
	}
	if viewResource == 127 && direction == assets.SetDirectionNorth && owner == "court" && clock == 3 {
		return "nitecour.set", "", "", true
	}
	target, found := NiteInteriorTarget(viewResource, direction, owner)
	return target, "", "", found
}

func NiteDoorLocked(owner string, day, clock int, phase int16, debugging, fightOn bool, random *NativeRandom, inventoryOwners map[string]string) (bool, error) {
	if debugging {
		return false, nil
	}
	switch owner {
	case "undertak", "doctor", "stage":
		return clock == 3 || day == 2 && clock == 1 && phase < 2 || day == 3 && clock == 1 && phase < 2 || day == 4 || fightOn, nil
	case "saloon":
		return clock == 1 && day != 4 || day == 1 && phase != 7 || fightOn, nil
	case "apoth":
		return clock == 3 || day == 2 && clock == 1 && phase < 2 || day == 3 && clock == 1 && phase < 2 || day == 4 || fightOn, nil
	case "hotel":
		return day == 4 || fightOn, nil
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
	case "court", "mayor":
		return fightOn, nil
	case "bank":
		hairpinOwner := inventoryOwners["hairpin"]
		return day == 2 && clock == 1 && phase < 2 || day == 3 && clock == 1 && phase < 2 || day == 4 || fightOn || clock == 3 && !(day == 3 && hairpinOwner == "stranger"), nil
	case "jail":
		return day < 2 || day == 2 && clock < 3 || fightOn && !(day == 4 && clock == 3), nil
	case "chin":
		return day == 1 && phase < 2 || day == 2 && clock == 2 && phase > 0 || day == 4 || fightOn, nil
	case "storeback":
		return true, nil
	case "paper":
		return clock == 3 || day == 2 && clock == 1 && phase < 2 || day == 3 && clock == 1 || day == 4 || fightOn, nil
	case "back":
		return fightOn, nil
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

func HotLowerDoorAt(viewResource uint32, direction int16, point uint32) (string, bool) {
	x, y := int16(point>>16), int16(point)
	if viewResource == 34 && direction == assets.SetDirectionWest && x > 128 && y > 73 && x < 394 && y < 262 {
		return "hotout", true
	}
	return "", false
}

func HotLowerExitToTown(viewResource uint32, direction int16, owner string) (string, bool) {
	if viewResource == 34 && direction == assets.SetDirectionWest && owner == "hotout" {
		return "west", true
	}
	return "", false
}

func SallowerExitToTown(direction int16, owner string) bool {
	return direction == assets.SetDirectionEast && owner == "salout"
}
