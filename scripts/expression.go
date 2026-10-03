package scripts

import "fmt"

type ExpressionValue [8]byte

type ConditionFrame struct {
	Program       Program
	Marker        int
	Arguments     Program
	ArgumentStart int
	VariableScope any
	Context       any
	ValueScope    any
}

type ConditionOperations interface {
	ResolveVariable(scope any, identifier []byte, recordIndex int, cache *Record) (uint16, uint32, error)
	EvaluateValue(context any, program Program, recordIndex int, scope any) (ExpressionValue, uint32, uint32, error)
	AssignVariable(scope any, index uint16, value ExpressionValue) (uint32, error)
}

func EvaluateCondition(frame ConditionFrame, operations ConditionOperations) (int32, uint16, error) {
	marker := frame.Marker
	if marker < 0 || marker >= len(frame.Program.Records) {
		return -1, 0, fmt.Errorf("code marker index %d is out of range", marker)
	}
	left, status, err := identifierAt(frame.Program, marker+1)
	if err != nil || status != 0 {
		return -1, status, err
	}
	right, status, err := identifierAt(frame.Arguments, frame.ArgumentStart)
	if err != nil || status != 0 {
		return -1, status, err
	}
	if equalPascalString(left, right) {
		return -1, 0, nil
	}
	if kindAt(frame.Program, marker+2) != 4018 || kindAt(frame.Arguments, frame.ArgumentStart+1) != 4018 {
		return -1, 2, nil
	}
	nameIndex := marker + 3
	valueIndex := frame.ArgumentStart + 2
	if kindAt(frame.Program, nameIndex) == 4019 {
		bodyIndex, err := nextNonLineRecord(frame.Program, nameIndex+1)
		if err != nil {
			return -1, 0, err
		}
		if kindAt(frame.Arguments, valueIndex) != 4019 {
			return -1, 2, nil
		}
		return int32(bodyIndex - marker), 0, nil
	}
	if operations == nil {
		return -1, 0, ErrNoConditionOperations
	}
	for {
		name, status, err := identifierAt(frame.Program, nameIndex)
		if err != nil || status != 0 {
			return -1, status, err
		}
		variableRecord := nameIndex + 1
		if variableRecord < 0 || variableRecord >= len(frame.Program.Records) {
			return -1, 0, fmt.Errorf("variable record index %d is out of range", variableRecord)
		}
		index, operationStatus, err := operations.ResolveVariable(frame.VariableScope, name, variableRecord, &frame.Program.Records[variableRecord])
		if err != nil {
			return -1, 0, err
		}
		if uint16(operationStatus) != 0 {
			return -1, uint16(operationStatus), nil
		}
		if valueIndex < 0 || valueIndex >= len(frame.Arguments.Records) {
			return -1, 0, fmt.Errorf("expression record index %d is out of range", valueIndex)
		}
		value, consumed, operationStatus, err := operations.EvaluateValue(frame.Context, frame.Arguments, valueIndex, frame.ValueScope)
		if err != nil {
			return -1, 0, err
		}
		if uint16(operationStatus) != 0 {
			return -1, uint16(operationStatus), nil
		}
		if uint64(valueIndex)+uint64(consumed) > uint64(len(frame.Arguments.Records)) {
			return -1, 0, fmt.Errorf("expression record offset %d exceeds the record stream", consumed)
		}
		valueIndex += int(consumed)
		operationStatus, err = operations.AssignVariable(frame.VariableScope, index, value)
		if err != nil {
			return -1, 0, err
		}
		if uint16(operationStatus) != 0 {
			return -1, uint16(operationStatus), nil
		}
		switch kindAt(frame.Program, variableRecord) {
		case 4020:
			nameIndex += 2
			if kindAt(frame.Arguments, valueIndex) != 4020 {
				return -1, 0x1c, nil
			}
			valueIndex++
		case 4019:
			bodyIndex, err := nextNonLineRecord(frame.Program, variableRecord+1)
			if err != nil {
				return -1, 0, err
			}
			if kindAt(frame.Arguments, valueIndex) != 4019 {
				return -1, 2, nil
			}
			return int32(bodyIndex - marker), 0, nil
		default:
			return -1, 2, nil
		}
	}
}

var ErrNoConditionOperations = fmt.Errorf("condition operation handlers are unavailable")

func identifierAt(program Program, index int) ([]byte, uint16, error) {
	if index < 0 || index >= len(program.Records) {
		return nil, 0, fmt.Errorf("identifier record index %d is out of range", index)
	}
	if program.Records[index].Kind != 5 {
		return nil, 14, nil
	}
	identifier, err := program.IdentifierPascal(index)
	return identifier, 0, err
}

func equalPascalString(left, right []byte) bool {
	if len(left) == 0 || len(right) == 0 || left[0] != right[0] || len(left) != int(left[0])+1 || len(right) != int(right[0])+1 {
		return false
	}
	for i := 1; i < len(left); i++ {
		if lowerASCII(left[i]) != lowerASCII(right[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(value byte) byte {
	if value >= 'A' && value <= 'Z' {
		return value + ('a' - 'A')
	}
	return value
}

func kindAt(program Program, index int) uint16 {
	if index < 0 || index >= len(program.Records) {
		return 0
	}
	return program.Records[index].Kind
}

func nextNonLineRecord(program Program, index int) (int, error) {
	for kindAt(program, index) == 6 {
		index++
	}
	if index < 0 || index >= len(program.Records) {
		return 0, fmt.Errorf("record index %d is out of range", index)
	}
	return index, nil
}

func ExpressionPrecedence(opcode uint16) (uint8, bool) {
	switch opcode {
	case 8001, 8002:
		return 1, true
	case 8003, 8004:
		return 0, true
	case 8005:
		return 5, true
	case 8006:
		return 6, true
	case 8007:
		return 2, true
	case 8008, 8009:
		return 4, true
	case 8010, 8011, 8012, 8013:
		return 3, true
	default:
		return 0, false
	}
}

func ApplyBinaryOperator(left, right Record, opcode uint16, strings *StringRegisters) (Record, uint16, error) {
	result := Record{Kind: left.Kind, Tail: left.Tail}
	switch opcode {
	case 8001, 8002, 8003, 8004:
		if left.Kind != 4 || right.Kind != 4 {
			return Record{}, 14, nil
		}
		switch opcode {
		case 8001:
			result.Data = left.Data + right.Data
		case 8002:
			result.Data = left.Data - right.Data
		case 8003:
			result.Data = left.Data * right.Data
		case 8004:
			if right.Data == 0 {
				return Record{}, 0x37, nil
			}
			// IDIV at 0x004223FD: signed division.
			result.Data = uint32(int32(left.Data) / int32(right.Data))
		}
	case 8005, 8006:
		if left.Kind != 2 || right.Kind != 2 {
			return Record{}, 14, nil
		}
		if opcode == 8005 {
			result.Data = left.Data & right.Data
		} else {
			result.Data = left.Data | right.Data
		}
	case 8007:
		if strings == nil {
			return Record{}, 0, fmt.Errorf("expression string registers are unavailable")
		}
		leftText, status, err := strings.Load(left)
		if err != nil || status != 0 {
			return Record{}, status, err
		}
		rightText, status, err := strings.Load(right)
		if err != nil || status != 0 {
			return Record{}, status, err
		}
		length := int(leftText[0]) + int(rightText[0])
		if length > 0xff {
			return Record{}, 0x1a, nil
		}
		text := make([]byte, length+1)
		text[0] = byte(length)
		copy(text[1:], leftText[1:])
		copy(text[1+int(leftText[0]):], rightText[1:])
		return strings.Store(text)
	case 8008, 8009:
		var equal bool
		if left.Kind != right.Kind {
			// Mismatched types compare unequal and still yield a boolean
			// (0x004224FD and 0x004225BF write type 2 in both cases).
			result.Kind = 2
			result.Data = 0
			if opcode == 8009 {
				result.Data = 1
			}
			// The native expression stack holds strings in place; the port's
			// registers must be released here or a mismatched comparison
			// leaks one.
			for _, operand := range [...]Record{left, right} {
				if operand.Kind == 3 && strings != nil {
					if _, status, err := strings.Load(operand); err != nil || status != 0 {
						return Record{}, status, err
					}
				}
			}
			return result, 0, nil
		}
		switch left.Kind {
		case 2, 4:
			equal = left.Data == right.Data
		case 3:
			if strings == nil {
				return Record{}, 0, fmt.Errorf("expression string registers are unavailable")
			}
			leftText, status, err := strings.Load(left)
			if err != nil || status != 0 {
				return Record{}, status, err
			}
			rightText, status, err := strings.Load(right)
			if err != nil || status != 0 {
				return Record{}, status, err
			}
			equal = equalPascalString(leftText, rightText)
		default:
			return Record{}, 14, nil
		}
		result.Kind = 2
		if opcode == 8008 && equal || opcode == 8009 && !equal {
			result.Data = 1
		}
	case 8010, 8011, 8012, 8013:
		if left.Kind != 4 || right.Kind != 4 {
			return Record{}, 14, nil
		}
		result.Kind = 2
		// JL/JG/JLE/JGE at 0x004226A7..0x0042278B: signed comparisons.
		l, r := int32(left.Data), int32(right.Data)
		switch opcode {
		case 8010:
			result.Data = boolWord(l > r)
		case 8011:
			result.Data = boolWord(l < r)
		case 8012:
			result.Data = boolWord(l >= r)
		case 8013:
			result.Data = boolWord(l <= r)
		}
	default:
		return Record{}, 14, nil
	}
	return result, 0, nil
}

func boolWord(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

type ExpressionAtomParser func(program Program, start int) (Record, uint32, uint32, error)

type ExpressionState struct {
	Stack             [40]Record
	Top               int
	Strings           *StringRegisters
	AvailableBytes    func() int32
	SetProgramCounter func(int)
}

func ResetExpressionRuntime(state *ExpressionState, strings *StringRegisters) {
	if state != nil {
		state.Top = 0
	}
	if strings != nil {
		strings.Reset()
	}
}

func (s *ExpressionState) Evaluate(program Program, start int, parseAtom ExpressionAtomParser) (Record, uint32, uint32, error) {
	if s == nil || s.AvailableBytes == nil || s.SetProgramCounter == nil || parseAtom == nil {
		return Record{}, 0, 0, fmt.Errorf("expression runtime dependencies are unavailable")
	}
	if s.AvailableBytes() < 0x800 {
		return Record{}, 0, 0x2c, nil
	}
	if start < 0 || start >= len(program.Records) || s.Top < 0 || s.Top >= len(s.Stack) {
		return Record{}, 0, 0, fmt.Errorf("expression start or stack index is out of range")
	}
	base := s.Top
	s.SetProgramCounter(start)
	value, consumed, status, err := parseAtom(program, start)
	if err != nil || uint16(status) != 0 {
		return Record{}, 0, status, err
	}
	if consumed == 0 {
		return Record{}, 0, 0, fmt.Errorf("expression atom consumed no records")
	}
	if stackStatus := s.push(value); stackStatus != 0 {
		return Record{}, 0, uint32(stackStatus), nil
	}
	operatorStart := s.Top
	position := start + int(consumed)
	for {
		if position < 0 || position >= len(program.Records) {
			return Record{}, 0, 0, fmt.Errorf("expression record index %d is out of range", position)
		}
		if program.Records[position].Kind < 8000 || program.Records[position].Kind > 8014 {
			break
		}
		if stackStatus := s.push(program.Records[position]); stackStatus != 0 {
			return Record{}, 0, uint32(stackStatus), nil
		}
		s.SetProgramCounter(position)
		if position+1 >= len(program.Records) {
			return Record{}, 0, 0, fmt.Errorf("operand record index %d is out of range", position+1)
		}
		value, consumed, status, err = parseAtom(program, position+1)
		if err != nil || uint16(status) != 0 {
			return Record{}, 0, status, err
		}
		if consumed == 0 {
			return Record{}, 0, 0, fmt.Errorf("expression atom consumed no records")
		}
		if stackStatus := s.push(value); stackStatus != 0 {
			return Record{}, 0, uint32(stackStatus), nil
		}
		position += int(consumed) + 1
	}
	for precedence := uint8(0); precedence < 7; precedence++ {
		for operator := operatorStart; operator < s.Top; operator += 2 {
			level, ok := ExpressionPrecedence(s.Stack[operator].Kind)
			if !ok {
				return Record{}, 0, 0xffff, nil
			}
			if level != precedence {
				continue
			}
			if operator <= base || operator+1 >= s.Top {
				return Record{}, 0, 0, fmt.Errorf("operator at stack index %d has no operand pair", operator)
			}
			value, operationStatus, err := ApplyBinaryOperator(s.Stack[operator-1], s.Stack[operator+1], s.Stack[operator].Kind, s.Strings)
			if err != nil || operationStatus != 0 {
				return Record{}, 0, uint32(operationStatus), err
			}
			s.Stack[operator-1] = value
			copy(s.Stack[operator:s.Top-2], s.Stack[operator+2:s.Top])
			s.Top -= 2
			operator -= 2
		}
	}
	if base >= s.Top {
		return Record{}, 0, 0, fmt.Errorf("expression stack lost its result")
	}
	result := s.Stack[base]
	s.Top--
	return result, uint32(position - start), 0, nil
}

func (s *ExpressionState) push(value Record) uint16 {
	if s.Top < 0 || s.Top >= len(s.Stack) {
		return 3
	}
	s.Stack[s.Top] = value
	s.Top++
	if s.Top > 0x27 {
		return 3
	}
	return 0
}
