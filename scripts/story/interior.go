package story

import "redust/assets"

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
