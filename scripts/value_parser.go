package scripts

import "fmt"

type ExpressionAtomHook func(context any, program Program, start int, local, global *VariableTable, strings *StringRegisters) (Record, uint32, uint32, error)

type ExpressionAtomServices struct {
	ReadContextPascal func(context any, offset int) ([]byte, error)
	CallBlock         ExpressionAtomHook
	DispatchValue     ExpressionAtomHook
	GetWaveVolume     func() int
	SetWaveVolume     func(int) error
}

type ExpressionValueParser struct {
	Context      any
	Local        *VariableTable
	Global       *VariableTable
	State        *ExpressionState
	Strings      *StringRegisters
	ContextPages *ScriptContextPages
	Services     ExpressionAtomServices
}

func (p *ExpressionValueParser) Parse(program Program, start int) (Record, uint32, uint32, error) {
	if p == nil || start < 0 || start >= len(program.Records) {
		return Record{}, 0, 0, fmt.Errorf("expression value index %d is out of range", start)
	}
	kind := program.Records[start].Kind
	if kind == LookupOpcode("wavevolume") {
		if p.Services.GetWaveVolume == nil {
			return Record{}, 0, 0, fmt.Errorf("wave volume getter is unavailable")
		}
		if kindAt(program, start+1) != LookupOpcode("(") || kindAt(program, start+2) != LookupOpcode(")") {
			return Record{}, 0, 2, nil
		}
		level := p.Services.GetWaveVolume()
		if level < 0 || level > 9 {
			return Record{}, 0, 10, nil
		}
		return Record{Kind: 4, Data: uint32(level)}, 3, 0, nil
	}
	if kind == LookupOpcode("path") {
		return p.parsePathValue(program, start)
	}
	switch kind {
	case 4018:
		if p.State == nil {
			return Record{}, 0, 0, fmt.Errorf("nested expression state is unavailable")
		}
		value, consumed, status, err := p.State.Evaluate(program, start+1, p.Parse)
		if err != nil || uint16(status) != 0 {
			return Record{}, 0, status, err
		}
		closing := start + 1 + int(consumed)
		if kindAt(program, closing) != 4019 {
			return Record{}, 0, 2, nil
		}
		return value, consumed + 2, 0, nil
	case 3:
		if p.Strings == nil {
			return Record{}, 0, 0, fmt.Errorf("expression string registers are unavailable")
		}
		pascal, err := program.LiteralPascal(start)
		if err != nil {
			return Record{}, 0, 0, err
		}
		value, status, err := p.Strings.Store(pascal)
		return value, 1, uint32(status), err
	case 4:
		return Record{Kind: 4, Data: program.Records[start].Data}, 1, 0, nil
	case 8002:
		value, consumed, status, err := p.Parse(program, start+1)
		if err != nil || uint16(status) != 0 {
			return Record{}, 0, status, err
		}
		if value.Kind != 4 {
			return Record{}, 0, 14, nil
		}
		value.Data = uint32(-int32(value.Data))
		return value, consumed + 1, 0, nil
	case 4021:
		return Record{Kind: 2, Data: 1}, 1, 0, nil
	case 4022:
		return Record{Kind: 2}, 1, 0, nil
	case 8007:
		value, consumed, status, err := p.Parse(program, start+1)
		if err != nil || uint16(status) != 0 {
			return Record{}, 0, status, err
		}
		if value.Kind != 2 {
			return Record{}, 0, 14, nil
		}
		value.Data ^= 1
		return value, consumed + 1, 0, nil
	case 0xfba, 0xfbb:
		if p.Strings == nil {
			return Record{}, 0, 0, fmt.Errorf("context string services are unavailable")
		}
		offset := 30
		if kind == 0xfbb {
			offset = 62
		}
		reader := p.Services.ReadContextPascal
		if reader == nil {
			reader = ReadScriptFrameContextPascal
		}
		pascal, err := reader(p.Context, offset)
		if err != nil {
			return Record{}, 0, 0, err
		}
		value, status, err := p.Strings.Store(pascal)
		return value, 1, uint32(status), err
	}
	if kind >= 16000 && kind <= 20109 {
		if p.Services.DispatchValue == nil {
			return Record{}, 0, 0, fmt.Errorf("value opcode dispatch is unavailable")
		}
		return p.Services.DispatchValue(p.Context, program, start, p.Local, p.Global, p.Strings)
	}
	name, status, err := identifierAt(program, start)
	if err != nil || status != 0 {
		return Record{}, 0, uint32(status), err
	}
	if kindAt(program, start+1) == 4018 {
		if p.Services.CallBlock == nil {
			return Record{}, 0, 0, fmt.Errorf("script call handler is unavailable")
		}
		return p.Services.CallBlock(p.Context, program, start, p.Local, p.Global, p.Strings)
	}
	value, status, err := readVariableValue(p.Local, name, &program.Records[start], p.Strings)
	if err != nil || status == 0 {
		return value, 1, uint32(status), err
	}
	if status != 9 {
		return Record{}, 0, uint32(status), nil
	}
	value, status, err = readVariableValue(p.Global, name, &program.Records[start], p.Strings)
	return value, 1, uint32(status), err
}

func (p *ExpressionValueParser) parsePathValue(program Program, start int) (Record, uint32, uint32, error) {
	if p.ContextPages == nil || p.Strings == nil {
		return Record{}, 0, 0, fmt.Errorf("native script context pages or string registers are unavailable")
	}
	if kindAt(program, start+1) != LookupOpcode("(") {
		return Record{}, 0, 2, nil
	}
	index, consumed, status, err := p.Parse(program, start+2)
	if err != nil || status != 0 {
		return Record{}, 0, status, err
	}
	if index.Kind != 4 {
		return Record{}, 0, 14, nil
	}
	contextIndex := int(int32(index.Data))
	if contextIndex <= 0 || contextIndex >= nativeScriptContextCount {
		return Record{}, 0, 10, nil
	}
	pascal, err := p.ContextPages.Restore(contextIndex)
	if err != nil {
		return Record{}, 0, 0, err
	}
	value, stringStatus, err := p.Strings.Store(pascal)
	if err != nil || stringStatus != 0 {
		return value, consumed + 3, uint32(stringStatus), err
	}
	if kindAt(program, start+2+int(consumed)) != LookupOpcode(")") {
		return Record{}, 0, 2, nil
	}
	return value, consumed + 3, uint32(stringStatus), err
}

func readVariableValue(table *VariableTable, name []byte, cache *Record, strings *StringRegisters) (Record, uint16, error) {
	id, status, err := table.Lookup(name, cache)
	if err != nil || status != 0 {
		return Record{}, status, err
	}
	return table.ReadValue(id, strings)
}
