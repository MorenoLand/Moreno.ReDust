package story

import (
	. "redust/scripts"
	"fmt"
	"strings"

	"redust/assets"
)

// TrotterStoryState is the story state NativeTrotterSetTransition reads.
type TrotterStoryState struct {
	Day             int16
	Clock           int16
	TrotterPhase    int16
	Phase           int16
	TrotterActorSet string
	DocPhase        int16
}

// The set-open and set-close placement of Trotter. The placement itself is
// done by the shipped cast script (setupactor / putdownactor); this only
// chooses which one applies.
type TrotterActorTransition struct {
	Setup             string
	Hide              bool
	Direction         string
	SetGamePhase      int16
	SetGamePhaseValid bool
}

func NativeTrotterSetTransition(state TrotterStoryState, set string, opening bool) TrotterActorTransition {
	transition := TrotterActorTransition{}
	switch strings.ToLower(set) {
	case "sallower":
		if opening {
			if state.Day == 1 && state.TrotterPhase < 6 && state.Phase < 7 || state.Day == 2 && state.Clock == 2 && state.TrotterPhase == 3 {
				transition.Setup = "bar"
			}
			if state.Day == 3 && state.Clock == 3 && state.Phase == 3 {
				transition.Hide, transition.SetGamePhase, transition.SetGamePhaseValid = true, 4, true
			}
			if state.Day == 4 {
				transition.Setup = "day4am"
			}
		} else {
			transition.Hide = state.Day == 4 || state.Clock > 1 && !(state.Day == 1 && state.Phase != 7) && strings.EqualFold(state.TrotterActorSet, "sallower")
		}
	case "doctor2":
		if opening && state.Day == 3 && state.Clock == 2 && state.DocPhase == 8 {
			transition.Setup, transition.Direction = "inner", "south"
		}
		if !opening && strings.EqualFold(state.TrotterActorSet, "doctor2") {
			transition.Hide = true
		}
	}
	return transition
}

// loadProgramResources parses the listed script resources of one asset file.
func loadProgramResources(workspace assets.Workspace, asset string, resources []uint32) (map[uint32]Program, error) {
	cache, err := workspace.OpenResourceCache(asset)
	if err != nil {
		return nil, err
	}
	defer cache.Close()
	header, err := cache.Header()
	if err != nil {
		return nil, err
	}
	programs := make(map[uint32]Program, len(resources))
	for _, resource := range resources {
		if resource >= header.CountB {
			return nil, fmt.Errorf("%s resource %d exceeds %d entries", asset, resource, header.CountB)
		}
		lease, err := cache.Acquire(resource)
		if err != nil {
			return nil, fmt.Errorf("read %s resource %d: %w", asset, resource, err)
		}
		data, readErr := lease.Bytes()
		closeErr := lease.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s resource %d: %w", asset, resource, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s resource %d: %w", asset, resource, closeErr)
		}
		program, err := ParseProgram(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s resource %d: %w", asset, resource, err)
		}
		programs[resource] = program
	}
	return programs, nil
}
