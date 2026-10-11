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
	// SoundCount is countsounds (FUN_0040EEE0): the number of sounds in the
	// matching open track's first row array. found is false when no open track
	// has the internal name (status 0x0A).
	SoundCount(track string) (count int, found bool, err error)
	// SoundName is the lookup behind indextosound (FUN_0040EF60): the name in
	// the 1-based row of that track's first row array.
	SoundName(track string, index int) (name string, found bool, err error)
	// TrackCount is counttracks (FUN_0040F010): the number of open track files,
	// DAT_004599FC.
	TrackCount() int
	// TrackName is the lookup behind indextotrack (FUN_0040F040): the internal
	// name of the 1-based open track. found is false outside 1..count (0x0A).
	TrackName(index int) (name string, found bool)
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
	case "countsounds":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		// FUN_004220A0 takes the name and FUN_00421FC0 rejects a non-string (0x0E).
		if len(args) != 1 || args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.Tracks == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: countsounds has no track files", ErrHostOpcodeUnimplemented)
		}
		count, found, err := h.Env.Tracks.SoundCount(args[0].Text)
		if err != nil {
			return Record{}, 0, 0, true, err
		}
		if !found {
			return Record{}, 0, 0x0a, true, nil
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 4, Int: int32(count)})
		return record, consumed, status, true, err
	case "indextosound":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		// FUN_00417070 wants a comma after the name (0x1C) before it reads the index.
		switch {
		case len(args) == 1:
			return Record{}, 0, ScriptStatusMissingComma, true, nil
		case len(args) != 2:
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		if args[0].Kind != 3 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.Tracks == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: indextosound has no track files", ErrHostOpcodeUnimplemented)
		}
		// The track is looked up before the index is type-checked (0x0A, then 0x0E).
		count, found, err := h.Env.Tracks.SoundCount(args[0].Text)
		if err != nil {
			return Record{}, 0, 0, true, err
		}
		if !found {
			return Record{}, 0, 0x0a, true, nil
		}
		if args[1].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		index := int(args[1].Int)
		if index < 1 || index > count {
			return Record{}, 0, 0x0a, true, nil
		}
		sound, found, err := h.Env.Tracks.SoundName(args[0].Text, index)
		if err != nil {
			return Record{}, 0, 0, true, err
		}
		if !found {
			return Record{}, 0, 0x0a, true, nil
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: sound})
		return record, consumed, status, true, err
	case "counttracks":
		// FUN_0040F010 takes no argument, and FUN_004137b0 then requires the
		// close record right after the open one; anything else is status 0x02.
		if call.Kind(call.Start+1) != opOpen || call.Kind(call.Start+2) != opClose {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		if h.Env.Tracks == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: counttracks has no track files", ErrHostOpcodeUnimplemented)
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 4, Int: int32(h.Env.Tracks.TrackCount())})
		return record, 3, status, true, err
	case "indextotrack":
		args, consumed, status, err := call.Args()
		if err != nil || status != 0 {
			return Record{}, 0, status, true, err
		}
		// FUN_0040F040 reads one argument with FUN_004220a0. With none, the close
		// record reaches FUN_00422040, which rejects it with 0x0E, as it does a
		// non-integer index. The index is then checked against 1..count (0x0A).
		if len(args) == 0 || args[0].Kind != 4 {
			return Record{}, 0, ScriptStatusWrongType, true, nil
		}
		if h.Env.Tracks == nil {
			return Record{}, 0, 0, true, fmt.Errorf("%w: indextotrack has no track files", ErrHostOpcodeUnimplemented)
		}
		name, found := h.Env.Tracks.TrackName(int(args[0].Int))
		if !found {
			return Record{}, 0, 0x0a, true, nil
		}
		// FUN_004137b0 wants the close record after the index, so extras are 0x02.
		if len(args) != 1 {
			return Record{}, 0, ScriptStatusMalformed, true, nil
		}
		record, status, err := call.Interpreter.Record(ScriptValue{Kind: 3, Text: name})
		return record, consumed, status, true, err
	}
	return Record{}, 0, 0, false, nil
}
