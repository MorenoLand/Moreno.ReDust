package assets

import (
	"encoding/binary"
	"fmt"
)

const stageSceneTableOffset = 0x838
const stageSceneRowSize = 0x1c

type StageScene struct {
	Fields [3]uint32
	Name   []byte
	Raw    [stageSceneRowSize]byte
}

type SceneHandler struct {
	ID             uint32
	Top            int16
	Left           int16
	Bottom         int16
	Right          int16
	ScriptResource uint32
	Name           []byte
}

type Stage struct {
	Width       uint16
	Height      uint16
	HeaderValue uint32
	Name        []byte
	PaletteRaw  []byte
	Scenes      []StageScene
	cache       *ResourceCache
}

func ParseStage(data []byte) (Stage, error) {
	if len(data) < stageSceneTableOffset {
		return Stage{}, fmt.Errorf("stage metadata is shorter than its %d-byte header", stageSceneTableOffset)
	}
	count := binary.LittleEndian.Uint32(data[0x834:0x838])
	if uint64(count) > uint64((len(data)-stageSceneTableOffset)/stageSceneRowSize) {
		return Stage{}, fmt.Errorf("stage scene count %d exceeds the metadata table", count)
	}
	name, err := stagePascalString(data, 0x824, 0x834)
	if err != nil {
		return Stage{}, fmt.Errorf("stage name: %w", err)
	}
	stage := Stage{Width: binary.LittleEndian.Uint16(data[0x1c:0x1e]), Height: binary.LittleEndian.Uint16(data[0x1e:0x20]), HeaderValue: binary.LittleEndian.Uint32(data[0x20:0x24]), Name: name, PaletteRaw: append([]byte(nil), data[0x24:0x824]...), Scenes: make([]StageScene, int(count))}
	for i := range stage.Scenes {
		start := stageSceneTableOffset + i*stageSceneRowSize
		row := data[start : start+stageSceneRowSize]
		scene := &stage.Scenes[i]
		copy(scene.Raw[:], row)
		for j := range scene.Fields {
			scene.Fields[j] = binary.LittleEndian.Uint32(row[j*4 : j*4+4])
		}
		scene.Name, err = stagePascalString(row, 12, stageSceneRowSize)
		if err != nil {
			return Stage{}, fmt.Errorf("stage scene %d name: %w", i, err)
		}
	}
	return stage, nil
}

func stagePascalString(data []byte, offset, end int) ([]byte, error) {
	if offset < 0 || end > len(data) || offset >= end {
		return nil, fmt.Errorf("Pascal string range [%d,%d) exceeds data length %d", offset, end, len(data))
	}
	length := int(data[offset])
	if length+1 > end-offset {
		return nil, fmt.Errorf("Pascal string at offset %d is truncated", offset)
	}
	return append([]byte(nil), data[offset:offset+length+1]...), nil
}

func (w Workspace) OpenStage(name string) (*Stage, error) {
	cache, err := w.OpenResourceCache(name)
	if err != nil {
		return nil, err
	}
	lease, err := cache.Acquire(0)
	if err != nil {
		cache.Close()
		return nil, err
	}
	data, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cache.Close()
		return nil, fmt.Errorf("read stage metadata resource: %w", err)
	}
	stage, err := ParseStage(data)
	if err != nil {
		cache.Close()
		return nil, err
	}
	stage.cache = cache
	return &stage, nil
}

func (s *Stage) AcquireSceneFrameResource(index int) (*ResourceLease, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("stage archive is unavailable")
	}
	if index < 0 || index >= len(s.Scenes) {
		return nil, fmt.Errorf("stage scene index %d is out of range", index)
	}
	return s.cache.Acquire(s.Scenes[index].Fields[1])
}

func (s *Stage) AcquireResource(index uint32) (*ResourceLease, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("stage archive is unavailable")
	}
	return s.cache.Acquire(index)
}

func (s *Stage) HitTestSceneHandler(index int, point uint32) (SceneHandler, bool, error) {
	if s == nil || s.cache == nil {
		return SceneHandler{}, false, fmt.Errorf("stage archive is unavailable")
	}
	if index < 0 || index >= len(s.Scenes) {
		return SceneHandler{}, false, fmt.Errorf("stage scene index %d is out of range", index)
	}
	lease, err := s.cache.Acquire(s.Scenes[index].Fields[2])
	if err != nil {
		return SceneHandler{}, false, err
	}
	data, readErr := lease.Bytes()
	closeErr := lease.Close()
	if readErr != nil {
		return SceneHandler{}, false, readErr
	}
	if closeErr != nil {
		return SceneHandler{}, false, closeErr
	}
	if len(data) < 4 {
		return SceneHandler{}, false, fmt.Errorf("scene %d handler table is shorter than its count", index)
	}
	count := int(binary.LittleEndian.Uint32(data[:4]))
	if count > (len(data)-4)/32 {
		return SceneHandler{}, false, fmt.Errorf("scene %d handler count %d exceeds its table", index, count)
	}
	x, y := int16(point>>16), int16(point)
	for i := count - 1; i >= 0; i-- {
		row := data[4+i*32 : 4+(i+1)*32]
		if x < int16(binary.LittleEndian.Uint16(row[6:8])) || x >= int16(binary.LittleEndian.Uint16(row[10:12])) || y < int16(binary.LittleEndian.Uint16(row[4:6])) || y >= int16(binary.LittleEndian.Uint16(row[8:10])) {
			continue
		}
		length := int(row[16])
		if length+1 > 16 {
			return SceneHandler{}, false, fmt.Errorf("scene %d handler row %d has a truncated name", index, i)
		}
		return SceneHandler{ID: binary.LittleEndian.Uint32(row[:4]), Top: int16(binary.LittleEndian.Uint16(row[4:6])), Left: int16(binary.LittleEndian.Uint16(row[6:8])), Bottom: int16(binary.LittleEndian.Uint16(row[8:10])), Right: int16(binary.LittleEndian.Uint16(row[10:12])), ScriptResource: binary.LittleEndian.Uint32(row[12:16]), Name: append([]byte(nil), row[16:17+length]...)}, true, nil
	}
	return SceneHandler{}, false, nil
}

func (s *Stage) Close() error {
	if s == nil || s.cache == nil {
		return nil
	}
	cache := s.cache
	if err := cache.Close(); err != nil {
		return err
	}
	s.cache = nil
	return nil
}
