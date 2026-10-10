package assets

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

const (
	propListCountOffset       = 0x938
	propListRowsOffset        = 0x93c
	propListRowSize           = 0x10
	propDefinitionName        = 0x2a
	propViewCountOffset       = 0x5a
	propViewRowsOffset        = 0x5e
	propViewRowSize           = 0x20
	propViewNameOffset        = 0x10
	propDescriptorRows        = 0x76
	propDescriptorRowSize     = 0x2c
	propDescriptorFrameCount  = 0x70
	propDescriptorDegreeCount = 0x72
)

type PropArchive struct {
	cache         *ResourceCache
	resourceCount uint32
	definitions   map[string]PropDefinition
	order         []string
}

type PropDefinition struct {
	Resource       uint32
	ScriptResource uint32
	Name           string
	views          map[string]uint32
}

type PropView struct {
	Resource  uint32
	Name      string
	Frames    [][]uint32
	variants  []PropFrameVariant
	sequences []int16
}

type PropFrameInfo struct {
	Resource                 uint32
	Angle                    int16
	Metric                   uint16
	Left, Top, Right, Bottom int16
	OriginX, OriginY         int16
}

type PropFrameVariant struct {
	Resource                 uint32
	Sequence                 int16
	Angle                    int16
	Metric                   uint16
	Left, Top, Right, Bottom int16
	OriginX, OriginY         int16
}

func (w Workspace) OpenPropArchive(name string) (*PropArchive, error) {
	cache, err := w.OpenResourceCache(name)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			_ = cache.Close()
		}
	}()
	header, err := cache.Header()
	if err != nil {
		return nil, err
	}
	list, err := readPropResource(cache, 0)
	if err != nil {
		return nil, err
	}
	if len(list) < propListRowsOffset {
		return nil, fmt.Errorf("prop list resource is truncated")
	}
	countValue := binary.LittleEndian.Uint32(list[propListCountOffset : propListCountOffset+4])
	if uint64(countValue) > uint64((len(list)-propListRowsOffset)/propListRowSize) {
		return nil, fmt.Errorf("prop list count %d exceeds resource size %d", countValue, len(list))
	}
	count := int(countValue)
	archive := &PropArchive{cache: cache, resourceCount: header.CountB, definitions: make(map[string]PropDefinition, count)}
	for index := range count {
		row := list[propListRowsOffset+index*propListRowSize : propListRowsOffset+(index+1)*propListRowSize]
		resource := binary.LittleEndian.Uint32(row[:4])
		if resource >= header.CountB {
			return nil, fmt.Errorf("prop list row %d references resource %d outside %d entries", index, resource, header.CountB)
		}
		data, err := readPropResource(cache, resource)
		if err != nil {
			return nil, fmt.Errorf("read prop definition %d: %w", resource, err)
		}
		definition, err := parsePropDefinition(resource, data, header.CountB)
		if err != nil {
			return nil, fmt.Errorf("parse prop definition %d: %w", resource, err)
		}
		key := asciiUpper(definition.Name)
		if _, exists := archive.definitions[key]; exists {
			return nil, fmt.Errorf("prop list contains duplicate definition %q", definition.Name)
		}
		archive.definitions[key] = definition
		archive.order = append(archive.order, definition.Name)
	}
	failed = false
	return archive, nil
}

func parsePropDefinition(resource uint32, data []byte, resourceCount uint32) (PropDefinition, error) {
	if len(data) <= propDefinitionName {
		return PropDefinition{}, fmt.Errorf("definition resource is shorter than its name field")
	}
	name, err := propName(data[propDefinitionName:])
	if err != nil {
		return PropDefinition{}, err
	}
	if len(data) < propViewRowsOffset {
		return PropDefinition{}, fmt.Errorf("definition %q view table header is truncated", name)
	}
	countValue := binary.LittleEndian.Uint32(data[propViewCountOffset : propViewCountOffset+4])
	if uint64(countValue) > uint64((len(data)-propViewRowsOffset)/propViewRowSize) {
		return PropDefinition{}, fmt.Errorf("definition %q view count %d exceeds resource size %d", name, countValue, len(data))
	}
	count := int(countValue)
	definition := PropDefinition{Resource: resource, ScriptResource: binary.LittleEndian.Uint32(data[0x26:0x2a]), Name: name, views: make(map[string]uint32, count)}
	for index := range count {
		row := data[propViewRowsOffset+index*propViewRowSize : propViewRowsOffset+(index+1)*propViewRowSize]
		descriptor := binary.LittleEndian.Uint32(row[:4])
		if descriptor >= resourceCount {
			return PropDefinition{}, fmt.Errorf("view %d references descriptor %d outside %d entries", index, descriptor, resourceCount)
		}
		viewName, err := propName(row[propViewNameOffset:])
		if err != nil {
			return PropDefinition{}, fmt.Errorf("view %d name: %w", index, err)
		}
		key := asciiUpper(viewName)
		if _, exists := definition.views[key]; exists {
			return PropDefinition{}, fmt.Errorf("definition %q has duplicate view %q", name, viewName)
		}
		definition.views[key] = descriptor
	}
	return definition, nil
}

func (a *PropArchive) Definition(name string) (PropDefinition, bool) {
	definition, found := a.definitions[asciiUpper(name)]
	return definition, found
}

func (a *PropArchive) View(propName, viewName string) (PropView, error) {
	if a == nil || a.cache == nil {
		return PropView{}, fmt.Errorf("prop archive is closed")
	}
	definition, found := a.definitions[asciiUpper(propName)]
	if !found {
		return PropView{}, fmt.Errorf("prop definition %q is absent", propName)
	}
	resource, found := definition.views[asciiUpper(viewName)]
	if !found {
		return PropView{}, fmt.Errorf("prop view %q is absent from %q", viewName, definition.Name)
	}
	data, err := readPropResource(a.cache, resource)
	if err != nil {
		return PropView{}, fmt.Errorf("read prop view descriptor %d: %w", resource, err)
	}
	if len(data) < propDescriptorRows {
		return PropView{}, fmt.Errorf("prop view descriptor %d is truncated", resource)
	}
	frameCount := int(binary.LittleEndian.Uint16(data[propDescriptorFrameCount : propDescriptorFrameCount+2]))
	degreeCountValue := binary.LittleEndian.Uint32(data[propDescriptorDegreeCount : propDescriptorDegreeCount+4])
	if frameCount == 0 || degreeCountValue == 0 || uint64(degreeCountValue) > uint64((len(data)-propDescriptorRows)/propDescriptorRowSize) {
		return PropView{}, fmt.Errorf("prop view descriptor %d has invalid frames=%d degrees=%d size=%d", resource, frameCount, degreeCountValue, len(data))
	}
	degreeCount := int(degreeCountValue)
	frames := make([][]uint32, degreeCount)
	variants := make([]PropFrameVariant, degreeCount)
	sequences := make([]int16, frameCount)
	for frame := range sequences {
		sequences[frame] = int16(binary.LittleEndian.Uint16(data[0x2e+frame*2 : 0x30+frame*2]))
	}
	for degree := range frames {
		row := data[propDescriptorRows+degree*propDescriptorRowSize : propDescriptorRows+(degree+1)*propDescriptorRowSize]
		if frameCount > len(row)/4 {
			return PropView{}, fmt.Errorf("prop view descriptor %d degree %d has %d frames in a %d-byte row", resource, degree, frameCount, len(row))
		}
		frames[degree] = make([]uint32, frameCount)
		for frame := range frames[degree] {
			frameResource := binary.LittleEndian.Uint32(row[frame*4 : frame*4+4])
			if frameResource >= a.resourceCount {
				return PropView{}, fmt.Errorf("prop view descriptor %d degree %d frame %d references resource %d outside %d entries", resource, degree, frame, frameResource, a.resourceCount)
			}
			frames[degree][frame] = frameResource
		}
		variants[degree] = PropFrameVariant{Resource: binary.LittleEndian.Uint32(row[:4]), Sequence: int16(binary.LittleEndian.Uint16(row[8:10])), Angle: int16(binary.LittleEndian.Uint16(row[0x28:0x2a])), Metric: binary.LittleEndian.Uint16(row[0x2a:0x2c]), Left: int16(binary.LittleEndian.Uint16(row[0x12:0x14])), Top: int16(binary.LittleEndian.Uint16(row[0x14:0x16])), Right: int16(binary.LittleEndian.Uint16(row[0x16:0x18])), Bottom: int16(binary.LittleEndian.Uint16(row[0x18:0x1a])), OriginX: int16(binary.LittleEndian.Uint16(row[0x1a:0x1c])), OriginY: int16(binary.LittleEndian.Uint16(row[0x1c:0x1e]))}
	}
	return PropView{Resource: resource, Name: viewName, Frames: frames, variants: variants, sequences: sequences}, nil
}

func (a *PropArchive) FrameInfo(propName, viewName string, frameIndex int, angle int16) (PropFrameInfo, error) {
	if a == nil || a.cache == nil {
		return PropFrameInfo{}, fmt.Errorf("prop archive is closed")
	}
	definition, found := a.definitions[asciiUpper(propName)]
	if !found {
		return PropFrameInfo{}, fmt.Errorf("prop definition %q is absent", propName)
	}
	resource, found := definition.views[asciiUpper(viewName)]
	if !found {
		return PropFrameInfo{}, fmt.Errorf("prop view %q is absent from %q", viewName, definition.Name)
	}
	data, err := readPropResource(a.cache, resource)
	if err != nil {
		return PropFrameInfo{}, fmt.Errorf("read prop view descriptor %d: %w", resource, err)
	}
	if len(data) < propDescriptorRows {
		return PropFrameInfo{}, fmt.Errorf("prop view descriptor %d is truncated", resource)
	}
	frameCount := int(binary.LittleEndian.Uint16(data[propDescriptorFrameCount : propDescriptorFrameCount+2]))
	variantCountValue := binary.LittleEndian.Uint32(data[propDescriptorDegreeCount : propDescriptorDegreeCount+4])
	if frameIndex < 0 || frameCount == 0 || uint64(propDescriptorRows)+uint64(variantCountValue)*propDescriptorRowSize > uint64(len(data)) || 0x2e+frameCount*2 > propDescriptorRows {
		return PropFrameInfo{}, fmt.Errorf("prop view descriptor %d has invalid frame/variant tables", resource)
	}
	sequence := int16(binary.LittleEndian.Uint16(data[0x2e+(frameIndex%frameCount)*2:0x30+(frameIndex%frameCount)*2])) - 1
	closest, selected := int16(1000), false
	var result PropFrameInfo
	for index := 0; index < int(variantCountValue); index++ {
		row := data[propDescriptorRows+index*propDescriptorRowSize : propDescriptorRows+(index+1)*propDescriptorRowSize]
		if int16(binary.LittleEndian.Uint16(row[8:10])) != sequence {
			continue
		}
		distance := nativeAngleDistance(int16(binary.LittleEndian.Uint16(row[0x28:0x2a])), angle)
		if distance < closest {
			closest, selected = distance, true
			result = PropFrameInfo{Resource: binary.LittleEndian.Uint32(row[:4]), Angle: int16(binary.LittleEndian.Uint16(row[0x28:0x2a])), Metric: binary.LittleEndian.Uint16(row[0x2a:0x2c]), Left: int16(binary.LittleEndian.Uint16(row[0x12:0x14])), Top: int16(binary.LittleEndian.Uint16(row[0x14:0x16])), Right: int16(binary.LittleEndian.Uint16(row[0x16:0x18])), Bottom: int16(binary.LittleEndian.Uint16(row[0x18:0x1a])), OriginX: int16(binary.LittleEndian.Uint16(row[0x1a:0x1c])), OriginY: int16(binary.LittleEndian.Uint16(row[0x1c:0x1e]))}
			if distance < 1 {
				break
			}
		}
	}
	if !selected {
		return PropFrameInfo{}, fmt.Errorf("prop view %q frame %d has no variant for sequence %d at angle %d", viewName, frameIndex, sequence, angle)
	}
	return result, nil
}

func (v PropView) FrameInfo(frameIndex int, angle int16) (PropFrameInfo, error) {
	if frameIndex < 0 || len(v.sequences) == 0 {
		return PropFrameInfo{}, fmt.Errorf("prop view %q frame index %d is invalid", v.Name, frameIndex)
	}
	sequence := v.sequences[frameIndex%len(v.sequences)] - 1
	closest, selected := int16(1000), false
	var result PropFrameInfo
	for _, variant := range v.variants {
		if variant.Sequence != sequence {
			continue
		}
		distance := nativeAngleDistance(variant.Angle, angle)
		if distance < closest {
			closest, selected = distance, true
			result = PropFrameInfo{Resource: variant.Resource, Angle: variant.Angle, Metric: variant.Metric, Left: variant.Left, Top: variant.Top, Right: variant.Right, Bottom: variant.Bottom, OriginX: variant.OriginX, OriginY: variant.OriginY}
			if distance < 1 {
				break
			}
		}
	}
	if !selected {
		return PropFrameInfo{}, fmt.Errorf("prop view %q frame %d has no variant for sequence %d at angle %d", v.Name, frameIndex, sequence, angle)
	}
	return result, nil
}

func (v PropView) FrameResource(degree, frame int) (uint32, error) {
	if degree < 0 || degree >= len(v.Frames) || frame < 0 || frame >= len(v.Frames[degree]) {
		return 0, fmt.Errorf("prop view %q frame %d/%d is outside %d degree rows", v.Name, degree, frame, len(v.Frames))
	}
	return v.Frames[degree][frame], nil
}

func (a *PropArchive) Resource(index uint32) ([]byte, error) {
	if a == nil || a.cache == nil {
		return nil, fmt.Errorf("prop archive is closed")
	}
	if index >= a.resourceCount {
		return nil, fmt.Errorf("prop resource %d is outside %d entries", index, a.resourceCount)
	}
	return readPropResource(a.cache, index)
}

func (a *PropArchive) Close() error {
	if a == nil || a.cache == nil {
		return nil
	}
	cache := a.cache
	a.cache = nil
	a.definitions = nil
	return cache.Close()
}

func readPropResource(cache *ResourceCache, index uint32) ([]byte, error) {
	lease, err := cache.Acquire(index)
	if err != nil {
		return nil, err
	}
	data, readErr := lease.Bytes()
	closeErr := lease.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

func propName(field []byte) (string, error) {
	if len(field) == 0 || int(field[0])+1 > len(field) {
		return "", fmt.Errorf("Pascal prop name length exceeds its %d-byte field", len(field))
	}
	return strings.TrimSpace(string(field[1 : 1+int(field[0])])), nil
}

// ListNames lists the definition names in the order the shop's list rows give
// them, which is the order its props enter the game's prop table.
func (a *PropArchive) ListNames() []string {
	if a == nil {
		return nil
	}
	return append([]string(nil), a.order...)
}

// Names lists the definition names in upper case, sorted.
func (a *PropArchive) Names() []string {
	if a == nil {
		return nil
	}
	names := make([]string, 0, len(a.definitions))
	for _, definition := range a.definitions {
		names = append(names, definition.Name)
	}
	sort.Strings(names)
	return names
}
