package scripts

import (
	"fmt"
	"image"
	"strings"

	"redust/assets"
)

type DeathSequence struct {
	Cause, Movie, SoundBank, FlatName        string
	Narration                                []string
	DelayFrames, FadeOutFrames, FadeInFrames int
	StopLoops                                []string
}

type DeathButtonKind uint8

const (
	DeathButtonNew DeathButtonKind = iota + 1
	DeathButtonOpen
	DeathButtonQuit
	DeathButtonHelp
	DeathButtonGun
)

type DeathButtonAction struct {
	Kind                                          DeathButtonKind
	Continuation                                  FlatMouseContinuation
	Movie, GameName, SaveQuestion, Prop, Sound    string
	FadeFrames, StartDay, StartClock, DelayFrames int
	Degree                                        int16
	Anchor                                        image.Point
}

func NewDeathSequence(workspace assets.Workspace, cause string, random *NativeRandom) (DeathSequence, error) {
	programs, err := loadProgramResources(workspace, "DATA/NEW.FLT", []uint32{14})
	if err != nil {
		return DeathSequence{}, err
	}
	return ParseDeathSequence(programs[14], cause, random)
}

func ParseDeathSequence(program Program, cause string, random *NativeRandom) (DeathSequence, error) {
	sequence := DeathSequence{Cause: cause}
	start, end, err := deathCodeRange(program, "death")
	if err != nil {
		return DeathSequence{}, err
	}
	for index := start; index < end; index++ {
		switch program.Records[index].Kind {
		case LookupOpcode("delay"):
			sequence.DelayFrames, err = deathNumberArgument(program, index+2)
		case LookupOpcode("screentoblack"):
			sequence.FadeOutFrames, err = deathNumberArgument(program, index+4)
		case LookupOpcode("blacktoscreen"):
			sequence.FadeInFrames, err = deathNumberArgument(program, index+4)
		case LookupOpcode("gotoflat"):
			sequence.FlatName, err = deathStringArgument(program, index+2)
		case LookupOpcode("stoploop"):
			kind, kindErr := deathStringArgument(program, index+2)
			owner, ownerErr := deathStringArgument(program, index+4)
			if kindErr != nil || ownerErr != nil || owner != "all" {
				return DeathSequence{}, fmt.Errorf("death stoploop at record %d is unsupported", index)
			}
			sequence.StopLoops = append(sequence.StopLoops, kind)
		}
		if err != nil {
			return DeathSequence{}, err
		}
	}
	if sequence.DelayFrames < 0 || sequence.FadeOutFrames < 1 || sequence.FadeInFrames < 1 || sequence.FlatName == "" {
		return DeathSequence{}, fmt.Errorf("death sequence lacks valid delays or flat")
	}
	start, end, err = deathCodeRange(program, "narrcomm")
	if err != nil {
		return DeathSequence{}, err
	}
	selected, bodySeen, matched, done := false, false, false, false
	for index := start; index < end && !done; index++ {
		switch program.Records[index].Kind {
		case LookupOpcode("opentrackfile"):
			bank, err := deathStringArgument(program, index+2)
			if err != nil {
				return DeathSequence{}, err
			}
			sequence.SoundBank = "DATA/" + strings.ToUpper(bank)
		case LookupOpcode("case"):
			if bodySeen {
				if selected {
					done = true
					continue
				}
				selected, bodySeen = false, false
			}
			name, err := deathStringArgument(program, index+1)
			if err != nil {
				return DeathSequence{}, err
			}
			if name == cause {
				selected, matched = true, true
			}
		case LookupOpcode("error"):
			bodySeen = true
			if selected {
				return DeathSequence{}, fmt.Errorf("native death narration rejects cause %q", cause)
			}
		case 5:
			name, err := program.IdentifierPascal(index)
			if err != nil || string(name[1:]) != "voiceone" {
				continue
			}
			bodySeen = true
			if selected {
				voice, err := deathStringArgument(program, index+2)
				if err != nil {
					return DeathSequence{}, err
				}
				sequence.Narration = append(sequence.Narration, voice)
			}
		}
		if bodySeen && selected && index+1 < end && program.Records[index+1].Kind == LookupOpcode("endswitch") {
			break
		}
	}
	if !matched || len(sequence.Narration) == 0 || sequence.SoundBank == "" {
		return DeathSequence{}, fmt.Errorf("death narration has no supported cause %q", cause)
	}
	sequence.Movie, err = deathMovieForCause(program, cause, random)
	if err != nil {
		return DeathSequence{}, err
	}
	return sequence, nil
}

func deathMovieForCause(program Program, cause string, random *NativeRandom) (string, error) {
	start, end, err := deathCodeRange(program, "deathmovie")
	if err != nil {
		return "", err
	}
	var movies []string
	hanging := map[uint32]string{}
	for index := start; index < end; index++ {
		if program.Records[index].Kind == LookupOpcode("if") && index+3 < end && program.Records[index+1].Kind == 5 && program.Records[index+2].Kind == LookupOpcode("=") && program.Records[index+3].Kind == 3 {
			name, err := program.IdentifierPascal(index + 1)
			if err != nil || string(name[1:]) != "playerdeath" {
				continue
			}
			value, err := deathStringArgument(program, index+3)
			if err != nil {
				return "", err
			}
			if value == cause {
				for at := index + 4; at < end && program.Records[at].Kind != LookupOpcode("endif"); at++ {
					if program.Records[at].Kind == LookupOpcode("playmovie") {
						movie, err := deathStringArgument(program, at+2)
						return "MOVIES/" + strings.ToUpper(movie), err
					}
				}
			}
		}
		if program.Records[index].Kind == LookupOpcode("playmovie") {
			movie, err := deathStringArgument(program, index+2)
			if err != nil {
				return "", err
			}
			movies = append(movies, movie)
		}
		if program.Records[index].Kind == LookupOpcode("case") && index+1 < end && program.Records[index+1].Kind == 4 {
			for at := index + 2; at < end && program.Records[at].Kind != LookupOpcode("case") && program.Records[at].Kind != LookupOpcode("endswitch"); at++ {
				if program.Records[at].Kind == LookupOpcode("playmovie") {
					movie, err := deathStringArgument(program, at+2)
					if err != nil {
						return "", err
					}
					hanging[program.Records[index+1].Data] = movie
					break
				}
			}
		}
	}
	if strings.HasPrefix(cause, "shot ") {
		if random == nil {
			return "", fmt.Errorf("death movie for %q requires native random", cause)
		}
		if len(hanging) != 3 {
			return "", fmt.Errorf("native death script has %d hanging movies, want 3", len(hanging))
		}
		movie, found := hanging[random.Inclusive(3)]
		if !found {
			return "", fmt.Errorf("native hanging movie case is missing")
		}
		return "MOVIES/" + strings.ToUpper(movie), nil
	}
	if !strings.HasPrefix(cause, "by ") || len(movies) == 0 {
		return "", fmt.Errorf("native death movie rejects cause %q", cause)
	}
	return "MOVIES/" + strings.ToUpper(movies[len(movies)-1]), nil
}

func ParseDeathButtonAction(program Program, handler string, debugging bool) (DeathButtonAction, bool, error) {
	if handler == "skull" {
		return DeathButtonAction{}, false, nil
	}
	start, end, err := deathCodeRange(program, "mousedown")
	if err != nil {
		return DeathButtonAction{}, false, err
	}
	action := DeathButtonAction{}
	switch handler {
	case "open":
		continuation, found, err := ParseFlatMouseContinuation(program, handler)
		if err != nil || !found {
			return action, found, err
		}
		action.Kind, action.Continuation = DeathButtonOpen, continuation
	case "new":
		action.Kind = DeathButtonNew
	case "quit":
		action.Kind = DeathButtonQuit
	case "help":
		action.Kind = DeathButtonHelp
	case "left", "right":
		action.Kind, action.Prop = DeathButtonGun, "gunimage"
	default:
		return action, false, nil
	}
	for index := start; index < end; index++ {
		switch program.Records[index].Kind {
		case LookupOpcode("screentoblack"):
			action.FadeFrames, err = deathNumberArgument(program, index+4)
		case LookupOpcode("opengame"), LookupOpcode("savegame"):
			action.GameName, err = deathStringArgument(program, index+2)
		case LookupOpcode("questiondialog"):
			if !debugging {
				action.SaveQuestion, err = deathStringArgument(program, index+2)
			}
		case LookupOpcode("propdeg"):
			value, numberErr := deathNumberArgument(program, index+4)
			action.Degree, err = int16(value), numberErr
		case LookupOpcode("propxy"):
			x, xErr := deathNumberArgument(program, index+4)
			y, yErr := deathNumberArgument(program, index+6)
			if xErr != nil {
				return action, false, xErr
			}
			action.Anchor, err = image.Pt(x, y), yErr
		case LookupOpcode("voicesound"):
			action.Sound, err = deathStringArgument(program, index+2)
		case LookupOpcode("delay"):
			action.DelayFrames, err = deathNumberArgument(program, index+2)
		case 5:
			name, nameErr := program.IdentifierPascal(index)
			if nameErr != nil {
				return action, false, nameErr
			}
			if index+2 >= end {
				continue
			}
			if program.Records[index+1].Kind == LookupOpcode("=") && program.Records[index+2].Kind == 4 {
				if string(name[1:]) == "day" {
					action.StartDay = int(program.Records[index+2].Data)
				}
				if string(name[1:]) == "clock" {
					action.StartClock = int(program.Records[index+2].Data)
				}
			}
			if string(name[1:]) == "spotmovie" {
				movie, movieErr := deathStringArgument(program, index+2)
				action.Movie, err = "MOVIES/"+strings.ToUpper(movie), movieErr
			}
		}
		if err != nil {
			return action, false, err
		}
	}
	return action, true, nil
}

func deathCodeRange(program Program, name string) (int, int, error) {
	start, err := FindCode(program, name)
	if err != nil {
		return 0, 0, err
	}
	length, err := NextCodeOffset(program.Records, start)
	if err != nil {
		return 0, 0, err
	}
	end := len(program.Records)
	if length >= 0 {
		end = start + int(length)
	}
	return start + 2, end, nil
}

func deathStringArgument(program Program, index int) (string, error) {
	value, err := program.LiteralPascal(index)
	if err != nil {
		return "", err
	}
	return string(value[1:]), nil
}

func deathNumberArgument(program Program, index int) (int, error) {
	if index < 0 || index >= len(program.Records) || program.Records[index].Kind != 4 {
		return 0, fmt.Errorf("death numeric argument at record %d is unavailable", index)
	}
	return int(int32(program.Records[index].Data)), nil
}
