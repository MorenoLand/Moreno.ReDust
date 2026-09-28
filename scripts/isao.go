package scripts

import "fmt"

type IsaoState struct {
	Day              int
	Clock            int
	Phase            int16
	TrotterPhase     int16
	OonaPhase        int16
	IsaoPhase        int16
	OonaActorValue   int32
	RingOwner        string
	ThunderbirdOwner string
}

type IsaoStep struct {
	Code              string
	Speech            []string
	NextCode          string
	NextGroup         int
	Finish            bool
	OpenInventory     bool
	SetIsaoPhase      int16
	SetIsaoPhaseValid bool
}

func IsaoEntry(state IsaoState) IsaoStep {
	if state.IsaoPhase == 999 {
		return IsaoStep{Speech: []string{"happy2"}, SetIsaoPhase: 0, SetIsaoPhaseValid: true, Finish: true}
	}
	if state.Day == 3 && state.Clock == 1 {
		return IsaoStep{Code: "ring"}
	}
	if state.OonaActorValue < 1 {
		return IsaoStep{Code: "whoareyou"}
	}
	if state.IsaoPhase == 1 {
		return IsaoStep{Code: "brushoff"}
	}
	return IsaoStep{Code: "runyoself"}
}

func IsaoPuppetResponse(code string, event int32, state IsaoState, randomChoice uint32) (IsaoStep, error) {
	if event == -1 {
		return IsaoStep{Finish: true}, nil
	}
	switch code {
	case "whoareyou":
		switch event {
		case 101:
			return IsaoStep{Speech: []string{"bored1"}, NextCode: "whoareyou", NextGroup: 1}, nil
		case 102:
			response := []string{"no1", "no2"}
			if randomChoice == 2 {
				response = []string{"happy1", "happy2"}
			} else if randomChoice != 1 && randomChoice != 3 {
				return IsaoStep{}, fmt.Errorf("Isao whoareyou random result %d is outside 1..3", randomChoice)
			}
			return IsaoStep{Speech: append(response, "bored1", "bye1"), Finish: true}, nil
		}
	case "runyoself":
		if event == 55555 {
			return IsaoStep{OpenInventory: true}, nil
		}
		if event != 101 {
			break
		}
		var speech []string
		switch state.Clock {
		case 1:
			return IsaoStep{}, fmt.Errorf("Isao runyoself event 101 reaches native error() at day %d clock 1", state.Day)
		case 2:
			if state.Day == 2 && state.Phase == 1 {
				speech = append(speech, "ominous1", "ominous2")
			}
			if state.Day == 2 && state.TrotterPhase == 3 {
				speech = append(speech, "ominous1", "ominous2")
			}
			if state.Day == 2 || state.Day == 3 {
				speech = append(speech, "sad1", "sad2", "sad3")
			}
		case 3:
			if state.Day == 2 {
				speech = append(speech, "happy1", "happy2")
			}
			if state.Day == 3 && state.ThunderbirdOwner == "stranger" {
				speech = append(speech, "happy1", "happy2")
			} else {
				speech = append(speech, "ominous1", "ominous2")
			}
		case 4:
			if state.Day == 1 && state.OonaPhase == 1 {
				speech = append(speech, "happy1", "happy2")
			} else {
				speech = append(speech, "sad1", "sad2", "sad3")
			}
		}
		return IsaoStep{Speech: speech, NextCode: "byenow", NextGroup: 0}, nil
	case "byenow":
		if event == 101 {
			return IsaoStep{Speech: []string{"bye1"}, Finish: true}, nil
		}
	case "ring":
		if event == 101 {
			return IsaoStep{Speech: []string{"newsong"}, Finish: true}, nil
		}
	case "brushoff":
		if event == 101 {
			return IsaoStep{Finish: true}, nil
		}
	}
	return IsaoStep{}, fmt.Errorf("Isao PUP %q does not handle event %d", code, event)
}
