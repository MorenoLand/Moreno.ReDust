package assets

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// Path is a walkable polyline between two named stars. The SET's coordinate
// table (the secondary resource) doubles as the path list: a row whose u32 at
// +0x18 is non-zero names a path resource, with the row's two names as its
// ends (FUN_0041BA70, FUN_0041BBD0). The resource holds a count, a total
// length, a bounding rectangle and then one 8-byte point per vertex: x, y, z
// and the length of the segment from the previous vertex (FUN_00411620).
type Path struct {
	Points [][3]int16
	// Segment[i] is the length from Points[i-1] to Points[i]; Segment[0] is 0.
	Segment []int
	Total   int
}

const pathHeaderSize = 16

func (w *Set) readPath(resource uint32) (*Path, error) {
	data, err := readSetResource(w.cache, resource)
	if err != nil {
		return nil, err
	}
	if len(data) < pathHeaderSize {
		return nil, fmt.Errorf("path resource %d is truncated", resource)
	}
	count := int(binary.LittleEndian.Uint32(data[0:4]))
	if count < 1 || pathHeaderSize+count*8 > len(data) {
		return nil, fmt.Errorf("path resource %d has %d points in %d bytes", resource, count, len(data))
	}
	path := &Path{Total: int(binary.LittleEndian.Uint32(data[4:8]))}
	for index := 0; index < count; index++ {
		row := data[pathHeaderSize+index*8 : pathHeaderSize+index*8+8]
		path.Points = append(path.Points, [3]int16{int16(binary.LittleEndian.Uint16(row[0:2])), int16(binary.LittleEndian.Uint16(row[2:4])), int16(binary.LittleEndian.Uint16(row[4:6]))})
		path.Segment = append(path.Segment, int(binary.LittleEndian.Uint16(row[6:8])))
	}
	return path, nil
}

// Reverse is FUN_0041BEE0: the vertices in the opposite order, each segment
// length staying with the pair of vertices it joins.
func (p *Path) Reverse() {
	count := len(p.Points)
	points := make([][3]int16, count)
	segment := make([]int, count)
	for index := 0; index < count; index++ {
		points[index] = p.Points[count-1-index]
		if index > 0 {
			segment[index] = p.Segment[count-index]
		}
	}
	p.Points, p.Segment = points, segment
}

// pathRows calls visit for each path row of the coordinate table.
func (s *Set) pathRows(visit func(first, second string, resource uint32) (bool, error)) error {
	if s == nil || s.cache == nil {
		return fmt.Errorf("SET is closed")
	}
	data, err := readSetResource(s.cache, s.secondaryResource)
	if err != nil {
		return fmt.Errorf("read SET coordinate table: %w", err)
	}
	if len(data) < setCoordinateHeaderSize {
		return fmt.Errorf("SET coordinate table header is truncated")
	}
	count := uint64(binary.LittleEndian.Uint32(data[0x18:0x1c]))
	if count > uint64((len(data)-setCoordinateHeaderSize)/setCoordinateRowSize) {
		return fmt.Errorf("SET coordinate count %d exceeds its table", count)
	}
	for index := uint64(0); index < count; index++ {
		row := data[setCoordinateHeaderSize+int(index)*setCoordinateRowSize : setCoordinateHeaderSize+int(index+1)*setCoordinateRowSize]
		resource := binary.LittleEndian.Uint32(row[0x18:0x1c])
		if resource == 0 {
			continue
		}
		first, err := castPascalAt(row, 8)
		if err != nil {
			return err
		}
		second, err := castPascalAt(row, 0x22)
		if err != nil {
			return err
		}
		if stop, err := visit(first, second, resource); err != nil || stop {
			return err
		}
	}
	return nil
}

// FindPath is FUN_0041BA70's table scan: the path joining the actor's star
// to the destination, reversed when the actor stands at the row's second end.
func (s *Set) FindPath(destination, actorStar string) (*Path, bool, error) {
	var found *Path
	err := s.pathRows(func(first, second string, resource uint32) (bool, error) {
		forward := strings.EqualFold(destination, second) && strings.EqualFold(actorStar, first)
		backward := strings.EqualFold(destination, first) && strings.EqualFold(actorStar, second)
		if !forward && !backward {
			return false, nil
		}
		path, err := s.readPath(resource)
		if err != nil {
			return true, err
		}
		if backward {
			path.Reverse()
		}
		found = path
		return true, nil
	})
	return found, found != nil, err
}

// FindPathTo is FUN_0041BBD0, used when the actor's star is "resume": any
// path ending at the destination, joined at the vertex nearest the actor.
func (s *Set) FindPathTo(destination string, position [3]int16) (*Path, bool, error) {
	var found *Path
	err := s.pathRows(func(first, second string, resource uint32) (bool, error) {
		forward := strings.EqualFold(destination, second)
		backward := !forward && strings.EqualFold(destination, first)
		if !forward && !backward {
			return false, nil
		}
		path, err := s.readPath(resource)
		if err != nil {
			return true, err
		}
		if backward {
			path.Reverse()
		}
		path.JoinAt(position)
		found = path
		return true, nil
	})
	return found, found != nil, err
}

// JoinAt is FUN_0041BCC0: the vertex nearest the position is replaced by the
// position, earlier vertices are dropped, and the segment lengths and total
// are rebuilt from the remaining vertices.
func (p *Path) JoinAt(position [3]int16) {
	best, bestDistance := -1, int64(1)<<62
	for index, point := range p.Points {
		dx, dy, dz := int64(position[0])-int64(point[0]), int64(position[1])-int64(point[1]), int64(position[2])-int64(point[2])
		if distance := dx*dx + dy*dy + dz*dz; distance < bestDistance {
			best, bestDistance = index, distance
		}
	}
	if best < 0 {
		return
	}
	if len(p.Points)-best == 1 {
		best = len(p.Points) - 2
	}
	if best < 0 {
		return
	}
	p.Points[best] = position
	p.Points = append([][3]int16(nil), p.Points[best:]...)
	p.Segment = make([]int, len(p.Points))
	p.Total = 0
	for index := 1; index < len(p.Points); index++ {
		dx, dy := int64(p.Points[index][0])-int64(p.Points[index-1][0]), int64(p.Points[index][1])-int64(p.Points[index-1][1])
		p.Segment[index] = int(isqrt(uint64(dx*dx + dy*dy)))
		p.Total += p.Segment[index]
	}
}

func isqrt(value uint64) uint64 {
	result, bit := uint64(0), uint64(1)<<62
	for bit > value {
		bit >>= 2
	}
	for bit != 0 {
		if value >= result+bit {
			value -= result + bit
			result = (result >> 1) + bit
		} else {
			result >>= 1
		}
		bit >>= 2
	}
	return result
}
