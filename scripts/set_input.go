package scripts

import "redust/assets"

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
