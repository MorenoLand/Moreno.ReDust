package scripts

import (
	"fmt"
	"image"

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
