package engine

import (
	"fmt"
	"strings"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"

	"redust/assets"
	"redust/audio"
	"redust/render"
)

type VoiceOne struct {
	context                 *ebitenaudio.Context
	sound                   audio.NativeSound
	player                  *audio.Player
	name, subtitle          string
	start, duration         uint32
	started, active, closed bool
}

func NewVoiceOne(bank *audio.SoundBank, name string, context *ebitenaudio.Context, speech *assets.PuppetSpeech) (*VoiceOne, error) {
	if bank == nil || name == "" {
		return nil, fmt.Errorf("voiceone requires a sound bank and cue name")
	}
	sound, err := bank.Load(name)
	if err != nil {
		return nil, err
	}
	frameBytes := sound.Format.Channels * sound.Format.BitsPerSample / 8
	if sound.Format.SampleRate < 1 || frameBytes < 1 || len(sound.Samples) == 0 || len(sound.Samples)%frameBytes != 0 {
		return nil, fmt.Errorf("voiceone cue %q has invalid PCM duration", name)
	}
	duration := (uint64(len(sound.Samples)/frameBytes)*60 + uint64(sound.Format.SampleRate) - 1) / uint64(sound.Format.SampleRate)
	if duration < 1 || duration > 0x7fffffff {
		return nil, fmt.Errorf("voiceone cue %q duration is outside native clock range", name)
	}
	voice := &VoiceOne{context: context, sound: sound, name: name, duration: uint32(duration)}
	if speech != nil {
		if !strings.EqualFold(speech.Name, name) {
			return nil, fmt.Errorf("voiceone cue %q does not match subtitle speech %q", name, speech.Name)
		}
		if speech.HasSubtitle() {
			voice.subtitle = speech.SubtitleText()
		}
	}
	return voice, nil
}

func (v *VoiceOne) Start(now uint32) error {
	if v == nil || v.started || v.closed {
		return fmt.Errorf("voiceone cannot start in its current state")
	}
	if v.context != nil {
		player, err := audio.NewPlayer(v.context, v.sound.Samples, v.sound.Format)
		if err != nil {
			return err
		}
		v.player = player
		v.player.Play()
	}
	v.start, v.started, v.active = now, true, true
	return nil
}

func (v *VoiceOne) Update(now uint32) (bool, error) {
	if v == nil || !v.started {
		return false, fmt.Errorf("voiceone has not started")
	}
	if !v.active {
		return true, nil
	}
	finished := now-v.start >= v.duration
	if v.player != nil {
		finished = !v.player.IsPlaying()
	}
	if !finished {
		return false, nil
	}
	return true, v.Close()
}

func (v *VoiceOne) Active() bool { return v != nil && v.active }
func (v *VoiceOne) DurationFrames() uint32 {
	if v == nil {
		return 0
	}
	return v.duration
}
func (v *VoiceOne) Subtitle() string {
	if v == nil || !v.active {
		return ""
	}
	return v.subtitle
}
func (v *VoiceOne) Name() string {
	if v == nil {
		return ""
	}
	return v.name
}

func (v *VoiceOne) Frame(base render.IndexedFrame) (render.IndexedFrame, error) {
	if subtitle := v.Subtitle(); subtitle != "" {
		return render.DrawNativeSubtitle(base, subtitle)
	}
	return base, nil
}

func (v *VoiceOne) Close() error {
	if v == nil {
		return nil
	}
	v.active, v.closed = false, true
	v.sound = audio.NativeSound{}
	if v.player == nil {
		return nil
	}
	player := v.player
	v.player = nil
	return player.Close()
}
