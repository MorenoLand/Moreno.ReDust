package scripts

import "fmt"

type ExpressionValue [32]byte

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
	ResolveVariable(scope any, identifier []byte, recordIndex int) (uint16, uint32)
	EvaluateValue(context any, program Program, recordIndex int, scope any) (ExpressionValue, uint32, uint32)
	AssignVariable(scope any, index uint16, value ExpressionValue) uint32
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
		index, operationStatus := operations.ResolveVariable(frame.VariableScope, name, variableRecord)
		if uint16(operationStatus) != 0 {
			return -1, uint16(operationStatus), nil
		}
		if valueIndex < 0 || valueIndex >= len(frame.Arguments.Records) {
			return -1, 0, fmt.Errorf("expression record index %d is out of range", valueIndex)
		}
		frame.Program.Records[variableRecord].Tail = index
		value, consumed, operationStatus := operations.EvaluateValue(frame.Context, frame.Arguments, valueIndex, frame.ValueScope)
		if uint16(operationStatus) != 0 {
			return -1, uint16(operationStatus), nil
		}
		if uint64(valueIndex)+uint64(consumed) > uint64(len(frame.Arguments.Records)) {
			return -1, 0, fmt.Errorf("expression record offset %d exceeds the record stream", consumed)
		}
		valueIndex += int(consumed)
		operationStatus = operations.AssignVariable(frame.VariableScope, index, value)
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
