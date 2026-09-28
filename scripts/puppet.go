package scripts

import (
	"fmt"
	"image"
	"strings"

	"redust/assets"
)

func PuppetSpeechCalls(program Program, codeName string, stopOpcode uint16) ([]string, error) {
	start, err := FindCode(program, codeName)
	if err != nil {
		return nil, err
	}
	length, err := NextCodeOffset(program.Records, start)
	if err != nil {
		return nil, err
	}
	end := len(program.Records)
	if length >= 0 {
		end = start + int(length)
	}
	lines := []string{}
	for index := start + 2; index < end; index++ {
		record := program.Records[index]
		if record.Kind == stopOpcode {
			break
		}
		if record.Kind != LookupOpcode("puppetspeak") {
			continue
		}
		if index+3 >= end || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+2].Kind != 3 || program.Records[index+3].Kind != LookupOpcode(")") {
			return nil, fmt.Errorf("puppetspeak call at record %d does not match its literal call form", index)
		}
		literal, err := program.LiteralPascal(index + 2)
		if err != nil {
			return nil, err
		}
		lines = append(lines, string(literal[1:]))
		index += 3
	}
	return lines, nil
}

type PuppetChoice struct {
	Text    string
	EventID int32
}

func PuppetBevelChoices(program Program, codeName string) ([]PuppetChoice, error) {
	start, err := FindCode(program, codeName)
	if err != nil {
		return nil, err
	}
	length, err := NextCodeOffset(program.Records, start)
	if err != nil {
		return nil, err
	}
	end := len(program.Records)
	if length >= 0 {
		end = start + int(length)
	}
	choices := []PuppetChoice{}
	for index := start + 2; index < end; index++ {
		if program.Records[index].Kind == LookupOpcode("puppetevent") {
			break
		}
		if program.Records[index].Kind != LookupOpcode("puppetbevel") {
			continue
		}
		if index+5 >= end || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+2].Kind != 3 || program.Records[index+3].Kind != LookupOpcode(",") || program.Records[index+4].Kind != 4 || program.Records[index+5].Kind != LookupOpcode(")") {
			return nil, fmt.Errorf("puppetbevel call at record %d does not match its literal/event call form", index)
		}
		literal, err := program.LiteralPascal(index + 2)
		if err != nil {
			return nil, err
		}
		choices = append(choices, PuppetChoice{Text: assets.DecodePuppetText(literal[1:]), EventID: int32(program.Records[index+4].Data)})
		index += 5
	}
	return choices, nil
}

func PuppetBevelChoiceGroups(program Program, codeName string) ([][]PuppetChoice, error) {
	start, err := FindCode(program, codeName)
	if err != nil {
		return nil, err
	}
	length, err := NextCodeOffset(program.Records, start)
	if err != nil {
		return nil, err
	}
	end := len(program.Records)
	if length >= 0 {
		end = start + int(length)
	}
	groups, current := [][]PuppetChoice{}, []PuppetChoice{}
	for index := start + 2; index < end; index++ {
		if program.Records[index].Kind == LookupOpcode("puppetevent") {
			if len(current) > 0 {
				groups = append(groups, current)
				current = []PuppetChoice{}
			}
			continue
		}
		if program.Records[index].Kind != LookupOpcode("puppetbevel") {
			continue
		}
		if index+5 >= end || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+2].Kind != 3 || program.Records[index+3].Kind != LookupOpcode(",") || program.Records[index+4].Kind != 4 || program.Records[index+5].Kind != LookupOpcode(")") {
			return nil, fmt.Errorf("puppetbevel call at record %d does not match its literal/event call form", index)
		}
		literal, err := program.LiteralPascal(index + 2)
		if err != nil {
			return nil, err
		}
		current = append(current, PuppetChoice{Text: assets.DecodePuppetText(literal[1:]), EventID: int32(program.Records[index+4].Data)})
		index += 5
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups, nil
}

func PuppetEventSpeechCalls(program Program, codeName string, event int32) ([]string, error) {
	return PuppetEventSpeechCallsOccurrence(program, codeName, event, 0)
}

func PuppetEventSpeechCallsOccurrence(program Program, codeName string, event int32, occurrence int) ([]string, error) {
	if occurrence < 0 {
		return nil, fmt.Errorf("event %d occurrence %d is negative", event, occurrence)
	}
	start, err := FindCode(program, codeName)
	if err != nil {
		return nil, err
	}
	length, err := NextCodeOffset(program.Records, start)
	if err != nil {
		return nil, err
	}
	end := len(program.Records)
	if length >= 0 {
		end = start + int(length)
	}
	active, lines, matches := false, []string{}, [][]string{}
	for index := start + 2; index < end; index++ {
		record := program.Records[index]
		if record.Kind == LookupOpcode("case") {
			if active {
				matches = append(matches, lines)
				active, lines = false, nil
			}
			if index+1 < end && program.Records[index+1].Kind == 4 && int32(program.Records[index+1].Data) == event {
				active = true
				lines = []string{}
				index++
			}
			continue
		}
		if active && record.Kind == LookupOpcode("endswitch") {
			matches = append(matches, lines)
			active, lines = false, nil
			continue
		}
		if !active || record.Kind != LookupOpcode("puppetspeak") {
			continue
		}
		if index+3 >= end || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+2].Kind != 3 || program.Records[index+3].Kind != LookupOpcode(")") {
			return nil, fmt.Errorf("puppetspeak call at record %d does not match its literal call form", index)
		}
		literal, err := program.LiteralPascal(index + 2)
		if err != nil {
			return nil, err
		}
		lines = append(lines, string(literal[1:]))
		index += 3
	}
	if active {
		matches = append(matches, lines)
	}
	if occurrence >= len(matches) {
		return nil, fmt.Errorf("event %d occurrence %d has no case in %s", event, occurrence, codeName)
	}
	return matches[occurrence], nil
}

func PuppetBevelChoiceInCase(program Program, codeName, switchName, caseName string) (PuppetChoice, bool, error) {
	start, err := FindCode(program, codeName)
	if err != nil {
		return PuppetChoice{}, false, err
	}
	length, err := NextCodeOffset(program.Records, start)
	if err != nil {
		return PuppetChoice{}, false, err
	}
	end := len(program.Records)
	if length >= 0 {
		end = start + int(length)
	}
	for index := start + 2; index+1 < end; index++ {
		if program.Records[index].Kind != LookupOpcode("switch") || program.Records[index+1].Kind != 5 {
			continue
		}
		variable, err := program.IdentifierPascal(index + 1)
		if err != nil || !strings.EqualFold(string(variable[1:]), switchName) {
			continue
		}
		for caseIndex := index + 2; caseIndex < end; caseIndex++ {
			if program.Records[caseIndex].Kind == LookupOpcode("endswitch") {
				break
			}
			if program.Records[caseIndex].Kind != LookupOpcode("case") || caseIndex+1 >= end {
				continue
			}
			value, valueEnd, found := puppetCaseValue(program, caseIndex+1, end)
			if !found || !strings.EqualFold(value, caseName) {
				continue
			}
			for choiceIndex := valueEnd; choiceIndex < end; choiceIndex++ {
				if program.Records[choiceIndex].Kind == LookupOpcode("case") || program.Records[choiceIndex].Kind == LookupOpcode("endswitch") {
					break
				}
				if program.Records[choiceIndex].Kind != LookupOpcode("puppetbevel") {
					continue
				}
				if choiceIndex+5 >= end || program.Records[choiceIndex+1].Kind != LookupOpcode("(") || program.Records[choiceIndex+2].Kind != 3 || program.Records[choiceIndex+3].Kind != LookupOpcode(",") || program.Records[choiceIndex+4].Kind != 4 || program.Records[choiceIndex+5].Kind != LookupOpcode(")") {
					return PuppetChoice{}, false, fmt.Errorf("puppetbevel call at record %d does not match its literal/event call form", choiceIndex)
				}
				literal, err := program.LiteralPascal(choiceIndex + 2)
				if err != nil {
					return PuppetChoice{}, false, err
				}
				return PuppetChoice{Text: assets.DecodePuppetText(literal[1:]), EventID: int32(program.Records[choiceIndex+4].Data)}, true, nil
			}
		}
	}
	return PuppetChoice{}, false, nil
}

func puppetCaseValue(program Program, index, end int) (string, int, bool) {
	if index >= end {
		return "", index, false
	}
	if program.Records[index].Kind == 3 {
		value, err := program.LiteralPascal(index)
		return string(value[1:]), index + 1, err == nil
	}
	if program.Records[index].Kind == 4 {
		return fmt.Sprint(program.Records[index].Data), index + 1, true
	}
	if program.Records[index].Kind == LookupOpcode("-") && index+1 < end && program.Records[index+1].Kind == 4 {
		return fmt.Sprint(-int32(program.Records[index+1].Data)), index + 2, true
	}
	return "", index, false
}

func NativePuppetChoiceAt(point uint32, choices []PuppetChoice) (int32, bool) {
	position := image.Pt(int(int16(point>>16)), int(int16(point)))
	for index, choice := range choices {
		top := 264 + index*24
		if position.In(image.Rect(0, top, 512, top+24)) {
			return choice.EventID, true
		}
	}
	return 0, false
}

func LeroyBySignResponseCalls(event int32) ([]string, bool) {
	switch event {
	case 101:
		return []string{"leroy.45", "leroy.46", "leroy.47"}, true
	case 102:
		return []string{"leroy.48", "leroy.49"}, true
	case 103:
		return []string{"leroy.50", "leroy.51", "leroy.52"}, true
	case 104:
		return []string{"leroy.53", "leroy.54", "leroy.55"}, true
	default:
		return nil, false
	}
}

type PuppetResponseTransition struct {
	Repeats   bool
	Returns   bool
	SetsPhase bool
}

func LeroyBySignResponseTransition(event int32, day int) (PuppetResponseTransition, bool) {
	switch event {
	case 101:
		return PuppetResponseTransition{Repeats: day != 1, Returns: day == 1}, true
	case 102, 103:
		return PuppetResponseTransition{Repeats: true}, true
	case 104:
		return PuppetResponseTransition{Returns: true, SetsPhase: true}, true
	default:
		return PuppetResponseTransition{}, false
	}
}

type MarieGiftResponse struct {
	Speech  []string
	Counter int32
}

func MarieGift(what string, hankerchiefDegree int16, counter int32) (MarieGiftResponse, bool) {
	switch strings.ToLower(what) {
	case "sugarcubes":
		return MarieGiftResponse{Speech: []string{"marie.1", "marie.2"}, Counter: counter}, true
	case "flowers":
		return MarieGiftResponse{Speech: []string{"marie.3", "marie.4", "marie.5"}, Counter: counter}, true
	case "history":
		return MarieGiftResponse{Speech: []string{"marie.6"}, Counter: counter}, true
	case "hankerchief":
		if hankerchiefDegree == 0 {
			return MarieGiftResponse{Speech: []string{"marie.8", "marie.121"}, Counter: counter}, true
		}
	}
	switch counter {
	case 0:
		return MarieGiftResponse{Speech: []string{"marie.8"}, Counter: 1}, true
	case 1:
		return MarieGiftResponse{Speech: []string{"marie.9", "marie.8"}, Counter: 2}, true
	case 2:
		return MarieGiftResponse{Speech: []string{"marie.8", "marie.10"}, Counter: 0}, true
	default:
		return MarieGiftResponse{}, false
	}
}
