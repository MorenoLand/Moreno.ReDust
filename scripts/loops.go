package scripts

import "fmt"

type ScriptLoop struct {
	Kind      uint16
	Owner     string
	Callback  string
	Remaining int32
	Paused    bool
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
	if s == nil {
		return 0, fmt.Errorf("loop scheduler is unavailable")
	}
	if dispatch == nil {
		return 0, fmt.Errorf("loop scheduler dispatcher is unavailable")
	}
	for i, slot := range s.slots {
		if slot == nil || slot.Paused {
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
