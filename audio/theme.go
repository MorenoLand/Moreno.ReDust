package audio

import (
	"encoding/binary"
	"fmt"
	"strings"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"
)

const nativeThemeNameOffset = 0x9e
const nativeThemeSequenceOffset = 0x1e

type NativeTheme struct {
	Name   string
	Tracks []NativeSound
	Events []int
}

func (t NativeTheme) FirstVoiceName() string {
	if len(t.Events) == 0 || t.Events[0] < 0 || t.Events[0] >= len(t.Tracks) {
		return ""
	}
	name := t.Tracks[t.Events[0]].Metadata.Name
	if len(name) == 0 || int(name[0])+1 != len(name) {
		return ""
	}
	return string(name[1:])
}

func (b *SoundBank) LoadTheme(name string) (NativeTheme, error) {
	if b == nil || b.resources == nil {
		return NativeTheme{}, fmt.Errorf("sound bank is closed")
	}
	lease, err := b.resources.Acquire(0)
	if err != nil {
		return NativeTheme{}, err
	}
	metadata, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return NativeTheme{}, err
	}
	if len(metadata) < nativeSoundTableOffset || len(metadata) <= nativeThemeNameOffset {
		return NativeTheme{}, fmt.Errorf("sound bank metadata is too short for its theme header")
	}
	nameLength := int(metadata[nativeThemeNameOffset])
	if nativeThemeNameOffset+1+nameLength > len(metadata) {
		return NativeTheme{}, fmt.Errorf("sound bank has a truncated theme name")
	}
	themeName := string(metadata[nativeThemeNameOffset+1 : nativeThemeNameOffset+1+nameLength])
	if !strings.EqualFold(name, themeName) {
		return NativeTheme{}, fmt.Errorf("theme %q is not in sound bank %q", name, themeName)
	}
	sampleCount := int(binary.LittleEndian.Uint16(metadata[0x18:0x1a]))
	voiceCount := int(binary.LittleEndian.Uint16(metadata[0x1a:0x1c]))
	sequenceCount := int(binary.LittleEndian.Uint16(metadata[0x1c:0x1e]))
	if sampleCount+voiceCount > (len(metadata)-nativeSoundTableOffset)/nativeSoundRowSize || sequenceCount > (len(metadata)-nativeThemeSequenceOffset)/2 {
		return NativeTheme{}, fmt.Errorf("sound bank theme tables exceed resource-zero metadata")
	}
	tracks, events, trackByVoice := make([]NativeSound, 0), make([]int, sequenceCount), make(map[int]int)
	for event := 0; event < sequenceCount; event++ {
		voice := int(binary.LittleEndian.Uint16(metadata[nativeThemeSequenceOffset+event*2 : nativeThemeSequenceOffset+event*2+2]))
		if voice < 1 || voice > voiceCount {
			return NativeTheme{}, fmt.Errorf("sound bank theme event %d selects voice %d outside 1..%d", event, voice, voiceCount)
		}
		track, ok := trackByVoice[voice]
		if !ok {
			row := nativeSoundTableOffset + (sampleCount+voice-1)*nativeSoundRowSize
			nameStart, nameLength := row+8, int(metadata[row+8])
			if nameLength+1 > row+nativeSoundRowSize-nameStart {
				return NativeTheme{}, fmt.Errorf("sound bank voice %d has a truncated name", voice)
			}
			sound, err := b.Load(string(metadata[nameStart+1 : nameStart+nameLength+1]))
			if err != nil {
				return NativeTheme{}, fmt.Errorf("load sound bank voice %d: %w", voice, err)
			}
			track = len(tracks)
			trackByVoice[voice] = track
			tracks = append(tracks, sound)
		}
		events[event] = track
	}
	if len(events) == 0 {
		return NativeTheme{}, fmt.Errorf("sound bank theme %q has no events", themeName)
	}
	return NativeTheme{Name: themeName, Tracks: tracks, Events: events}, nil
}

func (t NativeTheme) Play(context *ebitenaudio.Context) (*ebitenaudio.Player, error) {
	player, err := NewNativePlaylist(context, t.Tracks, t.Events, 0)
	if err != nil {
		return nil, err
	}
	player.SetVolume(1)
	return player, nil
}
