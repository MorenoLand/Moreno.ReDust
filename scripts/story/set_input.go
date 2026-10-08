package story

import (
	"fmt"
	"redust/assets"
)

type SetObjectAction struct {
	Object string
	Movie  string
}

func NiteDogGateMovie(direction int16, day int, dogVisible bool) (string, bool) {
	if direction != assets.SetDirectionNorth || day != 1 || !dogVisible {
		return "", false
	}
	return "MOVIES/DOG1.MOV", true
}

func NiteDogGateMovieInView(viewResource uint32, direction int16, day int, dogVisible bool) (string, bool) {
	if viewResource != 135 {
		return "", false
	}
	return NiteDogGateMovie(direction, day, dogVisible)
}

func NiteNorthObjectAction(direction int16, point uint32, clock int) (SetObjectAction, bool) {
	if direction != 1 {
		return SetObjectAction{}, false
	}
	x, y := int16(point>>16), int16(point)
	if x > 2 && y > 108 && x < 127 && y < 194 {
		movie := "MOVIES/WARNING.MOV"
		if clock == 3 {
			movie = "MOVIES/NITEWARN.MOV"
		}
		return SetObjectAction{Object: "rules", Movie: movie}, true
	}
	if x > 374 && y > 127 && x < 509 && y < 178 {
		movie := "MOVIES/FIREARM.MOV"
		if clock == 3 {
			movie = "MOVIES/NITEFIRE.MOV"
		}
		return SetObjectAction{Object: "fire", Movie: movie}, true
	}
	return SetObjectAction{}, false
}

func NiteSceneObjectAction(viewResource uint32, direction int16, point uint32, day, clock int) (SetObjectAction, bool) {
	x, y := int16(point>>16), int16(point)
	inside := func(left, top, right, bottom int16) bool { return x > left && y > top && x < right && y < bottom }
	if viewResource == 137 {
		return NiteNorthObjectAction(direction, point, clock)
	}
	switch viewResource {
	case 68:
		if clock == 3 {
			return SetObjectAction{}, false
		}
		if direction == assets.SetDirectionWest && inside(340, 143, 380, 202) {
			return SetObjectAction{Object: "grave1", Movie: "MOVIES/GRAVE1.MOV"}, true
		}
		if direction == assets.SetDirectionNorth && inside(208, 151, 239, 189) {
			return SetObjectAction{Object: "grave2", Movie: "MOVIES/GRAVE2.MOV"}, true
		}
		if direction == assets.SetDirectionSouth && inside(259, 158, 299, 197) {
			return SetObjectAction{Object: "graves", Movie: "MOVIES/GRAVES.MOV"}, true
		}
	case 86:
		if direction == assets.SetDirectionWest && day < 5 && inside(288, 125, 346, 175) {
			return SetObjectAction{Object: "news", Movie: fmt.Sprintf("MOVIES/PAPER%d.MOV", day)}, true
		}
		if direction == assets.SetDirectionEast && clock != 3 && inside(187, 44, 315, 264) {
			return SetObjectAction{Object: "outhouse", Movie: "MOVIES/OUTHOUSE.MOV"}, true
		}
	case 97:
		if direction == assets.SetDirectionNorth && inside(167, 3, 432, 107) {
			movie := "MOVIES/BELL.MOV"
			if clock == 3 {
				movie = "MOVIES/NITEBELL.MOV"
			}
			return SetObjectAction{Object: "bell", Movie: movie}, true
		}
		if direction == assets.SetDirectionSouth && clock != 3 && inside(133, 78, 333, 172) {
			return SetObjectAction{Object: "post", Movie: "MOVIES/DOCSIDE2.MOV"}, true
		}
	case 119:
		if direction == assets.SetDirectionSouth && clock != 3 && inside(89, 87, 241, 173) {
			return SetObjectAction{Object: "post", Movie: "MOVIES/JAILPOST.MOV"}, true
		}
	case 132:
		if direction == assets.SetDirectionEast && clock != 3 && inside(359, 126, 445, 229) {
			return SetObjectAction{Object: "apothfront", Movie: "MOVIES/APOTHFNT.MOV"}, true
		}
	case 133:
		if direction == assets.SetDirectionEast && clock != 3 && inside(307, 118, 419, 242) {
			return SetObjectAction{Object: "groceryfront", Movie: "MOVIES/GROCFRNT.MOV"}, true
		}
	case 149:
		if direction == assets.SetDirectionSouth && clock != 3 && inside(50, 54, 280, 170) {
			return SetObjectAction{Object: "post", Movie: "MOVIES/CHINPOST.MOV"}, true
		}
		if direction == assets.SetDirectionNorth && clock != 3 && inside(174, 30, 443, 187) {
			return SetObjectAction{Object: "hard", Movie: "MOVIES/GROCPOS.MOV"}, true
		}
	case 174:
		if direction == assets.SetDirectionWest && clock != 3 && inside(237, 27, 307, 146) {
			return SetObjectAction{Object: "liveryfront", Movie: "MOVIES/HOTELBAC.MOV"}, true
		}
	case 177:
		if direction == assets.SetDirectionWest && clock != 3 && inside(332, 157, 429, 231) {
			return SetObjectAction{Object: "mayorsign", Movie: "MOVIES/APOTH.MOV"}, true
		}
	}
	return SetObjectAction{}, false
}
