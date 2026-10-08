package story

import (
	"encoding/binary"
	"fmt"

	"redust/assets"
)

type AdvanceDayState struct {
	Day, Clock      int
	Phase           int16
	CurrentSet      string
	InventoryOwners map[string]string
}
type AdvanceDayRoute struct {
	Day, Clock                                                                       int
	Phase                                                                            int16
	Movie, SetName, ViewName, Direction, TownReturnScene, SavedScene, SavedDirection string
	Premovie, ResetDayOne, OpenFight, FightOn, GiveChest                             bool
}

func NativeAdvanceClockFields(clock, day, phase int) (int, int, int) {
	phase = 0
	if clock < 3 {
		return clock + 1, day, phase
	}
	return 1, day + 1, phase
}

func NativeAdvanceDayRoute(state AdvanceDayState) (AdvanceDayRoute, error) {
	clock, day, phase := NativeAdvanceClockFields(state.Clock, state.Day, int(state.Phase))
	route := AdvanceDayRoute{Day: day, Clock: clock, Phase: int16(phase), TownReturnScene: "Scene G5", SavedScene: "Scene C4", SavedDirection: "east"}
	if state.Day < 1 || state.Day > 5 || state.Clock < 1 || state.Clock > 3 || day > 5 {
		return AdvanceDayRoute{}, fmt.Errorf("native day advancement is unavailable at day%d clock%d", state.Day, state.Clock)
	}
	switch day {
	case 1:
		route.Clock, route.ResetDayOne, route.SetName, route.ViewName, route.Direction = 3, true, "nite.set", "Scene G15", "north"
	case 2:
		switch clock {
		case 1:
			route.Movie, route.SetName, route.ViewName, route.Direction = "MOVIES/D1ND2M.MOV", "hotroom.set", "Scene A1", "east"
			if state.CurrentSet == "mayroom" {
				route.SetName, route.ViewName, route.Direction, route.TownReturnScene, route.SavedScene, route.SavedDirection = "mayroom.set", "Scene B2", "west", "Scene J9", "Scene B1", "south"
			}
		case 2:
			route.Movie, route.SetName, route.Premovie = "MOVIES/D2MD2A.MOV", "town.set", true
		case 3:
			route.Movie, route.SetName, route.ViewName, route.Direction, route.TownReturnScene, route.Premovie = "MOVIES/D2AD2N.MOV", "jail.set", "Scene A1", "east", "Scene G12", true
		}
	case 3:
		switch clock {
		case 1:
			route.Movie, route.SetName, route.ViewName, route.Direction = "MOVIES/D2ND3M.MOV", "hotroom.set", "Scene A1", "east"
		case 2:
			route.Movie, route.SetName, route.Premovie = "MOVIES/D3MD3A.MOV", "town.set", true
		case 3:
			route.Movie, route.SetName, route.Premovie = "MOVIES/D3AD3N.MOV", "nite.set", true
		}
	case 4:
		switch clock {
		case 1:
			route.Movie, route.SetName, route.ViewName, route.Direction = "MOVIES/D3ND4M.MOV", "hotroom.set", "Scene A1", "east"
		case 2:
			route.SetName, route.OpenFight = "town.set", true
		case 3:
			route.Movie, route.SetName, route.Premovie, route.FightOn = "MOVIES/D4AD4N.MOV", "nite.set", true, true
		}
	case 5:
		route.Movie, route.SetName, route.ViewName, route.Direction, route.GiveChest = "MOVIES/D4ND5M.MOV", "town.set", "Scene G4", "south", true
	}
	return route, nil
}

func NativeCanAdvanceClock(state AdvanceDayState) (bool, error) {
	owner := state.InventoryOwners
	switch state.Day {
	case 1:
		if state.Clock == 3 {
			return false, nil
		}
	case 2:
		if state.Clock == 1 {
			count := 0
			for _, name := range []string{"gun", "boots", "bullets"} {
				if owner[name] == "stranger" {
					count++
				}
			}
			return count > 1, nil
		}
		if state.Clock == 2 || state.Clock == 3 {
			return false, nil
		}
	case 3:
		if state.Clock == 1 {
			return owner["ring"] == "jones" && owner["pages"] == "stranger", nil
		}
		if state.Clock == 2 {
			return owner["mask"] == "stranger" && owner["yunnibook"] == "stranger" && owner["flute"] == "stranger", nil
		}
		if state.Clock == 3 {
			return false, nil
		}
	case 4, 5:
		return false, nil
	}
	return false, fmt.Errorf("native canadvance rejects day%d clock%d", state.Day, state.Clock)
}

func NativeSetInitialView(workspace assets.Workspace, name string) (string, int16, error) {
	set, err := workspace.OpenSet(name)
	if err != nil {
		return "", 0, err
	}
	defer set.Close()
	data, err := set.Resource(0)
	if err != nil {
		return "", 0, err
	}
	if len(data) < 0x36 {
		return "", 0, fmt.Errorf("SET default position is truncated")
	}
	cell, scene, direction := binary.LittleEndian.Uint16(data[0x30:0x32]), binary.LittleEndian.Uint16(data[0x32:0x34]), int16(binary.LittleEndian.Uint16(data[0x34:0x36]))
	view, found := set.FindViewByIDs(cell, scene)
	if !found || !view.IsCell() || direction < 1 || direction > 4 {
		return "", 0, fmt.Errorf("SET %s initial position %d,%d,%d is unavailable", name, cell, scene, direction)
	}
	return string(view.Name[1:]), direction, nil
}
