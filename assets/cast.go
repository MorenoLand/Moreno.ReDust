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
const castPoseCountOffset = 0x5a
const castPoseRowsOffset = 0x5e
const castPoseRowSize = 0x20
const castPoseNameOffset = 0x10
const setCoordinateHeaderSize = 0x1c
const setCoordinateRowSize = 0x32

type CastPose struct {
	Name     string
	Resource uint32
}

type CastPoseFrameInfo struct {
	Resource uint32
	Angle    int16
	Metric   uint16
}

type CastActor struct {
	Resource uint32
	Name     string
	Selector string
	Location string
	Script   string
	Poses    []CastPose
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
		poseCount := int(binary.LittleEndian.Uint16(data[castPoseCountOffset : castPoseCountOffset+2]))
		if poseCount > (len(data)-castPoseRowsOffset)/castPoseRowSize {
			return Cast{}, fmt.Errorf("cast actor resource %d pose count %d exceeds its table", resource, poseCount)
		}
		poses := make([]CastPose, poseCount)
		for poseIndex := range poses {
			poseOffset := castPoseRowsOffset + poseIndex*castPoseRowSize
			poseResource := binary.LittleEndian.Uint32(data[poseOffset : poseOffset+4])
			if poseResource >= header.CountB {
				return Cast{}, fmt.Errorf("cast actor resource %d pose %d references resource %d outside %d entries", resource, poseIndex, poseResource, header.CountB)
			}
			poseName, err := castPascalAt(data, poseOffset+castPoseNameOffset)
			if err != nil {
				return Cast{}, fmt.Errorf("cast actor resource %d pose %d name: %w", resource, poseIndex, err)
			}
			poses[poseIndex] = CastPose{Name: poseName, Resource: poseResource}
		}
		actors[index] = CastActor{Resource: resource, Name: values[0], Selector: values[1], Location: values[2], Script: values[3], Poses: poses}
	}
	return Cast{Name: name, Actors: actors}, nil
}

func (a CastActor) PoseResource(name string) (uint32, bool) {
	for _, pose := range a.Poses {
		if strings.EqualFold(pose.Name, name) {
			return pose.Resource, true
		}
	}
	return 0, false
}

func (w Workspace) CastPoseFrameResource(c Cast, actor CastActor, poseName string, frameIndex int, relativeAngle int16) (uint32, bool, error) {
	frame, found, err := w.CastPoseFrame(c, actor, poseName, frameIndex, relativeAngle)
	return frame.Resource, found, err
}

func (w Workspace) CastPoseFrame(c Cast, actor CastActor, poseName string, frameIndex int, relativeAngle int16) (CastPoseFrameInfo, bool, error) {
	descriptor, found := actor.PoseResource(poseName)
	if !found {
		return CastPoseFrameInfo{}, false, nil
	}
	cache, err := w.OpenResourceCache(c.Name)
	if err != nil {
		return CastPoseFrameInfo{}, false, err
	}
	defer cache.Close()
	header, err := cache.Header()
	if err != nil {
		return CastPoseFrameInfo{}, false, err
	}
	if descriptor >= header.CountB {
		return CastPoseFrameInfo{}, false, fmt.Errorf("cast pose %q references resource %d outside %d entries", poseName, descriptor, header.CountB)
	}
	data, err := readSetResource(cache, descriptor)
	if err != nil {
		return CastPoseFrameInfo{}, false, fmt.Errorf("read cast pose %q descriptor %d: %w", poseName, descriptor, err)
	}
	if len(data) < 0x76 {
		return CastPoseFrameInfo{}, false, fmt.Errorf("cast pose descriptor %d is shorter than its frame tables", descriptor)
	}
	sequenceCount := int(binary.LittleEndian.Uint16(data[0x70:0x72]))
	if sequenceCount < 1 || frameIndex < 0 || 0x2e+sequenceCount*2 > len(data) {
		return CastPoseFrameInfo{}, false, fmt.Errorf("cast pose descriptor %d frame index %d is outside %d sequences", descriptor, frameIndex, sequenceCount)
	}
	frameIndex %= sequenceCount
	sequence := int16(binary.LittleEndian.Uint16(data[0x2e+frameIndex*2:0x30+frameIndex*2])) - 1
	variantCount := uint64(binary.LittleEndian.Uint32(data[0x72:0x76]))
	if variantCount > uint64((len(data)-0x76)/0x2c) {
		return CastPoseFrameInfo{}, false, fmt.Errorf("cast pose descriptor %d has %d variants outside its table", descriptor, variantCount)
	}
	closest, selected := int16(1000), false
	selectedFrame := CastPoseFrameInfo{}
	for index := uint64(0); index < variantCount; index++ {
		row := data[0x76+int(index)*0x2c : 0x76+int(index+1)*0x2c]
		if int16(binary.LittleEndian.Uint16(row[8:10])) != sequence {
			continue
		}
		distance := nativeAngleDistance(int16(binary.LittleEndian.Uint16(row[0x28:0x2a])), relativeAngle)
		if distance < closest {
			closest, selected = distance, true
			selectedFrame = CastPoseFrameInfo{Resource: binary.LittleEndian.Uint32(row[:4]), Angle: int16(binary.LittleEndian.Uint16(row[0x28:0x2a])), Metric: binary.LittleEndian.Uint16(row[0x2a:0x2c])}
			if distance < 1 {
				break
			}
		}
	}
	if !selected {
		return CastPoseFrameInfo{}, false, nil
	}
	if selectedFrame.Resource >= header.CountB {
		return CastPoseFrameInfo{}, false, fmt.Errorf("cast pose %q variant references resource %d outside %d entries", poseName, selectedFrame.Resource, header.CountB)
	}
	return selectedFrame, true, nil
}

func nativeAngleDistance(left, right int16) int16 {
	if right < left {
		left, right = right, left
	}
	direct, wrapped := right-left, left-right+0x100
	if wrapped <= direct {
		return wrapped
	}
	return direct
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
