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
// The blocks ReDust writes and reads at their native offsets:
//
//   - the APPL envelope: version 0x00010000, the two tags above, 128-entry
//     index pages, 64-byte record alignment (assets.WriteContainerTagged). The
//     record size is FUN_00402CC0's rounding, and the padding is FUN_00402030's;
//   - resource 0, the 0x1210-byte prefix plus one 0x104-byte record per open
//     APPL file:
//       +0x000  the game name, a Pascal string. FUN_004235C0 compares it with
//               the script's name argument through FUN_0042E5B0.
//       +0x100  nine 0x100-byte script context pages, page n at +0x100+n*0x100.
//               FUN_00423F20 restores pages 1..8 and keeps page 0.
//       +0xA00, +0xA04, +0xA08  three metadata words. ReDust writes zero, which
//               FUN_00424610 accepts as "no reference". Their native values are
//               not decoded.
//       +0xA0C  256 eight-byte palette entries from DAT_004590FC + 8.
//       +0x120C the manager record count. FUN_00424690 reads it (FUN_00424580 and
//               FUN_00424610 call that), but no store to it appears in the
//               serializer's decompile, so ReDust writes the count it reads.
//       +0x1210 + n*0x104  a manager record: a 4-byte node pointer (zero here,
//               since a process pointer has no equivalent) and a 0x100-byte
//               Pascal file name.
//   - resource 1, the 0x21E-byte globals block at DAT_004598A0: the keyaborts
//     word at +0xF6 (DAT_00459996) and the eight puppetparam words at +0xFC
//     (DAT_0045999C..AA). The other words are zero. The actor, cast, prop, shop
//     and track counts at +0x13C, +0x144, +0x14C, +0x154 and +0x15C size the
//     loops over resources 2..6, so they stay zero while those are unwritten.
//     The native load keeps the path strings at +0x00 and +0x7A and reads the
//     rest from the file.
//
// Resources 2..N are not written. They hold the actor (0xA4 stride), cast (0x1C),
// prop (0x9E), shop (0x1C) and track (0x26) tables, the per-track row arrays (0x68
// stride), the DAT_004596F8 registry, the three job tables and the job-slot handles.
// Their field layouts are not decoded, so a ReDust save is not loadable by real
// DUST, and a native save is not loadable by ReDust (it has no resource 99).
//
// Everything else lives in resource 99, which is ReDust's own: the magic
// "REDUSTSV", a version dword, a length dword and the JSON of GameProgress. The
// JSON leaves out the three fields the native blocks carry (the context pages,
// the puppet parameters and the keyaborts flag), so each is stored once, at its
// native offset.

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
	// The interpreter's pages, page 0 through 8 at +0x100 (FUN_00422DE0 calls
	// FUN_00417820 for each). A progress with no pages writes the boot state.
	texts := progress.ScriptContextPages
	if len(texts) == 0 {
		texts = scripts.BootContextPageTexts()
	}
	pages, err := scripts.ContextPagesFromTexts(texts)
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
	binary.LittleEndian.PutUint32(prefix[resource0ManagerCountOffset:], uint32(len(ctx.OpenFiles)))
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
	if progress.KeyAborts != nil {
		binary.LittleEndian.PutUint16(globals[rtdKeyAbortsAddr-rtdGlobalsBase:], uint16(*progress.KeyAborts))
	}
	// The three fields the native blocks carry are stored only there.
	state := progress
	state.ScriptContextPages, state.PuppetParams, state.KeyAborts = nil, nil, nil
	stateData, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode saved game state: %w", err)
	}
	payload := make([]byte, 16, 16+len(stateData))
	copy(payload, rtdStateMagic)
	binary.LittleEndian.PutUint32(payload[8:], rtdStateVersion)
	binary.LittleEndian.PutUint32(payload[12:], uint32(len(stateData)))
	payload = append(payload, stateData...)
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
	globals, err := container.ReadEntry(1)
	if err != nil || len(globals) < rtdGlobalsSize {
		return GameProgress{}, fmt.Errorf("%w (resource 1 is missing or short)", ErrNotSavedGame)
	}
	blocks, err := readRTDBlocks(prefix, globals)
	if err != nil {
		return GameProgress{}, fmt.Errorf("%w (%v)", ErrNotSavedGame, err)
	}
	progress, err := DecodeGameProgress(payload[16:])
	if err != nil {
		return GameProgress{}, fmt.Errorf("%w (%v)", ErrNotSavedGame, err)
	}
	progress.ScriptContextPages = blocks.ContextPages
	progress.PuppetParams = &blocks.PuppetParams
	progress.KeyAborts = &blocks.KeyAborts
	return progress, nil
}

// RTDBlocks is what the reader takes from the native-layout resources 0 and 1. Each
// field is read at the offset the reference uses; the fields ReDust does not model
// are returned raw.
type RTDBlocks struct {
	// ContextPages are the texts of script context pages 0 through 8.
	ContextPages [][]byte
	// Metadata are the words at resource 0 +0xA00, +0xA04 and +0xA08, raw.
	Metadata [3]uint32
	// Palette is the 0x800-byte palette at resource 0 +0xA0C.
	Palette []byte
	// Managers are the file names of the manager records at resource 0 +0x1210,
	// in the order the count at +0x120C gives.
	Managers []string
	// KeyAborts is the keyaborts word at resource 1 +0xF6 (DAT_00459996).
	KeyAborts int16
	// PuppetParams are the eight words at resource 1 +0xFC (DAT_0045999C..AA).
	PuppetParams [8]int16
}

// readRTDBlocks decodes resource 0 (prefix) and resource 1 (globals) at their native
// offsets.
func readRTDBlocks(prefix, globals []byte) (RTDBlocks, error) {
	if len(globals) < rtdGlobalsSize {
		return RTDBlocks{}, fmt.Errorf("save resource 1 has %d bytes, want %#x", len(globals), rtdGlobalsSize)
	}
	resource0, err := DecodeResource0(prefix)
	if err != nil {
		return RTDBlocks{}, err
	}
	pages, err := resource0.ContextPages()
	if err != nil {
		return RTDBlocks{}, err
	}
	metadata, err := resource0.MetadataWords()
	if err != nil {
		return RTDBlocks{}, err
	}
	palette, err := resource0.EntryTableBytes()
	if err != nil {
		return RTDBlocks{}, err
	}
	managers, err := resource0.ManagerNames()
	if err != nil {
		return RTDBlocks{}, err
	}
	blocks := RTDBlocks{
		ContextPages: pages.Texts(),
		Metadata:     metadata,
		Palette:      palette,
		Managers:     managers,
		KeyAborts:    int16(binary.LittleEndian.Uint16(globals[rtdKeyAbortsAddr-rtdGlobalsBase:])),
	}
	for slot := range blocks.PuppetParams {
		blocks.PuppetParams[slot] = int16(binary.LittleEndian.Uint16(globals[rtdPuppetParamAddr-rtdGlobalsBase+slot*2:]))
	}
	return blocks, nil
}

// ReadRTDBlocks reads resources 0 and 1 of a .rtd container at their native offsets,
// without reading resource 99.
func ReadRTDBlocks(data []byte) (RTDBlocks, error) {
	container, err := assets.NewContainer(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return RTDBlocks{}, fmt.Errorf("%w (%v)", ErrNotSavedGame, err)
	}
	prefix, err := container.ReadEntry(0)
	if err != nil || len(prefix) < rtdPrefixSize {
		return RTDBlocks{}, fmt.Errorf("%w (resource 0 is missing or short)", ErrNotSavedGame)
	}
	globals, err := container.ReadEntry(1)
	if err != nil {
		return RTDBlocks{}, fmt.Errorf("%w (resource 1 is missing)", ErrNotSavedGame)
	}
	return readRTDBlocks(prefix, globals)
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
