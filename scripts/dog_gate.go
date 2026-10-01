package scripts

import "strings"

func NiteDogGateMovieInSet(setName string, viewResource uint32, direction int16, day int, dogVisible bool) (string, bool) {
	setName = strings.TrimSuffix(strings.ToLower(setName), ".set")
	if setName != "nite" && setName != "town" {
		return "", false
	}
	return NiteDogGateMovieInView(viewResource, direction, day, dogVisible)
}
