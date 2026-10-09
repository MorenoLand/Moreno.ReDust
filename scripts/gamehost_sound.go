package scripts

import "fmt"

// TrackHost is the audio side of the track-file commands. The native engine
// keeps one array of open track files (DAT_004599F8/DAT_004599FC, 0x26 bytes
// each: the file's handle, its sound and voice tables and, at +0x16, the
// internal name that closetrackfile and playtheme match against).
type TrackHost interface {
	// OpenTrack is opentrackfile (FUN_0040E250 -> FUN_0040E340): load the file
	// and append it to the open list.
	OpenTrack(file string) error
	// CloseTrack is closetrackfile (FUN_0040E540): the entry whose internal
	// name matches is released (FUN_0040E650); no match is status 0x0A.
	CloseTrack(name string) bool
	// HaltSound and HaltVoice are FUN_0040E8D0 and FUN_0040E8F0, which call
	// FUN_00434C90 with (1,1,0,0) and (0,0,0,1).
	HaltSound()
	HaltVoice()
	// SoundLoop is the soundloop statement (FUN_0040EBC0): the sound is looked
	// up in the open tracks (found false is status 0x0A) and its loop flag set.
	SoundLoop(name string, on bool) (found bool)
	// Looping is the soundloop value (FUN_0040EE20, FUN_004354E0).
	Looping(name string) (looping, found bool)
	// CurrentVoice is currentvoice (FUN_0040F1D0): the name of the sound the
	// voice channel is playing, "" when it is idle.
	CurrentVoice() string
}

// soundCommand handles the track-file and sound-channel statements.
func (h *GameHost) soundCommand(name string, call *ScriptCall) (int, uint16, bool, error) {
	switch name {
	case "opentrackfile", "closetrackfile":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.Tracks == nil {
			return 0, 0, true, fmt.Errorf("%w: %s has no track files", ErrHostOpcodeUnimplemented, name)
		}
		if name == "opentrackfile" {
			return consumed, 0, true, h.Env.Tracks.OpenTrack(args[0].Text)
		}
		if !h.Env.Tracks.CloseTrack(args[0].Text) {
			return 0, 0x0a, true, nil
		}
		return consumed, 0, true, nil
	case "haltsound", "haltvoice":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if h.Env.Tracks == nil {
			return 0, 0, true, fmt.Errorf("%w: %s has no track files", ErrHostOpcodeUnimplemented, name)
		}
		if name == "haltsound" {
			h.Env.Tracks.HaltSound()
		} else {
			h.Env.Tracks.HaltVoice()
		}
		return consumed, 0, true, nil
	case "soundloop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return 0, status, true, err
		}
		if len(args) != 2 || args[0].Kind != 3 {
			return 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.Tracks == nil {
			return 0, 0, true, fmt.Errorf("%w: soundloop has no track files", ErrHostOpcodeUnimplemented)
		}
		// FUN_0040F210 resolves the sound before the flag is type-checked.
		known := false
		if _, found := h.Env.Tracks.Looping(args[0].Text); found {
			known = true
		}
		if !known {
			return 0, 0x0a, true, nil
		}
		if args[1].Kind != 2 {
			return 0, ScriptStatusWrongType, true, nil
		}
		h.Env.Tracks.SoundLoop(args[0].Text, args[1].Int != 0)
		return consumed, 0, true, nil
	}
	return 0, 0, false, nil
}

// soundValue handles soundloop(name) and currentvoice().
func (h *GameHost) soundValue(name string, call *ScriptCall) (Record, int, uint16, bool, error) {
	switch name {
	case "soundloop":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.Tracks == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: soundloop has no track files", ErrHostOpcodeUnimplemented)
		}
		looping, found := h.Env.Tracks.Looping(args[0].Text)
		if !found {
			return Record{}, 0, 0x0a, true, nil
		}
		return Record{Kind: 2, Data: boolWord(looping)}, consumed, 0, true, nil
	case "currentvoice":
		_, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		if h.Env.Tracks == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: currentvoice has no track files", ErrHostOpcodeUnimplemented)
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: h.Env.Tracks.CurrentVoice()})
		return record, consumed, status, true, err
	}
	return Record{}, 0, 0, false, nil
}
