package assets

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const castTableCountOffset = 0x938
const castTableRowsOffset = 0x93c
const castTableRowSize = 0x10
const castRecordMinimumSize = 0x6f
const setCoordinateHeaderSize = 0x1c
const setCoordinateRowSize = 0x32

type CastActor struct {
	Resource uint32
	Name     string
	Selector string
	Location string
	Script   string
	Position [3]int16
	Located  bool
}

type Cast struct {
	Name   string
	Actors []CastActor
}

func (w Workspace) OpenCast(name string) (Cast, error) {
	cache, err := w.OpenResourceCache(name)
	if err != nil {
		return Cast{}, err
	}
	defer cache.Close()
	header, err := cache.Header()
	if err != nil {
		return Cast{}, err
	}
	metadata, err := readSetResource(cache, 0)
	if err != nil {
		return Cast{}, fmt.Errorf("read cast metadata: %w", err)
	}
	if len(metadata) < castTableRowsOffset {
		return Cast{}, fmt.Errorf("cast metadata is shorter than its actor table header")
	}
	count := uint64(binary.LittleEndian.Uint32(metadata[castTableCountOffset:castTableRowsOffset]))
	if count > uint64((len(metadata)-castTableRowsOffset)/castTableRowSize) {
		return Cast{}, fmt.Errorf("cast actor count %d exceeds metadata table", count)
	}
	actors := make([]CastActor, int(count))
	for index := range actors {
		offset := castTableRowsOffset + index*castTableRowSize
		resource := binary.LittleEndian.Uint32(metadata[offset : offset+4])
		if resource >= header.CountB {
			return Cast{}, fmt.Errorf("cast actor row %d references resource %d outside %d entries", index, resource, header.CountB)
		}
		data, err := readSetResource(cache, resource)
		if err != nil {
			return Cast{}, fmt.Errorf("read cast actor resource %d: %w", resource, err)
		}
		if len(data) < castRecordMinimumSize {
			return Cast{}, fmt.Errorf("cast actor resource %d is shorter than its 0x6e-byte record", resource)
		}
		fields := [...]int{0x2a, 0x3a, 0x4a, 0x6e}
		values := [4]string{}
		for field, fieldOffset := range fields {
			values[field], err = castPascalAt(data, fieldOffset)
			if err != nil {
				return Cast{}, fmt.Errorf("cast actor resource %d field %#x: %w", resource, fieldOffset, err)
			}
		}
		actors[index] = CastActor{Resource: resource, Name: values[0], Selector: values[1], Location: values[2], Script: values[3]}
	}
	return Cast{Name: name, Actors: actors}, nil
}

func (c Cast) ResolveLocations(set *Set, selector string) ([]CastActor, error) {
	actors := make([]CastActor, 0, len(c.Actors))
	for _, actor := range c.Actors {
		if actor.Selector != "" && !strings.EqualFold(actor.Selector, selector) || actor.Location == "" {
			continue
		}
		position, found, err := set.ResolveLocation(actor.Location)
		if err != nil {
			return nil, fmt.Errorf("resolve cast actor %q location %q: %w", actor.Name, actor.Location, err)
		}
		if !found {
			continue
		}
		actor.Position, actor.Located = position, true
		actors = append(actors, actor)
	}
	return actors, nil
}

func (s *Set) ResolveLocation(name string) ([3]int16, bool, error) {
	if s == nil || s.cache == nil {
		return [3]int16{}, false, fmt.Errorf("SET is closed")
	}
	data, err := readSetResource(s.cache, s.secondaryResource)
	if err != nil {
		return [3]int16{}, false, fmt.Errorf("read SET coordinate table: %w", err)
	}
	if len(data) < setCoordinateHeaderSize {
		return [3]int16{}, false, fmt.Errorf("SET coordinate table header is truncated")
	}
	count := uint64(binary.LittleEndian.Uint32(data[0x18:0x1c]))
	if count > uint64((len(data)-setCoordinateHeaderSize)/setCoordinateRowSize) {
		return [3]int16{}, false, fmt.Errorf("SET coordinate count %d exceeds its table", count)
	}
	for index := uint64(0); index < count; index++ {
		row := data[setCoordinateHeaderSize+int(index)*setCoordinateRowSize : setCoordinateHeaderSize+int(index+1)*setCoordinateRowSize]
		primary, err := castPascalAt(row, 8)
		if err != nil {
			return [3]int16{}, false, fmt.Errorf("SET coordinate row %d primary name: %w", index, err)
		}
		if strings.EqualFold(primary, name) {
			return coordinatePoint(row, 2), true, nil
		}
		if binary.LittleEndian.Uint32(row[0x18:0x1c]) != 0 {
			alias, err := castPascalAt(row, 0x22)
			if err != nil {
				return [3]int16{}, false, fmt.Errorf("SET coordinate row %d alias: %w", index, err)
			}
			if strings.EqualFold(alias, name) {
				return coordinatePoint(row, 0x1c), true, nil
			}
		}
	}
	return [3]int16{}, false, nil
}

func coordinatePoint(row []byte, offset int) [3]int16 {
	return [3]int16{int16(binary.LittleEndian.Uint16(row[offset : offset+2])), int16(binary.LittleEndian.Uint16(row[offset+2 : offset+4])), int16(binary.LittleEndian.Uint16(row[offset+4 : offset+6]))}
}

func castPascalAt(data []byte, offset int) (string, error) {
	if offset < 0 || offset >= len(data) {
		return "", fmt.Errorf("Pascal string offset %d is outside %d bytes", offset, len(data))
	}
	length := int(data[offset])
	if length+1 > len(data)-offset {
		return "", fmt.Errorf("Pascal string at offset %d is truncated", offset)
	}
	return string(data[offset+1 : offset+1+length]), nil
}
