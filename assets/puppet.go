package assets

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const (
	puppetSpeechCountOffset         = 0x86e
	puppetSpeechRowsOffset          = 0x870
	puppetSpeechRowSize             = 0x138
	puppetSpeechPanelResourceOffset = 0x85a
	puppetSpeechCueCountOffset      = 0x06
	puppetSpeechVoiceResourceOffset = 0x08
	puppetSpeechCueResourceOffset   = 0x0c
	puppetSpeechTextOffset          = 0x18
	puppetSpeechNameOffset          = 0x118
)

type PuppetSpeech struct {
	Index         uint16
	Name          string
	Subtitle      []byte
	CueFrameLimit uint16
	VoiceResource uint32
	CueResource   uint32
}

type PuppetSpeechTable struct {
	PanelResource uint32
	Entries       []PuppetSpeech
}

func (s PuppetSpeech) SubtitleText() string {
	return DecodePuppetText(s.Subtitle)
}

// HasSubtitle is the line test of FUN_00409610: a non-empty text that does not
// start with '*', is not all spaces, and whose speech name (+0x118) is none of
// the four "idle 1".."idle 4" rows (strings at 0x0045D0BC..0x0045D0A4).
func (s PuppetSpeech) HasSubtitle() bool {
	if len(s.Subtitle) == 0 || s.Subtitle[0] == '*' {
		return false
	}
	for _, idle := range []string{"idle 1", "idle 2", "idle 3", "idle 4"} {
		if strings.EqualFold(s.Name, idle) {
			return false
		}
	}
	return strings.TrimSpace(string(s.Subtitle)) != ""
}

func DecodePuppetText(data []byte) string {
	text := make([]byte, len(data))
	for index, value := range data {
		switch {
		case value >= 0x20 && value <= 0x7a:
			text[index] = value
		case value == 0x7f:
			text[index] = ' '
		case value == 0xd5:
			text[index] = '\''
		default:
			text[index] = ' '
		}
	}
	return string(text)
}

func (w Workspace) OpenPuppetSpeechTable(name string) (PuppetSpeechTable, error) {
	cache, err := w.OpenResourceCache(name)
	if err != nil {
		return PuppetSpeechTable{}, err
	}
	defer cache.Close()
	header, err := cache.Header()
	if err != nil {
		return PuppetSpeechTable{}, err
	}
	lease, err := cache.Acquire(0)
	if err != nil {
		return PuppetSpeechTable{}, err
	}
	data, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return PuppetSpeechTable{}, err
	}
	return ParsePuppetSpeechTable(data, header.CountB)
}

func ParsePuppetSpeechTable(data []byte, resourceCount uint32) (PuppetSpeechTable, error) {
	if len(data) < puppetSpeechRowsOffset {
		return PuppetSpeechTable{}, fmt.Errorf("puppet speech table header is truncated")
	}
	panelResource := binary.LittleEndian.Uint32(data[puppetSpeechPanelResourceOffset : puppetSpeechPanelResourceOffset+4])
	if panelResource >= resourceCount {
		return PuppetSpeechTable{}, fmt.Errorf("puppet choice panel references resource %d outside %d entries", panelResource, resourceCount)
	}
	count := int(binary.LittleEndian.Uint16(data[puppetSpeechCountOffset:puppetSpeechRowsOffset]))
	if count > (len(data)-puppetSpeechRowsOffset)/puppetSpeechRowSize {
		return PuppetSpeechTable{}, fmt.Errorf("puppet speech count %d exceeds resource size %d", count, len(data))
	}
	entries := make([]PuppetSpeech, count)
	for index := range entries {
		row := data[puppetSpeechRowsOffset+index*puppetSpeechRowSize : puppetSpeechRowsOffset+(index+1)*puppetSpeechRowSize]
		name, err := puppetSpeechString(row[puppetSpeechNameOffset : puppetSpeechNameOffset+32])
		if err != nil {
			return PuppetSpeechTable{}, fmt.Errorf("puppet speech row %d name: %w", index, err)
		}
		text, err := puppetSpeechString(row[puppetSpeechTextOffset:puppetSpeechNameOffset])
		if err != nil {
			return PuppetSpeechTable{}, fmt.Errorf("puppet speech row %d subtitle: %w", index, err)
		}
		voiceResource := binary.LittleEndian.Uint32(row[puppetSpeechVoiceResourceOffset : puppetSpeechVoiceResourceOffset+4])
		cueResource := binary.LittleEndian.Uint32(row[puppetSpeechCueResourceOffset : puppetSpeechCueResourceOffset+4])
		if voiceResource >= resourceCount || cueResource >= resourceCount {
			return PuppetSpeechTable{}, fmt.Errorf("puppet speech row %d references resources %d and %d outside %d entries", index, voiceResource, cueResource, resourceCount)
		}
		entries[index] = PuppetSpeech{Index: uint16(index), Name: string(name), Subtitle: text, CueFrameLimit: binary.LittleEndian.Uint16(row[puppetSpeechCueCountOffset : puppetSpeechCueCountOffset+2]), VoiceResource: voiceResource, CueResource: cueResource}
	}
	return PuppetSpeechTable{PanelResource: panelResource, Entries: entries}, nil
}

func puppetSpeechString(field []byte) ([]byte, error) {
	if len(field) == 0 || int(field[0])+1 > len(field) {
		return nil, fmt.Errorf("Pascal string length exceeds its %d-byte field", len(field))
	}
	return append([]byte(nil), field[1:1+int(field[0])]...), nil
}

// PuppetScript is one entry of a PUP's script table, resource 2: 0x28-byte
// rows after a 0x18-byte header whose +0x16 word is the count, each holding
// the script resource at +0 and a Pascal name at +8 (FUN_00408C00,
// FUN_00409160).
type PuppetScript struct {
	Name     string
	Resource uint32
}

// PuppetFile is what openpuppetfile (FUN_00407E50) reads besides the
// artwork: the puppet's name from resource 0 +0x85E and its script table.
type PuppetFile struct {
	Name    string
	Scripts []PuppetScript
}

func (w Workspace) OpenPuppetFile(name string) (PuppetFile, error) {
	cache, err := w.OpenResourceCache(name)
	if err != nil {
		return PuppetFile{}, err
	}
	defer cache.Close()
	metadata, err := readSetResource(cache, 0)
	if err != nil {
		return PuppetFile{}, fmt.Errorf("read puppet metadata: %w", err)
	}
	if len(metadata) < 0x85f || int(metadata[0x85e])+0x85f > len(metadata) || metadata[0x85e] > 31 {
		return PuppetFile{}, fmt.Errorf("puppet metadata has no name field")
	}
	file := PuppetFile{Name: string(metadata[0x85f : 0x85f+int(metadata[0x85e])])}
	table, err := readSetResource(cache, 2)
	if err != nil {
		return PuppetFile{}, fmt.Errorf("read puppet script table: %w", err)
	}
	if len(table) < 0x18 {
		return PuppetFile{}, fmt.Errorf("puppet script table is shorter than its header")
	}
	count := int(int16(binary.LittleEndian.Uint16(table[0x16:0x18])))
	if count < 0 || 0x18+count*0x28 > len(table) {
		return PuppetFile{}, fmt.Errorf("puppet script table count %d exceeds its %d bytes", count, len(table))
	}
	for index := 0; index < count; index++ {
		row := table[0x18+index*0x28 : 0x18+(index+1)*0x28]
		length := int(row[8])
		if length > 31 {
			return PuppetFile{}, fmt.Errorf("puppet script %d name is %d bytes", index, length)
		}
		file.Scripts = append(file.Scripts, PuppetScript{Name: string(row[9 : 9+length]), Resource: binary.LittleEndian.Uint32(row[0:4])})
	}
	return file, nil
}
