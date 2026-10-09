package save

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"redust/assets"
	"redust/scripts"
)

// The ReDust .rtd file.
//
// Real DUST writes its saved games with FUN_00422DE0 into an APPL container
// whose header tags are 0x5254444F and 0x44465254 ("ODTR", "TRFD" in memory
// order) and whose count is 100 (FUN_00401AA0). The container's records are raw
// memory dumps of the native engine: resource 0 is the 0x1210-byte prefix plus
// one 0x104-byte record per open APPL file, resource 1 is the 0x21E-byte
// engine globals block at 0x004598A0, resources 2..6 are the actor, cast,
// prop, shop and track tables, followed by the per-track row arrays, the
// DAT_004596F8 registry, the three job tables (0x540, 0x300 and 0x520 bytes)
// and the handles the sixteen job slots reference. Those tables hold pointers
// and handles of the native process, and the Go engine keeps its state in
// different structures, so ReDust cannot read a saved game made by real DUST
// and real DUST cannot read ReDust's.
//
// What ReDust does reproduce, with evidence:
//
//   - the APPL envelope: version 0x00010000, the two tags above, 128-entry
//     index pages, 64-byte record alignment (assets.WriteContainerTagged);
//   - resource 0's layout: the game-name Pascal string at +0x000 that
//     FUN_004235C0 compares with FUN_0042E5B0 against the script's name
//     argument, nine 0x100-byte script context pages at +0x100, three metadata
//     words at +0xA00/+0xA04/+0xA08, the 256-entry 8-byte palette at +0xA0C and
//     0x104-byte manager records from +0x1210;
//   - resource 1's size (0x21E) with the puppetparam words and the keyaborts
//     word at the offsets of their native globals.
//
// Everything else lives in resource 99, which is ReDust's own: the magic
// "REDUSTSV", a version dword, a length dword and the JSON of GameProgress.
const (
	// RTDStateResource is the ReDust-private resource that holds the game
	// state. The native count is 100, so index 99 is the last valid slot.
	RTDStateResource = 99
	rtdStateMagic    = "REDUSTSV"
	rtdStateVersion  = 1

	rtdPrefixSize      = 0x1210
	rtdGlobalsSize     = 0x21e
	rtdGlobalsBase     = 0x004598a0
	rtdKeyAbortsAddr   = 0x00459996
	rtdPuppetParamAddr = 0x0045999c
)

// Messages the native loader shows through MessageBoxA (FUN_0042DFB0): the
// strings at 0x0045D9E0 and 0x0045DA1C.
var (
	ErrNotSavedGame     = errors.New("This is not a valid saved game file.")
	ErrDifferentVersion = errors.New("This saved game is from a different version of this title.")
)

// RTDContext is what the serializer takes besides the game state.
type RTDContext struct {
	// GameName is the savegame() argument, written at resource 0 +0x000.
	GameName string
	// PaletteRaw is the 0x800-byte active palette (DAT_004590FC + 8); nil
	// writes an all-zero table.
	PaletteRaw []byte
	// OpenFiles names the APPL files that were open, one manager record each.
	OpenFiles []string
}

func putPascal(dst []byte, text string) {
	if len(text) > len(dst)-1 {
		text = text[:len(dst)-1]
	}
	dst[0] = byte(len(text))
	copy(dst[1:], text)
}

func readPascal(src []byte) string {
	if len(src) == 0 || int(src[0]) > len(src)-1 {
		return ""
	}
	return string(src[1 : 1+int(src[0])])
}

// EncodeRTD serializes progress into the ReDust .rtd container.
func EncodeRTD(progress GameProgress, ctx RTDContext) ([]byte, error) {
	if err := ValidateGameProgress(progress); err != nil {
		return nil, err
	}
	if strings.TrimSpace(ctx.GameName) == "" || len(ctx.GameName) > 255 {
		return nil, fmt.Errorf("saved game name %q is invalid", ctx.GameName)
	}
	if ctx.PaletteRaw != nil && len(ctx.PaletteRaw) != resource0EntrySize {
		return nil, fmt.Errorf("saved game palette has %d bytes, want %d", len(ctx.PaletteRaw), resource0EntrySize)
	}
	prefix := make([]byte, rtdPrefixSize+len(ctx.OpenFiles)*NativeSaveManagerRecordSize)
	putPascal(prefix[0:0x100], ctx.GameName)
	pages, err := scripts.NewScriptContextPages([]byte{0})
	if err != nil {
		return nil, err
	}
	pageBytes, err := pages.MarshalBinary()
	if err != nil {
		return nil, err
	}
	copy(prefix[resource0ContextOffset:], pageBytes)
	if ctx.PaletteRaw != nil {
		copy(prefix[resource0EntryOffset:], ctx.PaletteRaw)
	}
	for index, name := range ctx.OpenFiles {
		record := prefix[rtdPrefixSize+index*NativeSaveManagerRecordSize:]
		putPascal(record[4:4+0x100], name)
	}
	globals := make([]byte, rtdGlobalsSize)
	if progress.PuppetParams != nil {
		for slot, value := range progress.PuppetParams {
			binary.LittleEndian.PutUint16(globals[rtdPuppetParamAddr-rtdGlobalsBase+slot*2:], uint16(value))
		}
	}
	binary.LittleEndian.PutUint16(globals[rtdKeyAbortsAddr-rtdGlobalsBase:], 0)
	state, err := json.Marshal(progress)
	if err != nil {
		return nil, fmt.Errorf("encode saved game state: %w", err)
	}
	payload := make([]byte, 16, 16+len(state))
	copy(payload, rtdStateMagic)
	binary.LittleEndian.PutUint32(payload[8:], rtdStateVersion)
	binary.LittleEndian.PutUint32(payload[12:], uint32(len(state)))
	payload = append(payload, state...)
	var out bytes.Buffer
	entries := map[uint32][]byte{0: prefix, 1: globals, RTDStateResource: payload}
	if err := assets.WriteContainerTagged(&out, entries, [2]uint32{NativeSaveTagPrimary, NativeSaveTagSecondary}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// DecodeRTD reads a .rtd container. gameName is the script's name argument:
// like FUN_004235C0 it is compared case-insensitively with the name stored in
// resource 0, and a mismatch is ErrDifferentVersion.
func DecodeRTD(data []byte, gameName string) (GameProgress, error) {
	container, err := assets.NewContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return GameProgress{}, fmt.Errorf("%w (%v)", ErrNotSavedGame, err)
	}
	magic := container.Header().Magic
	if binary.LittleEndian.Uint32(magic[0:4]) != NativeSaveTagPrimary || binary.LittleEndian.Uint32(magic[4:8]) != NativeSaveTagSecondary {
		return GameProgress{}, fmt.Errorf("%w (header tags %x are not the saved game tags)", ErrNotSavedGame, magic)
	}
	prefix, err := container.ReadEntry(0)
	if err != nil || len(prefix) < rtdPrefixSize {
		return GameProgress{}, fmt.Errorf("%w (resource 0 is missing or short)", ErrNotSavedGame)
	}
	if gameName != "" && !strings.EqualFold(readPascal(prefix[0:0x100]), gameName) {
		return GameProgress{}, ErrDifferentVersion
	}
	payload, err := container.ReadEntry(RTDStateResource)
	if err != nil || len(payload) < 16 || string(payload[:8]) != rtdStateMagic {
		// A saved game of the native engine: same container and name, but
		// engine memory ReDust cannot interpret.
		return GameProgress{}, fmt.Errorf("%w (it was not written by ReDust)", ErrDifferentVersion)
	}
	if binary.LittleEndian.Uint32(payload[8:]) != rtdStateVersion {
		return GameProgress{}, fmt.Errorf("%w (ReDust save format %d)", ErrDifferentVersion, binary.LittleEndian.Uint32(payload[8:]))
	}
	length := binary.LittleEndian.Uint32(payload[12:])
	if uint64(length) != uint64(len(payload)-16) {
		return GameProgress{}, fmt.Errorf("%w (state length mismatch)", ErrNotSavedGame)
	}
	progress, err := DecodeGameProgress(payload[16:])
	if err != nil {
		return GameProgress{}, fmt.Errorf("%w (%v)", ErrNotSavedGame, err)
	}
	return progress, nil
}

// RTDPath is the path of the saved game name inside dir, with the native
// ".rtd" extension.
func RTDPath(dir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "<>:\"/\\|?*") {
		return "", fmt.Errorf("saved game file name %q is invalid", name)
	}
	return filepath.Join(dir, name+"."+NativeFileTypeRTD), nil
}

// SaveRTD writes the saved game atomically.
func SaveRTD(path string, progress GameProgress, ctx RTDContext) error {
	data, err := EncodeRTD(progress, ctx)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create saves directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".redust-save-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary save: %w", err)
	}
	temporaryPath := temporary.Name()
	_, err = temporary.Write(data)
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("write saved game: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace saved game: %w", err)
	}
	return nil
}

// LoadGameFile reads a saved game by extension: ".rtd" through DecodeRTD with
// gameName, anything else as the older ReDust JSON save.
func LoadGameFile(path, gameName string) (GameProgress, error) {
	if strings.EqualFold(filepath.Ext(path), "."+NativeFileTypeRTD) {
		data, err := os.ReadFile(path)
		if err != nil {
			return GameProgress{}, fmt.Errorf("read saved game: %w", err)
		}
		return DecodeRTD(data, gameName)
	}
	return LoadGameProgress(path)
}
