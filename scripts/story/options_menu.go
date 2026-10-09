package story

import (
	"fmt"
	"strings"

	. "redust/scripts"
)

// OptionsActionKind names the NEW.FLT options-flat handlers that act on the
// flat itself rather than leaving it.
type OptionsActionKind uint8

const (
	OptionsActionNone OptionsActionKind = iota
	// OptionsActionSubtitles is resource 42: it flips puppetparam(7) and shows
	// or hides the "check" prop at propxy("check", 307, 132).
	OptionsActionSubtitles
	// OptionsActionKeySelect is resources 27, 29 and 30: seldir=me,
	// propxy("keysel", x, y) and a flat "update" loop.
	OptionsActionKeySelect
	// OptionsActionCredits is resource 28, whose runcredits() opens
	// credits.flt and credits.prp.
	OptionsActionCredits
)

// OptionsAction is one options-flat handler read from its script records.
type OptionsAction struct {
	Kind OptionsActionKind
	// Param is the puppetparam slot a subtitles handler flips.
	Param int
	// Prop, X and Y are the prop and position the handler moves.
	Prop string
	X, Y int
	// Direction is `me` for a key box: "north", "east" or "west".
	Direction string
}

// ParseOptionsAction reads a button handler of the options flat. found is
// false for handlers it does not own.
func ParseOptionsAction(program Program, handlerName string) (OptionsAction, bool, error) {
	name := strings.ToLower(handlerName)
	switch name {
	case "subtitles":
		action := OptionsAction{Kind: OptionsActionSubtitles}
		for index, record := range program.Records {
			switch record.Kind {
			case LookupOpcode("puppetparam"):
				if index+2 < len(program.Records) && program.Records[index+2].Kind == 4 && action.Param == 0 {
					action.Param = int(program.Records[index+2].Data)
				}
			case LookupOpcode("propxy"):
				prop, x, y, ok, err := optionsPropXY(program, index)
				if err != nil {
					return OptionsAction{}, false, err
				}
				if ok && strings.EqualFold(prop, "check") {
					action.Prop, action.X, action.Y = prop, x, y
				}
			}
		}
		if action.Param == 0 || action.Prop == "" {
			return OptionsAction{}, false, fmt.Errorf("subtitles handler has no puppetparam slot or check position")
		}
		return action, true, nil
	case "north", "east", "west":
		action := OptionsAction{Kind: OptionsActionKeySelect, Direction: name}
		for index, record := range program.Records {
			if record.Kind != LookupOpcode("propxy") {
				continue
			}
			prop, x, y, ok, err := optionsPropXY(program, index)
			if err != nil {
				return OptionsAction{}, false, err
			}
			if ok && strings.EqualFold(prop, "keysel") {
				action.Prop, action.X, action.Y = prop, x, y
			}
		}
		if action.Prop == "" {
			return OptionsAction{}, false, nil
		}
		return action, true, nil
	case "credits":
		for index, record := range program.Records {
			if record.Kind != 5 {
				continue
			}
			identifier, err := program.IdentifierPascal(index)
			if err == nil && len(identifier) > 1 && strings.EqualFold(string(identifier[1:]), "runcredits") {
				return OptionsAction{Kind: OptionsActionCredits}, true, nil
			}
		}
	}
	return OptionsAction{}, false, nil
}

// optionsPropXY reads propxy("prop", x, y) at records[index].
func optionsPropXY(program Program, index int) (prop string, x, y int, ok bool, err error) {
	records := program.Records
	if index+7 >= len(records) || records[index+1].Kind != LookupOpcode("(") || records[index+2].Kind != 3 || records[index+4].Kind != 4 || records[index+6].Kind != 4 {
		return "", 0, 0, false, nil
	}
	text, err := program.LiteralPascal(index + 2)
	if err != nil {
		return "", 0, 0, false, err
	}
	return string(text[1:]), int(int32(records[index+4].Data)), int(int32(records[index+6].Data)), true, nil
}

// OptionsButtonBevels are the butbevel positions trackbut() (NEW.FLT
// resource 11) puts under the pressed button, by the button's handler name.
// The same flat's slider is positioned by menuvolume (BOOTFILE resource 1).
var OptionsButtonBevels = map[string][2]int{
	"save": {252, 198},
	"open": {252, 229},
	"quit": {252, 260},
	"help": {252, 291},
	"ok":   {252, 322},
}
