package story

import (
	"fmt"
	. "redust/scripts"
	. "redust/scripts/native"
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
	// FlatMouseActionQuit is NEW.FLT resource 31 (quit button): two
	// questiondialog prompts, a savegame and quit().
	FlatMouseActionQuit
	// FlatMouseActionHelp is NEW.FLT resource 26 (help button):
	// sendtostage(spotmovie("help.mov")) and then the flat's update loop.
	FlatMouseActionHelp
)

type FlatMouseContinuation struct {
	Action              FlatMouseAction
	FlatTarget          int
	GameName            string
	ReturnToCurrentFlat bool
	ReturnIdentifier    string
	// Prompts are the questiondialog texts of a quit handler, in script
	// order, and Movie is the spotmovie file of a help handler.
	Prompts      []string
	Movie        string
	VisualEffect uint16
	Duration     int
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
	if strings.EqualFold(handlerName, "quit") && flatMouseProgramCalls(program, "trackbut") {
		if continuation, found, err := parseFlatMouseQuit(program); err != nil || found {
			return continuation, found, err
		}
	}
	if strings.EqualFold(handlerName, "help") && flatMouseProgramCalls(program, "trackbut") {
		if movie, found := flatMouseSpotMovie(program); found {
			return FlatMouseContinuation{Action: FlatMouseActionHelp, FlatTarget: -1, Movie: movie}, true, nil
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

// parseFlatMouseQuit reads the quit button handler (NEW.FLT resource 31):
//
//	if questiondialog("Are you sure you want to quit?")=false  exitcode
//	if questiondialog("Save game before quitting?")  savegame("Dust 0.3")
//	quit()
//
// The prompts, the savegame name and the quit() call are taken from the
// handler's own records.
func parseFlatMouseQuit(program Program) (FlatMouseContinuation, bool, error) {
	continuation := FlatMouseContinuation{Action: FlatMouseActionQuit, FlatTarget: -1}
	quits := false
	for index, record := range program.Records {
		switch record.Kind {
		case LookupOpcode("quit"):
			quits = true
		case LookupOpcode("questiondialog"):
			if index+3 < len(program.Records) && program.Records[index+1].Kind == LookupOpcode("(") && program.Records[index+2].Kind == 3 {
				text, err := program.LiteralPascal(index + 2)
				if err != nil {
					return FlatMouseContinuation{}, false, fmt.Errorf("read quit prompt at record %d: %w", index, err)
				}
				continuation.Prompts = append(continuation.Prompts, string(text[1:]))
			}
		case LookupOpcode("savegame"):
			if index+3 < len(program.Records) && program.Records[index+2].Kind == 3 {
				name, err := program.LiteralPascal(index + 2)
				if err != nil {
					return FlatMouseContinuation{}, false, fmt.Errorf("read quit save name at record %d: %w", index, err)
				}
				continuation.GameName = string(name[1:])
			}
		}
	}
	if !quits {
		return FlatMouseContinuation{}, false, nil
	}
	return continuation, true, nil
}

// flatMouseSpotMovie finds the movie file literal a help handler passes to
// spotmovie through sendtostage.
func flatMouseSpotMovie(program Program) (string, bool) {
	sends := false
	for index, record := range program.Records {
		if record.Kind == LookupOpcode("sendtostage") {
			sends = true
		}
		if sends && record.Kind == 3 {
			text, err := program.LiteralPascal(index)
			if err == nil && strings.HasSuffix(strings.ToLower(string(text[1:])), ".mov") {
				return string(text[1:]), true
			}
		}
	}
	return "", false
}
