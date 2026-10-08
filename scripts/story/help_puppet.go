package story

import (
	"strings"
)

type HelpSpeechStage struct {
	Lines       []string
	DelayBefore uint32
}

type HelpChoiceResult struct {
	Speech   []HelpSpeechStage
	NextPage string
	Phase    int16
	SetPhase bool
	GiveBone bool
	GiveRing bool
	HideHelp bool
	Complete bool
}

func HelpPuppetChoice(page string, event int32) (HelpChoiceResult, bool) {
	lines := func(names ...string) []HelpSpeechStage { return []HelpSpeechStage{{Lines: names}} }
	complete := func(names ...string) HelpChoiceResult {
		return HelpChoiceResult{Speech: lines(names...), Complete: true}
	}
	phase := func(names ...string) HelpChoiceResult {
		return HelpChoiceResult{Speech: lines(names...), Phase: 2, SetPhase: true, Complete: true}
	}
	switch strings.ToLower(page) {
	case "mainloop":
		switch event {
		case -1:
			return complete("help.31", "help.32"), true
		case 101:
			return phase("help.33", "help.3", "help.34"), true
		case 102:
			return HelpChoiceResult{Speech: lines("help.35"), NextPage: "secondchance"}, true
		case 103:
			return HelpChoiceResult{Speech: lines("help.36", "help.35"), NextPage: "secondchance"}, true
		}
	case "secondchance":
		switch event {
		case -1:
			return complete("help.31", "help.32"), true
		case 101:
			return phase("help.37"), true
		case 102:
			return HelpChoiceResult{Speech: lines("help.38"), NextPage: "thirdchance"}, true
		}
	case "thirdchance":
		switch event {
		case -1:
			return complete("help.39", "help.40"), true
		case 101:
			return HelpChoiceResult{Speech: []HelpSpeechStage{{Lines: []string{"help.7", "help.43"}}, {Lines: []string{"help.44", "help.18"}, DelayBefore: 150}}, GiveBone: true, Complete: true}, true
		case 102:
			return phase("help.41", "help.42"), true
		}
	case "mustappologize":
		switch event {
		case -1:
			return complete("help.31", "help.32"), true
		case 101:
			return HelpChoiceResult{Speech: lines("help.45"), NextPage: "thirdchance", Phase: 1, SetPhase: true}, true
		case 102:
			return complete("help.46", "help.34"), true
		}
	case "givesring":
		switch event {
		case -1:
			return HelpChoiceResult{Phase: 3, SetPhase: true, Complete: true}, true
		case 101:
			return HelpChoiceResult{Speech: []HelpSpeechStage{{Lines: []string{"help.19a", "help.19b", "help.19c", "help.19d", "help.19e", "help.19f", "help.19g", "help.19h", "help.19i"}}, {Lines: []string{"help.50", "help.1", "help.51", "help.34"}}}, Phase: 3, SetPhase: true, GiveRing: true, HideHelp: true, Complete: true}, true
		case 102:
			return HelpChoiceResult{Speech: []HelpSpeechStage{{Lines: []string{"help.50", "help.1", "help.51", "help.34"}}}, Phase: 3, SetPhase: true, GiveRing: true, HideHelp: true, Complete: true}, true
		}
	}
	return HelpChoiceResult{}, false
}

func HelpPuppetEntryPage(phase int16, dogVisible bool) string {
	if phase == 3 {
		return "morehelp"
	}
	if !dogVisible {
		return "givesring"
	}
	switch phase {
	case 1:
		return "thirdchance"
	case 2:
		return "mustappologize"
	case 3:
		return "morehelp"
	default:
		return "mainloop"
	}
}
