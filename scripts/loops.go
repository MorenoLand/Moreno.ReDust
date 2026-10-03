package scripts

import (
	"fmt"
	"strings"
)

// Native loop kinds, from FUN_004112A0, which maps makeloop/stoploop's first
// argument: "actor" 1, "prop" 2, "scene" 3, "flat" 4; any other name is
// status 10.
const (
	LoopKindActor uint16 = 1
	LoopKindProp  uint16 = 2
	LoopKindScene uint16 = 3
	LoopKindFlat  uint16 = 4
)

// LoopKindByName reproduces FUN_004112A0's case-insensitive mapping.
func LoopKindByName(name string) (uint16, bool) {
	switch strings.ToLower(name) {
	case "actor":
		return LoopKindActor, true
	case "prop":
		return LoopKindProp, true
	case "scene":
		return LoopKindScene, true
	case "flat":
		return LoopKindFlat, true
	}
	return 0, false
}

// LoopKindName is the inverse of LoopKindByName.
func LoopKindName(kind uint16) string {
	switch kind {
	case LoopKindActor:
		return "actor"
	case LoopKindProp:
		return "prop"
	case LoopKindScene:
		return "scene"
	case LoopKindFlat:
		return "flat"
	}
	return ""
}

// MigrateLegacyKinds rewrites loops saved before the native kind numbers were
// adopted, when actor loops were stored as 2 and scene loops as 1.
func (s *LoopSchedulerState) MigrateLegacyKinds() {
	for _, slot := range s.Slots {
		if slot == nil {
			continue
		}
		switch slot.Kind {
		case 2:
			slot.Kind = LoopKindActor
		case 1:
			slot.Kind = LoopKindScene
		}
	}
}

type ScriptLoop struct {
	Kind       uint16 `json:"kind"`
	Owner      string `json:"owner"`
	Callback   string `json:"callback"`
	Remaining  int32  `json:"remaining"`
	Paused     bool   `json:"paused"`
	PauseDepth int16  `json:"pauseDepth,omitempty"`
}

type LoopSchedulerState struct {
	Slots []*ScriptLoop `json:"slots"`
}

type LoopScheduler struct {
	slots [32]*ScriptLoop
}

type LoopDispatch func(ScriptLoop) (uint16, error)

func (s *LoopScheduler) Register(loop ScriptLoop) uint16 {
	if s == nil {
		return 0x23
	}
	for i, slot := range s.slots {
		if slot == nil {
			slots := loop
			s.slots[i] = &slots
			return 0
		}
	}
	return 0x23
}

func (s *LoopScheduler) Pass(dispatch LoopDispatch) (uint16, error) {
	return s.PassWhere(dispatch, nil)
}

func (s *LoopScheduler) PassWhere(dispatch LoopDispatch, service func(ScriptLoop) bool) (uint16, error) {
	if s == nil {
		return 0, fmt.Errorf("loop scheduler is unavailable")
	}
	if dispatch == nil {
		return 0, fmt.Errorf("loop scheduler dispatcher is unavailable")
	}
	for i, slot := range s.slots {
		if slot == nil || slot.Paused || slot.PauseDepth != 0 || service != nil && !service(*slot) {
			continue
		}
		slot.Remaining--
		if slot.Remaining >= 1 {
			continue
		}
		current := *slot
		s.slots[i] = nil
		status, err := dispatch(current)
		if err != nil {
			return 0, err
		}
		if status != 0 {
			s.slots[i] = nil
			return status, nil
		}
	}
	return 0, nil
}

func (s *LoopScheduler) Len() int {
	if s == nil {
		return 0
	}
	count := 0
	for _, slot := range s.slots {
		if slot != nil {
			count++
		}
	}
	return count
}

func (s *LoopScheduler) Stop(kind uint16, owner string) {
	if s == nil {
		return
	}
	all := strings.EqualFold(owner, "all")
	for index, slot := range s.slots {
		if slot != nil && slot.Kind == kind && (all || strings.EqualFold(slot.Owner, owner)) {
			s.slots[index] = nil
			if !all {
				return
			}
		}
	}
}

func (s *LoopScheduler) SetPaused(kind uint16, owner string, paused bool) {
	if s == nil {
		return
	}
	all := strings.EqualFold(owner, "all")
	for _, slot := range s.slots {
		if slot == nil || slot.Kind != kind || !all && !strings.EqualFold(slot.Owner, owner) {
			continue
		}
		if slot.PauseDepth == 0 && slot.Paused {
			slot.PauseDepth = 1
		}
		if paused {
			slot.PauseDepth++
		} else {
			slot.PauseDepth--
			if slot.PauseDepth < 0 {
				slot.PauseDepth = 0
			}
		}
		slot.Paused = slot.PauseDepth != 0
		if !all {
			return
		}
	}
}

func (s *LoopScheduler) Snapshot() LoopSchedulerState {
	state := LoopSchedulerState{Slots: make([]*ScriptLoop, 32)}
	if s != nil {
		for index, slot := range s.slots {
			if slot != nil {
				copy := *slot
				if copy.Paused && copy.PauseDepth == 0 {
					copy.PauseDepth = 1
				}
				state.Slots[index] = &copy
			}
		}
	}
	return state
}

func (state LoopSchedulerState) Validate() error {
	if len(state.Slots) != 32 {
		return fmt.Errorf("loop snapshot has %d slots, want 32", len(state.Slots))
	}
	for index, slot := range state.Slots {
		if slot != nil && (slot.Kind > 4 || len(slot.Owner) > 15 || len(slot.Callback) > 15 || !slot.Paused && slot.PauseDepth != 0) {
			return fmt.Errorf("loop snapshot slot %d is invalid", index)
		}
	}
	return nil
}

func (s *LoopScheduler) Restore(state LoopSchedulerState) error {
	if s == nil {
		return fmt.Errorf("loop scheduler is unavailable")
	}
	if err := state.Validate(); err != nil {
		return err
	}
	restored := LoopScheduler{}
	for index, slot := range state.Slots {
		if slot != nil {
			copy := *slot
			if copy.Paused && copy.PauseDepth == 0 {
				copy.PauseDepth = 1
			}
			restored.slots[index] = &copy
		}
	}
	*s = restored
	return nil
}
