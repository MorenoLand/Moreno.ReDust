package scripts

import (
	"fmt"
	"strings"
)

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
