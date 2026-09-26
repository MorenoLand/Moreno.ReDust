package scripts

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type StatementDispatcher func(start int) (uint16, error)

type ConditionCodeSession struct {
	Frame      ConditionFrame
	Runtime    *ConditionRuntime
	Dispatch   StatementDispatcher
	CloseFrame func()
}

var ErrNoStatementDispatcher = errors.New("statement dispatcher is unavailable")

func (s *ConditionCodeSession) EvaluateExpression(start int) (int32, uint16, error) {
	if s == nil {
		return -1, 0, fmt.Errorf("condition code session is nil")
	}
	frame := s.Frame
	frame.Marker = start
	return EvaluateCondition(frame, s.Runtime)
}

func (s *ConditionCodeSession) DispatchStatement(start int) (uint16, error) {
	if s == nil || s.Dispatch == nil {
		return 0, ErrNoStatementDispatcher
	}
	return s.Dispatch(start)
}

func (s *ConditionCodeSession) Close() {
	if s != nil && s.CloseFrame != nil {
		s.CloseFrame()
	}
}

type ConditionRuntime struct {
	Expressions *ExpressionState
	Strings     *StringRegisters
	Global      *VariableTable
	Services    ExpressionAtomServices
}

func (r *ConditionRuntime) ResolveVariable(scope any, name []byte, recordIndex int, cache *Record) (uint16, uint32, error) {
	table, ok := scope.(*VariableTable)
	if !ok || table == nil {
		return 0, 9, nil
	}
	id, status, err := table.ResolveOrCreate(name, cache)
	return id, uint32(status), err
}

func (r *ConditionRuntime) EvaluateValue(context any, program Program, recordIndex int, scope any) (ExpressionValue, uint32, uint32, error) {
	if r == nil || r.Expressions == nil {
		return ExpressionValue{}, 0, 0, fmt.Errorf("expression state is unavailable")
	}
	var local *VariableTable
	if scope != nil {
		var ok bool
		local, ok = scope.(*VariableTable)
		if !ok {
			return ExpressionValue{}, 0, 0, fmt.Errorf("expression variable scope has type %T", scope)
		}
	}
	strings := r.Strings
	if strings == nil {
		strings = r.Expressions.Strings
	}
	previousStrings := r.Expressions.Strings
	if strings != nil {
		r.Expressions.Strings = strings
	}
	defer func() {
		r.Expressions.Strings = previousStrings
	}()
	parser := ExpressionValueParser{Context: context, Local: local, Global: r.Global, State: r.Expressions, Strings: strings, Services: r.Services}
	value, consumed, status, err := r.Expressions.Evaluate(program, recordIndex, parser.Parse)
	if err != nil {
		return ExpressionValue{}, consumed, status, err
	}
	var result ExpressionValue
	binary.LittleEndian.PutUint16(result[:2], value.Kind)
	binary.LittleEndian.PutUint32(result[2:6], value.Data)
	binary.LittleEndian.PutUint16(result[6:8], value.Tail)
	return result, consumed, status, nil
}

func (r *ConditionRuntime) AssignVariable(scope any, index uint16, value ExpressionValue) (uint32, error) {
	if r == nil {
		return 9, nil
	}
	table, ok := scope.(*VariableTable)
	if !ok || table == nil {
		return 9, nil
	}
	strings := r.Strings
	if strings == nil && r.Expressions != nil {
		strings = r.Expressions.Strings
	}
	status, err := table.WriteValue(index, value, strings)
	return uint32(status), err
}
