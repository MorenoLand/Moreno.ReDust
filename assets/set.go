package assets

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const setViewTableOffset = 0x1c10
const setViewRowSize = 0x20
const setBackgroundRowSize = 0x1c
const setPaletteOffset = 0x1270
const setPaletteSize = 0x800

const (
	SetDirectionNorth int16 = 1
	SetDirectionSouth int16 = 2
	SetDirectionEast  int16 = 3
	SetDirectionWest  int16 = 4
)

type SetView struct {
	SceneID        uint16
	DirectionID    uint16
	Flags          uint16
	ReferenceIndex uint16
	Name           []byte
	Resource       uint32
}

// IsCell reports whether the view is a walkable cell of the set's graph.
//
// SetView.Flags bit 0 is set on every view that has no entry in the set's background
// table. Across the five sets measured, the correspondence is exact in both directions:
// NITE.SET has 225 views of which 52 carry Flags == 0 and the table spans exactly those
// same 52 (DirectionID, SceneID) pairs; BANK.SET 3 of 12 and 3; COURT.SET 8 of 20 and 8;
// UNDER/HUB.SET 16 of 49 and 16; STORE.SET 2 of 12 and 2. So bit 0 clear means "this view
// is a real cell" and set means "this view is a backdrop with no edges".
//
// Only Flags values 0 and 1 occur, so the test is written against the bit; the observed
// data cannot distinguish it from Flags == 0 and the bit form is the one that survives a
// future flag being added.
func (v SetView) IsCell() bool {
	return v.Flags&1 == 0
}

// CellViews returns the set's walkable cells in table order.
func (s *Set) CellViews() []SetView {
	if s == nil {
		return nil
	}
	cells := make([]SetView, 0, len(s.views))
	for _, view := range s.views {
		if view.IsCell() {
			cells = append(cells, view)
		}
	}
	return cells
}

type Set struct {
	cache              *ResourceCache
	views              []SetView
	backgroundResource uint32
	backgroundCount    int
	backgroundTable    []byte
	palette            []byte
	secondaryResource  uint32
	resourceCount      uint32
}

func (w Workspace) OpenSet(name string) (*Set, error) {
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
	metadata, err := readSetResource(cache, 0)
	if err != nil {
		return nil, fmt.Errorf("read SET metadata: %w", err)
	}
	if len(metadata) < setPaletteOffset+setPaletteSize || len(metadata) < setViewTableOffset {
		return nil, fmt.Errorf("SET metadata is shorter than its view and palette tables")
	}
	viewCount := binary.LittleEndian.Uint32(metadata[0x1c0c:0x1c10])
	if uint64(viewCount) > uint64((len(metadata)-setViewTableOffset)/setViewRowSize) {
		return nil, fmt.Errorf("SET view count %d exceeds its metadata table", viewCount)
	}
	backgroundCount := int(binary.LittleEndian.Uint32(metadata[8:12]))
	backgroundResource := binary.LittleEndian.Uint32(metadata[0x1e:0x22])
	secondaryResource := binary.LittleEndian.Uint32(metadata[0x22:0x26])
	if backgroundResource >= header.CountB || secondaryResource >= header.CountB {
		return nil, fmt.Errorf("SET table resources %d/%d exceed %d entries", backgroundResource, secondaryResource, header.CountB)
	}
	backgroundTable, err := readSetResource(cache, backgroundResource)
	if err != nil {
		return nil, fmt.Errorf("read SET background table resource %d: %w", backgroundResource, err)
	}
	if backgroundCount < 0 || uint64(backgroundCount)*setBackgroundRowSize > uint64(len(backgroundTable)) {
		return nil, fmt.Errorf("SET background row count %d exceeds resource %d (%d bytes)", backgroundCount, backgroundResource, len(backgroundTable))
	}
	views := make([]SetView, int(viewCount))
	for index := range views {
		offset := setViewTableOffset + index*setViewRowSize
		row := metadata[offset : offset+setViewRowSize]
		viewName, err := stagePascalString(row, 12, len(row))
		if err != nil {
			return nil, fmt.Errorf("SET view %d name: %w", index, err)
		}
		resource := binary.LittleEndian.Uint32(row[28:32])
		if resource >= header.CountB {
			return nil, fmt.Errorf("SET view %d references resource %d outside %d entries", index, resource, header.CountB)
		}
		views[index] = SetView{SceneID: binary.LittleEndian.Uint16(row[0:2]), DirectionID: binary.LittleEndian.Uint16(row[2:4]), Flags: binary.LittleEndian.Uint16(row[8:10]), ReferenceIndex: binary.LittleEndian.Uint16(row[10:12]), Name: viewName, Resource: resource}
	}
	failed = false
	return &Set{cache: cache, views: views, backgroundResource: backgroundResource, backgroundCount: backgroundCount, backgroundTable: backgroundTable, palette: append([]byte(nil), metadata[setPaletteOffset:setPaletteOffset+setPaletteSize]...), secondaryResource: secondaryResource, resourceCount: header.CountB}, nil
}

func readSetResource(cache *ResourceCache, index uint32) ([]byte, error) {
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

func (s *Set) Views() []SetView {
	if s == nil {
		return nil
	}
	views := make([]SetView, len(s.views))
	for index, view := range s.views {
		view.Name = append([]byte(nil), view.Name...)
		views[index] = view
	}
	return views
}

func (s *Set) FindView(name string) (SetView, bool) {
	if s == nil {
		return SetView{}, false
	}
	for _, view := range s.views {
		if len(view.Name) > 0 && strings.EqualFold(string(view.Name[1:]), name) {
			view.Name = append([]byte(nil), view.Name...)
			return view, true
		}
	}
	return SetView{}, false
}

func (s *Set) FindViewByIDs(directionID, sceneID uint16) (SetView, bool) {
	if s == nil {
		return SetView{}, false
	}
	for _, view := range s.views {
		if view.DirectionID == directionID && view.SceneID == sceneID {
			view.Name = append([]byte(nil), view.Name...)
			return view, true
		}
	}
	return SetView{}, false
}

func (s *Set) BackgroundResourceForPoints(from, to [3]int16) (uint32, bool, error) {
	if s == nil {
		return 0, false, fmt.Errorf("SET is unavailable")
	}
	for index := 0; index < s.backgroundCount; index++ {
		offset := index * setBackgroundRowSize
		row := s.backgroundTable[offset : offset+setBackgroundRowSize]
		matches := true
		for axis, point := range from {
			if int16(binary.LittleEndian.Uint16(row[axis*2:axis*2+2])) != point {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		for axis, point := range to {
			if int16(binary.LittleEndian.Uint16(row[(axis+3)*2:(axis+4)*2])) != point {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		resource := binary.LittleEndian.Uint32(row[24:28])
		if resource >= s.resourceCount {
			return 0, false, fmt.Errorf("SET background row %d references resource %d outside %d entries", index, resource, s.resourceCount)
		}
		return resource, true, nil
	}
	return 0, false, nil
}

// ActorCellVisible reports whether a world position stands in the cell the displayed
// background depicts.
//
// A set's walkable cells are the (DirectionID, SceneID) pairs its background table names,
// and SetView.IsCell separates them from the backdrop-only views. A world position maps
// onto that same grid by dividing by 256, the inverse of the conversion
// NativeActorCameraPosition applies to a cell to reach a camera position, and every
// coordinate point in NITE.SET lands inside it: 36 points over 29 distinct cells, none
// outside 0..14 on either axis.
//
// The cell on screen is the player's own cell stepped once along the player's facing
// direction, because that is exactly how the displayed backdrop is chosen --
// BackgroundResourceForDirection(view, point[2]) names the edge leaving the player's cell in
// direction point[2], and its background is what gets drawn. So an actor is visible when it
// stands in the player's cell or in the cell that edge leads to, and nowhere else. Each cell
// carries its own painted backdrop per direction, so a cell two or more steps away is
// covered by a different backdrop and cannot be on screen at all.
//
// This is the whole of the visibility rule, and it is a partition rather than an occlusion
// test: the reference's draw path has no wall mask, no per-pixel occlusion and no cell test
// -- its cull is a depth test alone -- so cells are separated by which backdrop is drawn,
// not by anything that knows about walls.
func (s *Set) ActorCellVisible(playerPoint [3]int16, position [3]int16) bool {
	if s == nil || s.cache == nil {
		return true
	}
	playerDirection, playerScene := int(playerPoint[0]), int(playerPoint[1])
	actorDirection, actorScene := int(position[0])/256, int(position[1])/256
	if actorDirection == playerDirection && actorScene == playerScene {
		return true
	}
	stepDirection, stepScene := playerDirection, playerScene
	switch playerPoint[2] {
	case SetDirectionNorth:
		stepScene--
	case SetDirectionSouth:
		stepScene++
	case SetDirectionEast:
		stepDirection++
	case SetDirectionWest:
		stepDirection--
	default:
		return false
	}
	return actorDirection == stepDirection && actorScene == stepScene
}

func (s *Set) BackgroundResourceForDirection(view SetView, direction int16) (uint32, bool, error) {
	if direction < SetDirectionNorth || direction > SetDirectionWest {
		return 0, false, fmt.Errorf("SET direction %d is invalid", direction)
	}
	from := [3]int16{int16(view.DirectionID), int16(view.SceneID), direction}
	to := from
	switch direction {
	case SetDirectionNorth:
		to[1]--
	case SetDirectionSouth:
		to[1]++
	case SetDirectionEast:
		to[0]++
	case SetDirectionWest:
		to[0]--
	}
	resource, found, err := s.BackgroundResourceForPoints(from, to)
	if err != nil || found {
		return resource, found, err
	}
	rotated := int16(0)
	switch direction {
	case SetDirectionNorth:
		rotated = SetDirectionEast
	case SetDirectionSouth:
		rotated = SetDirectionWest
	case SetDirectionEast:
		rotated = SetDirectionSouth
	case SetDirectionWest:
		rotated = SetDirectionNorth
	}
	to = from
	to[2] = rotated
	return s.BackgroundResourceForPoints(from, to)
}

func (s *Set) BackgroundTableResource() uint32 {
	if s == nil {
		return 0
	}
	return s.backgroundResource
}

func (s *Set) Palette() []byte {
	if s == nil {
		return nil
	}
	return append([]byte(nil), s.palette...)
}

func (s *Set) SecondaryResource() uint32 {
	if s == nil {
		return 0
	}
	return s.secondaryResource
}

func (s *Set) Resource(index uint32) ([]byte, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("SET is closed")
	}
	return readSetResource(s.cache, index)
}

func (s *Set) Close() error {
	if s == nil || s.cache == nil {
		return nil
	}
	cache := s.cache
	s.cache = nil
	return cache.Close()
}
