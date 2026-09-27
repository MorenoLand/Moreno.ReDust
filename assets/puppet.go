package assets

import (
	"encoding/binary"
	"fmt"
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
