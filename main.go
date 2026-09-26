package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"redust/assets"
)

func run() error {
	work := flag.String("work", "bin", "work directory; assets are read from its assets child")
	debug := flag.Bool("debug", false, "enable diagnostics")
	flag.Parse()
	workspace, err := assets.NewWorkspace(*work)
	if err != nil {
		return fmt.Errorf("initialize work directory: %w", err)
	}
	if *debug {
		log.Printf("work=%s assets=%s", workspace.WorkDir, workspace.AssetRoot)
	}
	boot, err := workspace.OpenContainer("BootFile")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("user game files are required under %s; BootFile was not found: %w", workspace.AssetRoot, err)
		}
		return fmt.Errorf("open BootFile: %w", err)
	}
	defer boot.Close()
	if *debug {
		header := boot.Header()
		log.Printf("BootFile APPL header: entries=%d pages=%d size=%d", header.CountB, header.CountA>>7, header.FileSize)
	}
	return errors.New("BootFile loaded, but its verified script-to-scene startup path is still being translated")
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
