// courtsave writes a ReDust JSON save that starts in the courtyard, for replaying the
// courtyard door and exit. It reads an existing .rtd save for the rest of the game
// state, then sets the set, scene, point and day the courtyard scripts need.
//
// Usage: go run ./tools/courtsave -from=bin/saves/ballsacks.rtd -out=<path>.json
package main

import (
	"flag"
	"log"

	"redust/save"
)

func main() {
	from := flag.String("from", "bin/saves/ballsacks.rtd", "existing save to start from")
	out := flag.String("out", "", "JSON save to write")
	set := flag.String("set", "court", "set name (court or nitecour)")
	scene := flag.String("scene", "Scene C5", "view name; C5 is the courtyard's south view, facing south")
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
	progress.Point = [3]int16{2, 4, 2}
	progress.Day = 1
	progress.Clock = 1
	progress.TownReturnScene = "Scene G8"
	if err := save.SaveGameProgress(*out, progress); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s: set=%s view=%s point=%v", *out, progress.SetName, progress.ViewName, progress.Point)
}
