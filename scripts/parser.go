package scripts

import (
	"fmt"
	"strconv"
)

type Record struct {
	Kind uint16
	Data uint32
	Tail uint16
}

type Program struct {
	Records    []Record
	StringPool []byte
}

type ParseError struct {
	Offset int
	Code   uint16
	Reason string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("script parse error at byte %d (code %d): %s", e.Offset, e.Code, e.Reason)
}

func (p Program) Bytes() []byte {
	data := make([]byte, len(p.Records)*8+len(p.StringPool))
	for i, record := range p.Records {
		offset := i * 8
		data[offset] = byte(record.Kind)
		data[offset+1] = byte(record.Kind >> 8)
		data[offset+2] = byte(record.Data)
		data[offset+3] = byte(record.Data >> 8)
		data[offset+4] = byte(record.Data >> 16)
		data[offset+5] = byte(record.Data >> 24)
		data[offset+6] = byte(record.Tail)
		data[offset+7] = byte(record.Tail >> 8)
	}
	copy(data[len(p.Records)*8:], p.StringPool)
	return data
}

type stringPool struct {
	data    []byte
	offsets map[string]uint32
}

func (p *stringPool) intern(value []byte) uint32 {
	key := string(value)
	if offset, ok := p.offsets[key]; ok {
		return offset
	}
	offset := uint32(len(p.data))
	p.data = append(p.data, byte(len(value)))
	p.data = append(p.data, value...)
	p.offsets[key] = offset
	return offset
}

func CompileText(source []byte) (Program, error) {
	program := Program{}
	pool := stringPool{offsets: map[string]uint32{}}
	poolRefs := []bool{}
	appendRecord := func(record Record, poolReference bool) {
		program.Records = append(program.Records, record)
		poolRefs = append(poolRefs, poolReference)
	}
	for position := 0; position < len(source); {
		current := source[position]
		switch current {
		case ' ':
			position++
			for position < len(source) && source[position] == ' ' {
				position++
			}
		case '\r':
			position++
			indent := uint32(0)
			for position < len(source) && source[position] == '\t' {
				indent++
				position++
			}
			appendRecord(Record{Kind: 6, Data: indent}, false)
		case '"':
			start := position + 1
			end := start
			for end < len(source) && source[end] != '"' && source[end] != '\r' {
				end++
			}
			if end == len(source) || source[end] != '"' {
				return Program{}, &ParseError{Offset: start, Code: 0x26, Reason: "unterminated quoted string"}
			}
			if end-start > 0xff {
				return Program{}, &ParseError{Offset: start, Code: 0x26, Reason: "quoted string exceeds 255 bytes"}
			}
			offset := pool.intern(source[start:end])
			appendRecord(Record{Kind: 3, Data: offset}, true)
			position = end + 1
		default:
			if current >= '0' && current <= '9' {
				start := position
				for position < len(source) && source[position] >= '0' && source[position] <= '9' {
					position++
				}
				if position-start > 0xff {
					return Program{}, &ParseError{Offset: start, Code: 0x26, Reason: "number token exceeds 255 bytes"}
				}
				value, err := strconv.ParseInt(string(source[start:position]), 10, 32)
				if err != nil {
					return Program{}, &ParseError{Offset: start, Code: 0x26, Reason: "number is outside the 32-bit range"}
				}
				appendRecord(Record{Kind: 4, Data: uint32(value)}, false)
				continue
			}
			if isAlphaNumeric(current) {
				start := position
				for position < len(source) && isAlphaNumeric(source[position]) {
					position++
				}
				if position-start > 0xff {
					return Program{}, &ParseError{Offset: start, Code: 0x26, Reason: "word token exceeds 255 bytes"}
				}
				word := source[start:position]
				if opcode := LookupOpcode(string(word)); opcode != 1 {
					appendRecord(Record{Kind: opcode}, false)
				} else {
					appendRecord(Record{Kind: 5, Data: pool.intern(word)}, true)
				}
				continue
			}
			opcode := LookupOpcode(string([]byte{current}))
			if opcode == 1 {
				return Program{}, &ParseError{Offset: position + 1, Code: 0x26, Reason: fmt.Sprintf("unrecognized punctuation byte 0x%02x", current)}
			}
			appendRecord(Record{Kind: opcode}, false)
			position++
		}
	}
	recordBytes := uint32(len(program.Records) * 8)
	for i, isPoolReference := range poolRefs {
		if isPoolReference {
			program.Records[i].Data += recordBytes
		}
	}
	program.StringPool = pool.data
	return program, nil
}

func isAlphaNumeric(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}
