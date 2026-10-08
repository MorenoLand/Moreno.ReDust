package scripts

import (
	"errors"
	"fmt"
)

var ErrUnterminatedBlock = errors.New("script block ended before its closing parenthesis")
var ErrUnsupportedTerminatorPair = errors.New("unsupported structured block token pair")

func ParenthesizedBlockEnd(records []Record, start int) (int, error) {
	end, status, err := ParenthesizedBlockScan(records, start)
	if err != nil {
		return 0, err
	}
	if status != 0 {
		return 0, ErrUnterminatedBlock
	}
	return end, nil
}

func ParenthesizedBlockScan(records []Record, start int) (int, uint16, error) {
	if start < 0 || start >= len(records) {
		return 0, 0, ErrUnterminatedBlock
	}
	depth := 0
	for i := start; i < len(records); i++ {
		switch records[i].Kind {
		case 0, 6:
			return 0, 2, nil
		case 4018:
			depth++
		case 4019:
			depth--
			if depth < 1 {
				return i + 1, 0, nil
			}
		}
	}
	return 0, 0, ErrUnterminatedBlock
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

var ErrCodeNotFound = errors.New("script code name was not found")

func FindCode(program Program, name string) (int, error) {
	for index, record := range program.Records {
		if record.Kind != 4001 {
			continue
		}
		if index+1 >= len(program.Records) {
			return -1, fmt.Errorf("code marker %d has no name record", index)
		}
		identifier, err := program.IdentifierPascal(index + 1)
		if err != nil {
			return -1, fmt.Errorf("code marker %d: %w", index, err)
		}
		if string(identifier[1:]) == name {
			return index, nil
		}
	}
	return -1, ErrCodeNotFound
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
			if depth == 0 && !endIfReturnsAfter {
				return int32(i - start + 1), 0, nil
			}
		}
	}
	return -1, 0, fmt.Errorf("conditional block ended before its sentinel")
}

func FindNestedTerminator(records []Record, start int, open, close uint16) (int32, uint16, error) {
	if !((open == 4009 && close == 4010) || (open == 4012 && close == 4015)) {
		return -1, 0, fmt.Errorf("%w: %d/%d", ErrUnsupportedTerminatorPair, open, close)
	}
	return findNestedTerminator(records, start, open, close, 0x1f)
}

func FindWhileEnd(records []Record, start int) (int32, uint16, error) {
	return findNestedTerminator(records, start, 4016, 4017, 0x25)
}

func findNestedTerminator(records []Record, start int, open, close, endCodeStatus uint16) (int32, uint16, error) {
	if start < 0 || start >= len(records) {
		return -1, 0, fmt.Errorf("structured block start index %d is out of range", start)
	}
	var depth int16
	for i := start; i < len(records); i++ {
		switch records[i].Kind {
		case 0:
			return -1, 0x1b, nil
		case 4004:
			return -1, endCodeStatus, nil
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

func CaseBodyOffset(records []Record, start int) (int32, uint16, error) {
	if start < 0 || start >= len(records) {
		return -1, 0, fmt.Errorf("case-body start index %d is out of range", start)
	}
	program := Program{Records: records}
	current := start
	for {
		if KindAt(program, current) != 6 {
			return -1, 0x1b, nil
		}
		cursor := current
		for KindAt(program, cursor) == 6 {
			cursor++
		}
		if cursor >= len(records) {
			return -1, 0, fmt.Errorf("case-body scan left the record stream")
		}
		if records[cursor].Kind != 4011 {
			return int32(current - start), 0, nil
		}
		for {
			cursor++
			if cursor >= len(records) {
				return -1, 0, fmt.Errorf("case label ended before its line marker")
			}
			if records[cursor].Kind == 0 {
				return -1, 0x1b, nil
			}
			current = cursor
			if records[cursor].Kind == 6 {
				break
			}
		}
	}
}

func FindSwitchCase(program Program, start int, expected Record, state *ExpressionState, parse ExpressionAtomParser, strings *StringRegisters) (int32, uint16, error) {
	if start < 0 || start >= len(program.Records) {
		return -1, 0, fmt.Errorf("switch start index %d is out of range", start)
	}
	var expectedText []byte
	if expected.Kind == 3 {
		if strings == nil {
			return -1, 0, fmt.Errorf("switch string registers are unavailable")
		}
		var status uint16
		var err error
		expectedText, status, err = strings.Load(expected)
		if err != nil || status != 0 {
			return -1, status, err
		}
	}
	var depth int16
	for current := start; current < len(program.Records); current++ {
		switch program.Records[current].Kind {
		case 0:
			return -1, 0x1b, nil
		case 4004:
			return -1, 0x1f, nil
		case 4009:
			depth++
		case 4010:
			if depth == 0 {
				return -1, 0, nil
			}
			depth--
		case 4011:
			if depth != 0 {
				continue
			}
			if state == nil || parse == nil {
				return -1, 0, fmt.Errorf("switch expression runtime is unavailable")
			}
			value, consumed, status, err := state.Evaluate(program, current+1, parse)
			if err != nil || uint16(status) != 0 {
				return -1, uint16(status), err
			}
			if value.Kind != expected.Kind {
				return -1, 14, nil
			}
			matched := false
			switch value.Kind {
			case 4:
				matched = value.Data == expected.Data
			case 3:
				caseText, stringStatus, err := strings.Load(value)
				if err != nil || stringStatus != 0 {
					return -1, stringStatus, err
				}
				matched = equalPascalString(caseText, expectedText)
			}
			afterExpression := current + 1 + int(consumed)
			if matched {
				bodyOffset, bodyStatus, err := CaseBodyOffset(program.Records, afterExpression)
				if err != nil || bodyStatus != 0 {
					return -1, bodyStatus, err
				}
				return int32(afterExpression-start) + bodyOffset, 0, nil
			}
			current = afterExpression
		}
	}
	return -1, 0, fmt.Errorf("switch block ended before its sentinel")
}
