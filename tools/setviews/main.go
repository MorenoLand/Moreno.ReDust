// setviews prints the views of one SET file: each scene's name, its scene and direction
// ids, and its flags. It is a development aid for choosing a save point in a set.
//
// Usage: go run ./tools/setviews -work=bin DATA/COURT.SET
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"redust/assets"
)

func main() {
	work := flag.String("work", "bin", "work directory; assets are read from its assets child")
	rows := flag.Bool("rows", false, "list the background rows (from, to, resource) instead of the views")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: setviews [-work=dir] [-rows] DATA/NAME.SET")
		os.Exit(2)
	}
	workspace, err := assets.NewWorkspace(*work)
	if err != nil {
		log.Fatal(err)
	}
	set, err := workspace.OpenSet(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	defer set.Close()
	if *rows {
		for _, row := range set.BackgroundRows() {
			fmt.Printf("from=%v to=%v resource=%d\n", row.From, row.To, row.Resource)
		}
		return
	}
	for _, view := range set.Views() {
		fmt.Printf("%-16s scene=%d direction=%d flags=%#x resource=%d\n",
			string(view.Name[1:]), view.SceneID, view.DirectionID, view.Flags, view.Resource)
	}
}
