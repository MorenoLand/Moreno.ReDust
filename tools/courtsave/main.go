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
	"strings"

	"redust/save"
	"redust/scripts"
)

func main() {
	from := flag.String("from", "bin/saves/ballsacks.rtd", "existing save to start from")
	out := flag.String("out", "", "JSON save to write")
	set := flag.String("set", "court", "set file name: court, nitecour, chin, and so on")
	scene := flag.String("scene", "Scene C5", "view name; C5 is the courtyard's south view, facing south")
	path6 := flag.String("path6", "", "script context page 6, the path prefix a set's stage search uses (for example dust:target:)")
	gun := flag.Bool("gun", false, "hold the gun with six bullets and show its gunhand prop, for the target range's shots")
	hand := flag.String("hand", "", "hold this inventory item in hand and give it to the player (for example mask, for the mine puzzle)")
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
	if *gun {
		holdGun(&progress)
	}
	if *hand != "" {
		holdItem(&progress, *hand)
	}
	if err := save.SaveGameProgress(*out, progress); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s: set=%s view=%s point=%v", *out, progress.SetName, progress.ViewName, progress.Point)
}

// holdGun puts the gun in the player's hand with six bullets, the state the target range's
// shot needs (BOOTFILE's mousedown fires bullet only with handitem "gun" and a gunhand view
// other than reload). The gunhand prop is shown in its idle view at the bullet's anchor.
func holdGun(progress *save.GameProgress) {
	progress.HandItem = "gun"
	if progress.InventoryOwners == nil {
		progress.InventoryOwners = map[string]string{}
	}
	progress.InventoryOwners["gun"] = "stranger"
	progress.InventoryOwners["bullets"] = "stranger"
	setGlobal := func(name string, value scripts.GlobalVariable) {
		for index := range progress.ScriptGlobals {
			if progress.ScriptGlobals[index].Name == name {
				progress.ScriptGlobals[index] = value
				return
			}
		}
		progress.ScriptGlobals = append(progress.ScriptGlobals, value)
	}
	setGlobal("bulletcount", scripts.GlobalVariable{Name: "bulletcount", Kind: 4, Int: 6})
	setGlobal("handitem", scripts.GlobalVariable{Name: "handitem", Kind: 3, Text: "gun"})
	if progress.ScriptProps == nil {
		return
	}
	props := progress.ScriptProps
	found := false
	for index := range props.Props {
		if strings.EqualFold(props.Props[index].Name, "gunhand") {
			props.Props[index].Visible, props.Props[index].View = true, "idle"
			props.Props[index].AnchorX, props.Props[index].AnchorY = 260, 165
			found = true
		}
	}
	if !found {
		log.Printf("no gunhand prop in the save; the shot needs the target range's shop open")
	}
}

// holdItem puts a named inventory item in the player's hand and gives it to the player, the
// state a save needs before the item can be used (BOOTFILE's mousedown and an actor's offerobject
// read handitem).
func holdItem(progress *save.GameProgress, item string) {
	progress.HandItem = item
	if progress.InventoryOwners == nil {
		progress.InventoryOwners = map[string]string{}
	}
	progress.InventoryOwners[item] = "stranger"
	for index := range progress.ScriptGlobals {
		if progress.ScriptGlobals[index].Name == "handitem" {
			progress.ScriptGlobals[index] = scripts.GlobalVariable{Name: "handitem", Kind: 3, Text: item}
			return
		}
	}
	progress.ScriptGlobals = append(progress.ScriptGlobals, scripts.GlobalVariable{Name: "handitem", Kind: 3, Text: item})
}
