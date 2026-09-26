package assets

import (
	"encoding/binary"
	"fmt"
)

type SceneMove uint16

const (
	SceneMoveLeft SceneMove = iota + 1
	SceneMoveRight
	SceneMoveStraight
	SceneMoveBackwards
)

func (s *Set) StartPoint() ([3]int16, error) {
	if s == nil || s.cache == nil {
		return [3]int16{}, fmt.Errorf("SET is closed")
	}
	data, err := readSetResource(s.cache, 0)
	if err != nil {
		return [3]int16{}, err
	}
	if len(data) < 0x36 {
		return [3]int16{}, fmt.Errorf("SET start position is truncated")
	}
	return [3]int16{int16(binary.LittleEndian.Uint16(data[0x30:0x32])), int16(binary.LittleEndian.Uint16(data[0x32:0x34])), int16(binary.LittleEndian.Uint16(data[0x34:0x36]))}, nil
}

func (s *Set) MovePoint(point [3]int16, move SceneMove) ([3]int16, uint32, bool, error) {
	if s == nil || s.cache == nil {
		return point, 0, false, fmt.Errorf("SET is closed")
	}
	next := point
	switch move {
	case SceneMoveLeft:
		switch point[2] {
		case 1:
			next[2] = 4
		case 2:
			next[2] = 3
		case 3:
			next[2] = 1
		case 4:
			next[2] = 2
		default:
			return point, 0, false, fmt.Errorf("SET orientation %d is invalid", point[2])
		}
	case SceneMoveRight:
		switch point[2] {
		case 1:
			next[2] = 3
		case 2:
			next[2] = 4
		case 3:
			next[2] = 2
		case 4:
			next[2] = 1
		default:
			return point, 0, false, fmt.Errorf("SET orientation %d is invalid", point[2])
		}
	case SceneMoveStraight:
		switch point[2] {
		case 1:
			next[1]--
		case 2:
			next[1]++
		case 3:
			next[0]++
		case 4:
			next[0]--
		default:
			return point, 0, false, fmt.Errorf("SET orientation %d is invalid", point[2])
		}
	case SceneMoveBackwards:
		switch point[2] {
		case 1:
			next[1]++
		case 2:
			next[1]--
		case 3:
			next[0]--
		case 4:
			next[0]++
		default:
			return point, 0, false, fmt.Errorf("SET orientation %d is invalid", point[2])
		}
	default:
		return point, 0, false, fmt.Errorf("SET movement %d is invalid", move)
	}
	resource, found, err := s.BackgroundResourceForPoints(point, next)
	return next, resource, found, err
}
