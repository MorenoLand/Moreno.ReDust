package scripts

import "strings"

func CastActorMouseDownScene(actorName string) (string, bool) {
	if strings.EqualFold(actorName, "dog") {
		return "Scene G12", true
	}
	return "", false
}
