package scripts

import (
	"fmt"
	"strings"
)

type FlatMouseStatus uint8

const (
	FlatMouseInactive FlatMouseStatus = iota
	FlatMousePending
	FlatMouseCancel
	FlatMouseResume
)

type FlatMouseAction uint8

const (
	FlatMouseActionNone FlatMouseAction = iota
	FlatMouseActionSaveGame
	FlatMouseActionOpenGame
	FlatMouseActionGoToFlat
	FlatMouseActionExamineInventory
)

type FlatMouseContinuation struct {
	Action              FlatMouseAction
	FlatTarget          int
	GameName            string
	ReturnToCurrentFlat bool
	ReturnIdentifier    string
	VisualEffect        uint16
	Duration            int
}

type FlatMouseSession struct {
	Target        string
	PressPoint    uint32
	PointerPoint  uint32
	PointerInside bool
	Status        FlatMouseStatus
	continuation  FlatMouseContinuation
}

func NewFlatMouseSession(target string, pressPoint uint32, continuation FlatMouseContinuation) FlatMouseSession {
	return FlatMouseSession{Target: target, PressPoint: pressPoint, PointerPoint: pressPoint, PointerInside: true, Status: FlatMousePending, continuation: continuation}
}

func (s *FlatMouseSession) Advance(point uint32, leftDown, leftReleased, insideTarget bool) FlatMouseStatus {
	if s == nil {
		return FlatMouseInactive
	}
	if s.Status != FlatMousePending {
		return s.Status
	}
	s.PointerPoint, s.PointerInside = point, insideTarget
	if leftReleased || !leftDown {
		if insideTarget {
			s.Status = FlatMouseResume
		} else {
			s.Status = FlatMouseCancel
		}
	}
	return s.Status
}

func (s *FlatMouseSession) Resume() (FlatMouseContinuation, bool) {
	if s == nil || s.Status != FlatMouseResume {
		return FlatMouseContinuation{}, false
	}
	return s.continuation, true
}

func ParseFlatMouseContinuation(program Program, handlerName string) (FlatMouseContinuation, bool, error) {
	if strings.EqualFold(handlerName, "info") && flatMouseProgramCalls(program, "trackbut") {
		for index, record := range program.Records {
			if record.Kind != LookupOpcode("sendtoprop") || index+7 >= len(program.Records) {
				continue
			}
			item, itemErr := program.IdentifierPascal(index + 2)
			call, callErr := program.IdentifierPascal(index + 4)
			if itemErr == nil && callErr == nil && strings.EqualFold(string(item[1:]), "handitem") && strings.EqualFold(string(call[1:]), "infoyoself") && program.Records[index+1].Kind == LookupOpcode("(") && program.Records[index+3].Kind == LookupOpcode(",") && program.Records[index+5].Kind == LookupOpcode("(") && program.Records[index+6].Kind == LookupOpcode(")") && program.Records[index+7].Kind == LookupOpcode(")") {
				return FlatMouseContinuation{Action: FlatMouseActionExamineInventory, FlatTarget: -1}, true, nil
			}
		}
	}
	continuation, found, err := parseFlatMouseGameAction(program)
	if err != nil {
		return FlatMouseContinuation{}, false, err
	}
	if found {
		expectedHandler := "save"
		if continuation.Action == FlatMouseActionOpenGame {
			expectedHandler = "open"
		}
		if strings.EqualFold(handlerName, expectedHandler) && flatMouseProgramCalls(program, "trackbut") {
			// The return-to-captured-flat behaviour is read from the handler itself
			// rather than assumed from the action: resource 24 (save) ends with a
			// dynamic gotoflat, resource 25 (open) has no gotoflat at all.
			action, _, actionErr := MouseDownFlatAction(program)
			if actionErr != nil {
				return FlatMouseContinuation{}, false, actionErr
			}
			continuation.ReturnIdentifier = action.ReturnIdentifier
			continuation.ReturnToCurrentFlat = action.ReturnIdentifier != ""
			return continuation, true, nil
		}
		return FlatMouseContinuation{}, false, nil
	}
	if !strings.EqualFold(handlerName, "ok") {
		return FlatMouseContinuation{}, false, nil
	}
	action, found, err := MouseDownFlatAction(program)
	if err != nil {
		return FlatMouseContinuation{}, false, err
	}
	if !found {
		return FlatMouseContinuation{Action: FlatMouseActionGoToFlat, FlatTarget: 0, VisualEffect: LookupOpcode("barndoorclose"), Duration: 30}, true, nil
	}
	return FlatMouseContinuation{Action: FlatMouseActionGoToFlat, FlatTarget: action.FlatTarget, VisualEffect: action.VisualEffect, Duration: action.Duration}, true, nil
}

func parseFlatMouseGameAction(program Program) (FlatMouseContinuation, bool, error) {
	for index, record := range program.Records {
		action := FlatMouseActionNone
		switch record.Kind {
		case LookupOpcode("savegame"):
			action = FlatMouseActionSaveGame
		case LookupOpcode("opengame"):
			action = FlatMouseActionOpenGame
		default:
			continue
		}
		if index+3 >= len(program.Records) || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+2].Kind != 3 || program.Records[index+3].Kind != LookupOpcode(")") {
			return FlatMouseContinuation{}, false, fmt.Errorf("game save opcode at record %d has an unsupported argument", index)
		}
		name, err := program.LiteralPascal(index + 2)
		if err != nil {
			return FlatMouseContinuation{}, false, fmt.Errorf("read game save name at record %d: %w", index, err)
		}
		if len(name) < 2 || strings.TrimSpace(string(name[1:])) == "" {
			return FlatMouseContinuation{}, false, fmt.Errorf("game save opcode at record %d has an empty name", index)
		}
		return FlatMouseContinuation{Action: action, FlatTarget: -1, GameName: string(name[1:])}, true, nil
	}
	return FlatMouseContinuation{}, false, nil
}

func flatMouseProgramCalls(program Program, name string) bool {
	for index, record := range program.Records {
		if record.Kind != 5 {
			continue
		}
		identifier, err := program.IdentifierPascal(index)
		if err == nil && len(identifier) > 1 && strings.EqualFold(string(identifier[1:]), name) {
			return true
		}
	}
	return false
}
