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

// defaultPan is the pan word a freshly built sound record holds (FUN_00434A10
// writes 0x80 at record +0x31), so a sound that soundpan never set reads 128.
const defaultPan = 128

// soundChannel is one of the two sound channels, native channels 1 and 2 (the
// record copies at DAT_004465A0 and DAT_00446608). A channel keeps the resource
// index of the sound it was given (record +0x39), which FUN_00434B80 clears
// once the device reports the channel done, and the sound's spelling (record
// +0x45) and bank, which closetrackfile uses to stop the channel.
type soundChannel struct {
	resource uint32
	name     string
	bank     *SoundBank
	player   *Player
}

// Tracks is the open track file list with the sound channels scripts drive:
// one-shot sounds, looping sounds, per-sound volumes and pans and the voice
// channel.
type Tracks struct {
	workspace assets.Workspace
	context   *ebitenaudio.Context
	open      []*OpenTrack
	loops     map[string]*Player
	volumes   map[string]uint8
	pans      map[string]uint8
	voice     *Player
	voiceName string
	// channels are the two sound channels bothsound plays on (native 1 and 2).
	channels [2]soundChannel
	// OnClose is told when a track is released, so a theme playing from it
	// can be stopped.
	OnClose func(track *OpenTrack)
}

// NewTracks returns an empty list. The context may be nil, in which case
// the files are tracked but nothing sounds.
func NewTracks(workspace assets.Workspace, context *ebitenaudio.Context) *Tracks {
	return &Tracks{workspace: workspace, context: context, loops: map[string]*Player{}, volumes: map[string]uint8{}, pans: map[string]uint8{}}
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

// TrackCount is counttracks (FUN_0040F010): the number of open track files,
// DAT_004599FC.
func (t *Tracks) TrackCount() int { return len(t.open) }

// TrackName is the lookup behind indextotrack (FUN_0040F040): the internal name
// at +0x16 of the 1-based open track. found is false outside 1..count, which
// is status 0x0A.
func (t *Tracks) TrackName(index int) (name string, found bool) {
	if index < 1 || index > len(t.open) {
		return "", false
	}
	return t.open[index-1].Name, true
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
		// FUN_00434C40 halts the sound channels whose sound came from the track.
		for channel := range t.channels {
			if t.channels[channel].bank == track.Bank {
				t.stopChannel(channel)
			}
		}
		_ = track.Bank.Close()
		t.open = append(t.open[:index], t.open[index+1:]...)
		// FUN_0040E650 frees the closed track's sound records, and a reopened
		// sound is rebuilt at defaultPan, so a pan with no open holder is dropped.
		for sound := range t.pans {
			if t.lookup(sound) == nil {
				delete(t.pans, sound)
			}
		}
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

// Pan is soundpan's value (FUN_0040EDC0 -> FUN_004354C0): the sound's pan word,
// defaultPan until soundpan sets it. found is false when no open track holds
// the sound.
func (t *Tracks) Pan(name string) (int, bool) {
	if t.lookup(name) == nil {
		return 0, false
	}
	if stored, ok := t.pans[strings.ToLower(name)]; ok {
		return int(stored), true
	}
	return defaultPan, true
}

// SetPan is soundpan (FUN_0040EAF0 -> FUN_004354D0): the level is clamped to
// 0..255 and stored on the sound. FUN_004354D0 has no audio gate, so the pan is
// kept whether or not the audio system is up. The port has no pan control in
// its playback (ebitengine players are not panned), and no cached native reader
// of the pan word was found, so the value is stored only.
func (t *Tracks) SetPan(name string, level int) bool {
	if t.lookup(name) == nil {
		return false
	}
	if level < 0 {
		level = 0
	} else if level > 255 {
		level = 255
	}
	t.pans[strings.ToLower(name)] = uint8(level)
	return true
}

// SampleRate is soundrate (FUN_0040EC70): the sound's sample rate from its
// resource header (11025, 22050 or 44100). It is read-only: the native command
// dispatcher has no soundrate statement, so nothing here sets it.
func (t *Tracks) SampleRate(name string) (int, bool, error) {
	track := t.lookup(name)
	if track == nil {
		return 0, false, nil
	}
	rate, err := track.Bank.SampleRate(strings.ToLower(name))
	return rate, true, err
}

// WaveVolume is the wavevolume getter (FUN_00434970): the application level
// 0..9, or 0 while the audio system is down (DAT_0045E068 clear). The port
// stands a nil audio context for that flag; the mapping is inferred, since the
// init path that sets DAT_0045E068 is not traced here.
func (t *Tracks) WaveVolume() int {
	if t.context == nil {
		return 0
	}
	return WaveVolume()
}

// SetWaveVolume is the wavevolume setter (FUN_004349A0): ignored while the
// audio system is down. The statement checks 0..9 before this is reached.
func (t *Tracks) SetWaveVolume(level int) error {
	if t.context == nil {
		return nil
	}
	return SetWaveVolume(level)
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

// PlaySound is bothsound. FUN_0040E830 searches the first sound table of each
// open track (FUN_0040F210, found false is status 0x0A), then FUN_00435240
// queues the sound on a sound channel. With the audio system down
// (DAT_0045E068 clear) the search still runs and nothing is queued.
func (t *Tracks) PlaySound(name string) (found bool, err error) {
	track := t.lookupFirst(name)
	if track == nil {
		return false, nil
	}
	if t.context == nil {
		return true, nil
	}
	t.pollChannels()
	// FUN_00435240 takes channel 1 when its resource index is below channel 2's,
	// and channel 2 otherwise.
	index := 1
	if t.channels[0].resource < t.channels[1].resource {
		index = 0
	}
	// FUN_00433F00 stops what the channel was playing (FUN_00433B50) first.
	t.stopChannel(index)
	key := strings.ToLower(name)
	t.channels[index] = soundChannel{resource: track.Bank.Resource(key), name: track.Bank.Spelling(key), bank: track.Bank}
	// The record's loop word (+0x33) is copied with the sound. FUN_004341C0 turns
	// it into the channel's repeat counter (DAT_00446510), which is not decoded,
	// so the sound plays once here, as singlesound does.
	player, err := track.Bank.PlayOne(t.context, key, t.Volume(name))
	if err != nil {
		return true, err
	}
	t.channels[index].player = player
	return true, nil
}

// lookupFirst is FUN_0040F210's search: the first open track whose first sound
// table holds the sound. The voice table is not searched.
func (t *Tracks) lookupFirst(name string) *OpenTrack {
	for _, track := range t.open {
		if track.Bank.HasFirstTable(name) {
			return track
		}
	}
	return nil
}

// pollChannels is the clean-up FUN_00434B80 runs before the channels are read:
// a channel whose device has finished loses its resource index, and the
// finished player is released.
func (t *Tracks) pollChannels() {
	for index := range t.channels {
		if t.channels[index].busy() {
			continue
		}
		t.stopChannel(index)
		t.channels[index] = soundChannel{}
	}
}

// stopChannel halts a sound channel's player. The channel keeps its resource
// index until the next poll, as the native record does.
func (t *Tracks) stopChannel(index int) {
	if player := t.channels[index].player; player != nil {
		_ = player.Close()
	}
	t.channels[index].player = nil
}

// busy is the port's stand-in for the device's channel-busy predicate
// (FUN_00433300): the channel's player is still playing.
func (c *soundChannel) busy() bool { return c.player.IsPlaying() }

// VoiceDone is voicedone (FUN_0040EEB0 -> FUN_00434C10): true when the voice
// channel (native channel 0) holds no sound after the poll. The poll is the one
// CurrentVoice runs. It is true while the audio system is down.
func (t *Tracks) VoiceDone() bool {
	if t.context == nil {
		return true
	}
	return t.CurrentVoice() == ""
}

// SoundDone is sounddone (FUN_0040EE80 -> FUN_00434BE0): true when neither sound
// channel holds a sound after the poll. It is true while the audio system is
// down, as the native gate returns before it reads the channels.
func (t *Tracks) SoundDone() bool {
	if t.context == nil {
		return true
	}
	t.pollChannels()
	return t.channels[0].resource == 0 && t.channels[1].resource == 0
}

// CurrentSound is currentsound (FUN_0040F0F0 -> FUN_00434EF0): the name of the
// sound the sound channels report. A busy channel is reported alone. When both
// are busy, channel 1 is reported if channel 2's resource index is below
// channel 1's, and channel 2 otherwise. It returns "" when neither channel is
// busy, and when the audio system is down. FUN_00434EF0 does not poll, so the
// indices are read as they stand.
func (t *Tracks) CurrentSound() string {
	if t.context == nil {
		return ""
	}
	busy1, busy2 := t.channels[0].busy(), t.channels[1].busy()
	switch {
	case busy1 && busy2:
		if t.channels[1].resource < t.channels[0].resource {
			return t.channels[0].name
		}
		return t.channels[1].name
	case busy2:
		return t.channels[1].name
	case busy1:
		return t.channels[0].name
	}
	return ""
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
	// FUN_00434C90 with (1,1,0,0) halts the two sound channels.
	for index := range t.channels {
		t.stopChannel(index)
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

// SoundCount is countsounds (FUN_0040EEE0): the count of the first row array of
// the open track whose internal name matches (record +4, the header word at
// 0x18). found is false when no open track has the name (status 0x0A).
func (t *Tracks) SoundCount(name string) (count int, found bool, err error) {
	track := t.Find(name)
	if track == nil {
		return 0, false, nil
	}
	count, _, err = track.Bank.soundRows()
	return count, true, err
}

// SoundName is the lookup behind indextosound (FUN_0040EF60): the Pascal name
// in row index (1-based) of the matching track's first row array, which
// FUN_00434A10 copies to the row's +0x45 field. The caller checks 1..count
// first; found is false only when the row is outside the array.
func (t *Tracks) SoundName(name string, index int) (sound string, found bool, err error) {
	track := t.Find(name)
	if track == nil {
		return "", false, nil
	}
	_, names, err := track.Bank.soundRows()
	if err != nil {
		return "", false, err
	}
	if index < 1 || index > len(names) {
		return "", false, nil
	}
	return names[index-1], true, nil
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
