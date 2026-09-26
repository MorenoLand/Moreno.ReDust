package scripts

import (
	"errors"
	"fmt"
)

var ErrUnterminatedBlock = errors.New("script block ended before its closing parenthesis")

func ParenthesizedBlockEnd(records []Record, start int) (int, error) {
	if start < 0 || start >= len(records) {
		return 0, ErrUnterminatedBlock
	}
	depth := 0
	for i := start; i < len(records); i++ {
		switch records[i].Kind {
		case 4018:
			depth++
		case 4019:
			depth--
			if depth < 1 {
				return i + 1, nil
			}
		case 0, 6:
			return 0, ErrUnterminatedBlock
		}
	}
	return 0, ErrUnterminatedBlock
}

func FindCodeMarker(records []Record) (int, bool) {
	for i, record := range records {
		if record.Kind == 4001 {
			return i, true
		}
		if record.Kind == 0 {
			return 0, false
		}
	}
	return 0, false
}

var ErrInvalidCodeMarker = errors.New("record is not a code marker")

func NextCodeOffset(records []Record, start int) (int32, error) {
	if start < 0 || start >= len(records) || records[start].Kind != 4001 {
		return 0, ErrInvalidCodeMarker
	}
	if records[start].Data != 0 {
		return int32(records[start].Data), nil
	}
	for i := start + 1; i < len(records); i++ {
		if records[i].Kind == 0 {
			records[start].Data = ^uint32(0)
			return -1, nil
		}
		if records[i].Kind == 4001 {
			records[start].Data = uint32(i - start)
			return int32(i - start), nil
		}
	}
	records[start].Data = ^uint32(0)
	return -1, nil
}

func FindConditionalBranch(records []Record, start int) (int32, uint16, error) {
	return scanConditional(records, start, false)
}

func FindConditionalEnd(records []Record, start int) (int32, uint16, error) {
	return scanConditional(records, start, true)
}

func scanConditional(records []Record, start int, endIfReturnsAfter bool) (int32, uint16, error) {
	if start < 0 || start >= len(records) {
		return -1, 0, fmt.Errorf("conditional start index %d is out of range", start)
	}
	var depth int16
	for i := start; i < len(records); i++ {
		switch records[i].Kind {
		case 0:
			return -1, 0x1b, nil
		case 4004:
			return -1, 0x1d, nil
		case 4006:
			depth++
		case 4007:
			if depth == 0 {
				if endIfReturnsAfter {
					return int32(i - start + 1), 0, nil
				}
				return -1, 0, nil
			}
			depth--
		case 4008:
			if depth == 0 {
				return int32(i - start + 1), 0, nil
			}
		}
	}
	return -1, 0, fmt.Errorf("conditional block ended before its sentinel")
}

func FindNestedTerminator(records []Record, start int, open, close uint16) (int32, uint16, error) {
	if start < 0 || start >= len(records) {
		return -1, 0, fmt.Errorf("structured block start index %d is out of range", start)
	}
	var depth int16
	for i := start; i < len(records); i++ {
		switch records[i].Kind {
		case 0:
			return -1, 0x1b, nil
		case 4004:
			return -1, 0x1f, nil
		case open:
			depth++
		case close:
			if depth == 0 {
				return int32(i - start + 1), 0, nil
			}
			depth--
		}
	}
	return -1, 0, fmt.Errorf("structured block ended before its sentinel")
}

func SkipFalseConditionalBlock(records []Record, start int) (int32, uint16, error) {
	offset, status, err := FindConditionalBranch(records, start)
	if err != nil || status != 0 || offset >= 0 {
		return offset, status, err
	}
	return FindConditionalEnd(records, start)
}
