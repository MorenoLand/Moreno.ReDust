package save

import (
	"encoding/binary"
	"fmt"

	"redust/render"
	"redust/scripts"
)

const resource0PrefixSize = 0x1210
const resource0ContextOffset = 0x100
const resource0ContextSize = 0x900
const resource0MetadataOffset = 0xa00
const resource0EntryOffset = 0xa0c
const resource0EntrySize = 0x800

// resource0ManagerCountOffset is the dword FUN_00424690 reads as the number of
// manager records. It sits just past the palette (0xA0C + 0x800).
const resource0ManagerCountOffset = 0x120c

type Resource0 struct {
	data []byte
}

func DecodeResource0(data []byte) (Resource0, error) {
	if len(data) < resource0PrefixSize {
		return Resource0{}, fmt.Errorf("save resource 0 has %d bytes, want at least %#x", len(data), resource0PrefixSize)
	}
	return Resource0{data: append([]byte(nil), data...)}, nil
}

func (r Resource0) MarshalBinary() ([]byte, error) {
	if len(r.data) < resource0PrefixSize {
		return nil, fmt.Errorf("save resource 0 has %d bytes, want at least %#x", len(r.data), resource0PrefixSize)
	}
	return append([]byte(nil), r.data...), nil
}

func (r Resource0) MetadataWords() ([3]uint32, error) {
	if len(r.data) < resource0PrefixSize {
		return [3]uint32{}, fmt.Errorf("save resource 0 has %d bytes, want at least %#x", len(r.data), resource0PrefixSize)
	}
	return [3]uint32{
		binary.LittleEndian.Uint32(r.data[resource0MetadataOffset : resource0MetadataOffset+4]),
		binary.LittleEndian.Uint32(r.data[resource0MetadataOffset+4 : resource0MetadataOffset+8]),
		binary.LittleEndian.Uint32(r.data[resource0MetadataOffset+8 : resource0MetadataOffset+12]),
	}, nil
}

func (r Resource0) EntryTableBytes() ([]byte, error) {
	if len(r.data) < resource0PrefixSize {
		return nil, fmt.Errorf("save resource 0 has %d bytes, want at least %#x", len(r.data), resource0PrefixSize)
	}
	return append([]byte(nil), r.data[resource0EntryOffset:resource0EntryOffset+resource0EntrySize]...), nil
}

func (r Resource0) PaletteState() (render.PaletteState, error) {
	entries, err := r.EntryTableBytes()
	if err != nil {
		return render.PaletteState{}, err
	}
	return render.PaletteStateFromRaw(entries)
}

func (r Resource0) ContextPages() (scripts.ScriptContextPages, error) {
	var pages scripts.ScriptContextPages
	if len(r.data) < resource0PrefixSize {
		return pages, fmt.Errorf("save resource 0 has %d bytes, want at least %#x", len(r.data), resource0PrefixSize)
	}
	if err := pages.UnmarshalBinary(r.data[resource0ContextOffset : resource0ContextOffset+resource0ContextSize]); err != nil {
		return pages, err
	}
	return pages, nil
}

func (r *Resource0) StoreContextPages(pages scripts.ScriptContextPages) error {
	if r == nil || len(r.data) < resource0PrefixSize {
		return fmt.Errorf("save resource 0 is unavailable or truncated")
	}
	data, err := pages.MarshalBinary()
	if err != nil {
		return err
	}
	copy(r.data[resource0ContextOffset:resource0ContextOffset+resource0ContextSize], data)
	return nil
}

func (r Resource0) RestoreContextPages(current *scripts.ScriptContextPages) error {
	if len(r.data) < resource0PrefixSize || current == nil {
		return fmt.Errorf("save resource 0 or script contexts are unavailable")
	}
	data, err := current.MarshalBinary()
	if err != nil {
		return err
	}
	copy(data[0x100:], r.data[resource0ContextOffset+0x100:resource0ContextOffset+resource0ContextSize])
	return current.UnmarshalBinary(data)
}

// ManagerNames returns the file name of each manager record at +0x1210, in the count
// the dword at +0x120C gives. A count that does not fit the block is refused.
func (r Resource0) ManagerNames() ([]string, error) {
	if len(r.data) < resource0PrefixSize {
		return nil, fmt.Errorf("save resource 0 has %d bytes, want at least %#x", len(r.data), resource0PrefixSize)
	}
	count := binary.LittleEndian.Uint32(r.data[resource0ManagerCountOffset : resource0ManagerCountOffset+4])
	available := (len(r.data) - resource0PrefixSize) / NativeSaveManagerRecordSize
	if uint64(count) > uint64(available) {
		return nil, fmt.Errorf("save resource 0 names %d manager records but holds %d", count, available)
	}
	names := make([]string, 0, count)
	for index := 0; index < int(count); index++ {
		offset := resource0PrefixSize + index*NativeSaveManagerRecordSize
		// The first four bytes are the node pointer; the Pascal name follows.
		names = append(names, readPascal(r.data[offset+4:offset+NativeSaveManagerRecordSize]))
	}
	return names, nil
}
