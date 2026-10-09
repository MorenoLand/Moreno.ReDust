package audio

import (
	"strings"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"

	"redust/assets"
)

// OpenTrack is one entry of the engine's open track file array
// (DAT_004599F8, 0x26 bytes each, FUN_0040E250): the file's sound bank and the
// internal name closetrackfile and playtheme match.
type OpenTrack struct {
	File string
	Name string
	Bank *SoundBank
}

// Tracks is the open track file list with the sound channels scripts drive:
// one-shot sounds, looping sounds, per-sound volumes and the voice channel.
type Tracks struct {
	workspace assets.Workspace
	context   *ebitenaudio.Context
	open      []*OpenTrack
	loops     map[string]*Player
	volumes   map[string]uint8
	voice     *Player
	voiceName string
	// OnClose is told when a track is released, so a theme playing from it
	// can be stopped.
	OnClose func(track *OpenTrack)
}

// NewTracks returns an empty list. The context may be nil, in which case
// the files are tracked but nothing sounds.
func NewTracks(workspace assets.Workspace, context *ebitenaudio.Context) *Tracks {
	return &Tracks{workspace: workspace, context: context, loops: map[string]*Player{}, volumes: map[string]uint8{}}
}

// Adopt appends a bank the caller already opened (UNILIB.SND at boot).
func (t *Tracks) Adopt(file string, bank *SoundBank) {
	t.open = append(t.open, &OpenTrack{File: file, Name: bank.ThemeName(), Bank: bank})
}

// Open returns the open tracks in the order they were opened.
func (t *Tracks) Open() []*OpenTrack { return t.open }

// Find is FUN_0040F2F0: the open track whose internal name matches.
func (t *Tracks) Find(name string) *OpenTrack {
	for _, track := range t.open {
		if strings.EqualFold(track.Name, name) {
			return track
		}
	}
	return nil
}

// OpenTrack is opentrackfile.
func (t *Tracks) OpenTrack(file string) error {
	path := "DATA/" + strings.ToUpper(strings.TrimPrefix(strings.ReplaceAll(file, `\`, "/"), "DATA/"))
	bank, err := OpenSoundBank(t.workspace, path)
	if err != nil {
		return err
	}
	t.open = append(t.open, &OpenTrack{File: path, Name: bank.ThemeName(), Bank: bank})
	return nil
}

// CloseTrack is closetrackfile (FUN_0040E540): the first entry whose name
// matches is freed together with its sounds (FUN_0040E650 -> FUN_00434C40).
func (t *Tracks) CloseTrack(name string) bool {
	for index, track := range t.open {
		if !strings.EqualFold(track.Name, name) {
			continue
		}
		if t.OnClose != nil {
			t.OnClose(track)
		}
		for sound, player := range t.loops {
			if track.Bank.Has(sound) {
				_ = player.Close()
				delete(t.loops, sound)
			}
		}
		_ = track.Bank.Close()
		t.open = append(t.open[:index], t.open[index+1:]...)
		return true
	}
	return false
}

// lookup is FUN_0040F210: the first open track holding the sound.
func (t *Tracks) lookup(name string) *OpenTrack {
	key := strings.ToLower(name)
	for _, track := range t.open {
		if track.Bank.Has(key) {
			return track
		}
	}
	return nil
}

// Has reports whether an open track holds the sound.
func (t *Tracks) Has(name string) bool { return t.lookup(name) != nil }

// Volume is the level soundvol set for the sound, 255 by default.
func (t *Tracks) Volume(name string) uint8 {
	if level, ok := t.volumes[strings.ToLower(name)]; ok {
		return level
	}
	return 255
}

// SetVolume is soundvol (FUN_0040E910); a playing loop follows it.
func (t *Tracks) SetVolume(name string, level int) {
	if level < 0 {
		level = 0
	} else if level > 255 {
		level = 255
	}
	key := strings.ToLower(name)
	t.volumes[key] = uint8(level)
	if player := t.loops[key]; player != nil {
		player.SetVolume(float64(level) / 255)
	}
}

// Play starts a sound once at its volume. A missing sound or context is not
// an error here; the caller decides whether an unknown name matters.
func (t *Tracks) Play(name string, voice bool) (found bool, err error) {
	track := t.lookup(name)
	if track == nil {
		return false, nil
	}
	if t.context == nil {
		return true, nil
	}
	player, err := track.Bank.PlayOne(t.context, strings.ToLower(name), t.Volume(name))
	if err != nil {
		return true, err
	}
	if voice {
		if t.voice != nil {
			_ = t.voice.Close()
		}
		t.voice, t.voiceName = player, strings.ToLower(name)
	} else {
		track.Bank.adopt(player)
	}
	return true, nil
}

// SoundLoop is the soundloop statement.
func (t *Tracks) SoundLoop(name string, on bool) bool {
	track := t.lookup(name)
	if track == nil {
		return false
	}
	key := strings.ToLower(name)
	if !on {
		if player := t.loops[key]; player != nil {
			_ = player.Close()
			delete(t.loops, key)
		}
		return true
	}
	if t.loops[key] != nil {
		return true
	}
	if t.context == nil {
		t.loops[key] = nil
		return true
	}
	player, err := track.Bank.PlayLoop(t.context, key, t.Volume(name))
	if err == nil && player != nil {
		t.loops[key] = player
	}
	return true
}

// Looping is the soundloop value: found is false when no open track holds
// the sound.
func (t *Tracks) Looping(name string) (looping, found bool) {
	if t.lookup(name) == nil {
		return false, false
	}
	_, looping = t.loops[strings.ToLower(name)]
	return looping, true
}

// HaltSound stops one-shot sounds and loops (FUN_0040E8D0).
func (t *Tracks) HaltSound() {
	for key, player := range t.loops {
		if player != nil {
			_ = player.Close()
		}
		delete(t.loops, key)
	}
	for _, track := range t.open {
		track.Bank.haltPlayers()
	}
}

// HaltVoice stops the voice channel (FUN_0040E8F0).
func (t *Tracks) HaltVoice() {
	if t.voice != nil {
		_ = t.voice.Close()
	}
	t.voice, t.voiceName = nil, ""
}

// CurrentVoice is currentvoice: the sound on the voice channel while it plays.
func (t *Tracks) CurrentVoice() string {
	if t.voice != nil && !t.voice.IsPlaying() {
		_ = t.voice.Close()
		t.voice, t.voiceName = nil, ""
	}
	return t.voiceName
}

// Close releases every open track except those adopted from the caller.
func (t *Tracks) Close() {
	t.HaltSound()
	t.HaltVoice()
}

func (b *SoundBank) adopt(player *Player) { b.players = append(b.players, player) }

func (b *SoundBank) haltPlayers() {
	if b == nil {
		return
	}
	for _, player := range b.players {
		_ = player.Close()
	}
	b.players = nil
}
