// courtsave writes a ReDust JSON save that starts in the courtyard, for replaying the
// courtyard door and exit. It reads an existing .rtd save for the rest of the game
// state, then sets the set, scene, point and day the courtyard scripts need.
//
// Usage: go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -out=<path>.json
package main

import (
	"flag"
	"fmt"
	"log"

	"redust/save"
)

func main() {
	from := flag.String("from", "bin/saves/ballsacks.rtd", "existing save to start from")
	out := flag.String("out", "", "JSON save to write")
	set := flag.String("set", "court", "set file name: court, nitecour, chin, and so on")
	scene := flag.String("scene", "Scene C5", "view name; C5 is the courtyard's south view, facing south")
	path6 := flag.String("path6", "", "script context page 6, the path prefix a set's stage search uses (for example dust:target:)")
	clock := flag.Int("clock", 1, "game clock; 3 is night, which opens nitecour when the set is night")
	point := flag.String("point", "2,4,2", "world point as direction,scene,facing (1 north, 2 south, 3 east, 4 west)")
	flag.Parse()
	if *out == "" {
		log.Fatal("-out is required")
	}
	progress, err := save.LoadGameFile(*from, "")
	if err != nil {
		log.Fatal(err)
	}
	progress.SetName = *set
	progress.ViewName = *scene
	var pointValue [3]int16
	if _, err := fmt.Sscanf(*point, "%d,%d,%d", &pointValue[0], &pointValue[1], &pointValue[2]); err != nil {
		log.Fatalf("-point must be direction,scene,facing: %v", err)
	}
	progress.Point = pointValue
	progress.Day = 1
	progress.Clock = *clock
	progress.TownReturnScene = "Scene G8"
	if *path6 != "" && len(progress.ScriptContextPages) == 9 {
		progress.ScriptContextPages[6] = []byte(*path6)
	}
	if err := save.SaveGameProgress(*out, progress); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s: set=%s view=%s point=%v", *out, progress.SetName, progress.ViewName, progress.Point)
}
