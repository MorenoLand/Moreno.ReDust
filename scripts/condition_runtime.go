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
	if s == nil {
		return 0, ErrNoStatementDispatcher
	}
	if start >= 0 && start < len(s.Frame.Program.Records) && s.Frame.Program.Records[start].Kind == LookupOpcode("path") {
		return s.Runtime.DispatchPathStatement(s.Frame.Context, s.Frame.Program, start, s.Frame.VariableScope)
	}
	if start >= 0 && start < len(s.Frame.Program.Records) && s.Frame.Program.Records[start].Kind == LookupOpcode("wavevolume") {
		return s.Runtime.DispatchWaveVolumeStatement(s.Frame.Context, s.Frame.Program, start, s.Frame.VariableScope)
	}
	if s.Dispatch == nil {
		return 0, ErrNoStatementDispatcher
	}
	return s.Dispatch(start)
}

func (s *ConditionCodeSession) DispatchVariableDeclaration(start int) (int32, uint16, error) {
	if s == nil || s.Runtime == nil {
		return -1, 0, ErrNoVariableDeclarationDispatcher
	}
	if start < 0 || start >= len(s.Frame.Program.Records) {
		return -1, 0, fmt.Errorf("variable declaration index %d is out of range", start)
	}
	local, ok := s.Frame.VariableScope.(*VariableTable)
	if s.Frame.VariableScope != nil && !ok {
		return -1, 0, fmt.Errorf("variable declaration scope has type %T", s.Frame.VariableScope)
	}
	if s.Frame.Program.Records[start].Kind == 4002 && s.Runtime.Global == nil {
		return -1, 0, fmt.Errorf("global variable table is unavailable")
	}
	if s.Frame.Program.Records[start].Kind == 4003 && local == nil {
		return -1, 0, fmt.Errorf("local variable table is unavailable")
	}
	return ResolveVariableDeclaration(&s.Frame.Program, start, s.Runtime.Global, local)
}

func (s *ConditionCodeSession) StatementInterrupt() (uint16, error) {
	if s == nil || s.Runtime == nil {
		return 0, ErrEscapeInterruptInputUnavailable
	}
	return s.Runtime.StatementInterrupt()
}

func (s *ConditionCodeSession) Close() {
	if s != nil && s.CloseFrame != nil {
		s.CloseFrame()
	}
}

type ConditionRuntime struct {
	Expressions            *ExpressionState
	Strings                *StringRegisters
	ContextPages           *ScriptContextPages
	Global                 *VariableTable
	EscapeInterruptEnabled uint16
	EscapeKeyPressed       func() bool
	Services               ExpressionAtomServices
}

func (r *ConditionRuntime) SetEscapeInterruptResult(value ExpressionValue) (uint16, error) {
	if r == nil {
		return 0, fmt.Errorf("condition runtime is unavailable")
	}
	if binary.LittleEndian.Uint16(value[:2]) != 2 {
		return 0x0e, nil
	}
	r.EscapeInterruptEnabled = uint16(binary.LittleEndian.Uint32(value[2:6]))
	return 0, nil
}

func (r *ConditionRuntime) EscapeInterruptValue() ExpressionValue {
	var value ExpressionValue
	if r != nil {
		binary.LittleEndian.PutUint16(value[:2], 2)
		binary.LittleEndian.PutUint32(value[2:6], uint32(r.EscapeInterruptEnabled))
	}
	return value
}

func (r *ConditionRuntime) ResetEscapeInterrupt() {
	if r != nil {
		r.EscapeInterruptEnabled = 0
	}
}

func (r *ConditionRuntime) StatementInterrupt() (uint16, error) {
	if r == nil || r.EscapeInterruptEnabled == 0 {
		return 0, nil
	}
	if r.EscapeKeyPressed == nil {
		return 0, ErrEscapeInterruptInputUnavailable
	}
	if r.EscapeKeyPressed() {
		return 0x35, nil
	}
	return 0, nil
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
	parser := ExpressionValueParser{Context: context, Local: local, Global: r.Global, State: r.Expressions, Strings: strings, ContextPages: r.ContextPages, Services: r.Services}
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

func (r *ConditionRuntime) DispatchPathStatement(context any, program Program, start int, scope any) (uint16, error) {
	if r == nil || start < 0 || start >= len(program.Records) || program.Records[start].Kind != LookupOpcode("path") {
		return 0, fmt.Errorf("path statement index %d is invalid", start)
	}
	if kindAt(program, start+1) != LookupOpcode("(") {
		return 2, nil
	}
	index, indexConsumed, status, err := r.EvaluateValue(context, program, start+2, scope)
	if err != nil || status != 0 {
		return uint16(status), err
	}
	comma := start + 2 + int(indexConsumed)
	if kindAt(program, comma) != LookupOpcode(",") {
		return 0x1c, nil
	}
	valueStart := comma + 1
	value, valueConsumed, status, err := r.EvaluateValue(context, program, valueStart, scope)
	if err != nil || status != 0 {
		return uint16(status), err
	}
	toRecord := func(value ExpressionValue) Record {
		return Record{Kind: binary.LittleEndian.Uint16(value[:2]), Data: binary.LittleEndian.Uint32(value[2:6]), Tail: binary.LittleEndian.Uint16(value[6:8])}
	}
	status16, err := r.SetPathValue(toRecord(index), toRecord(value))
	if err != nil || status16 != 0 {
		return status16, err
	}
	if kindAt(program, valueStart+int(valueConsumed)) != LookupOpcode(")") {
		return 2, nil
	}
	return 0, nil
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

func (r *ConditionRuntime) SetPathValue(index, value Record) (uint16, error) {
	if index.Kind != 4 {
		return 14, nil
	}
	contextIndex := int(int32(index.Data))
	if contextIndex <= 0 || contextIndex >= nativeScriptContextCount {
		return 10, nil
	}
	if r == nil || r.ContextPages == nil {
		return 0, fmt.Errorf("native script context pages are unavailable")
	}
	strings := r.Strings
	if strings == nil && r.Expressions != nil {
		strings = r.Expressions.Strings
	}
	if strings == nil {
		return 0, fmt.Errorf("expression string registers are unavailable")
	}
	pascal, status, err := strings.Load(value)
	if err != nil || status != 0 {
		return status, err
	}
	if err := r.ContextPages.Store(contextIndex, pascal); err != nil {
		return 0, err
	}
	return 0, nil
}
