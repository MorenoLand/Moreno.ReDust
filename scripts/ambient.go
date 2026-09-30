package scripts

import "strings"

type NativeSoundCue struct {
	Name   string
	Volume uint8
}

func NightWildlifeCue(currentTheme string, random *NativeRandom) (NativeSoundCue, bool) {
	if random == nil || !strings.EqualFold(currentTheme, "nightwind3") {
		return NativeSoundCue{}, false
	}
	if random.Inclusive(1000) < 8 {
		names := [...]string{"owl", "dogdist", "coyotedist1", "coyotedist2", "coyotedist3"}
		name := names[random.Inclusive(5)-1]
		volume := 128 + random.Inclusive(128)
		if volume > 255 {
			volume = 255
		}
		return NativeSoundCue{Name: name, Volume: uint8(volume)}, true
	}
	if random.Inclusive(1000) < 100 {
		names := [...]string{"cricket1", "cricket2", "cricket3"}
		return NativeSoundCue{Name: names[random.Inclusive(3)-1], Volume: uint8(64 + random.Inclusive(64))}, true
	}
	return NativeSoundCue{}, false
}

func DayWildlifeCue(currentTheme string, random *NativeRandom) (NativeSoundCue, bool) {
	if random == nil || !strings.EqualFold(currentTheme, "daymusic5") {
		return NativeSoundCue{}, false
	}
	if random.Inclusive(1000) < 8 {
		names := [...]string{"distdogbark", "horse"}
		return NativeSoundCue{Name: names[random.Inclusive(2)-1], Volume: uint8(64 + random.Inclusive(128))}, true
	}
	return NativeSoundCue{}, false
}
