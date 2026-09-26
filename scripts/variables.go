package scripts

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type VariableSlot [32]byte

type VariableTable struct {
	slots          []VariableSlot
	capacity       int
	stringHeap     []byte
	stringUsed     int
	stringCapacity int
	stringGarbage  int
}

var ErrInvalidVariableCache = errors.New("variable cache record is missing")

func NewVariableTable() *VariableTable {
	return &VariableTable{slots: make([]VariableSlot, 0, 20), capacity: 20, stringHeap: make([]byte, 0, 0x800), stringCapacity: 0x800}
}

func (t *VariableTable) ResolveOrCreate(name []byte, cache *Record) (uint16, uint16, error) {
	if len(name) == 0 || int(name[0])+1 != len(name) {
		return 0, 0, fmt.Errorf("malformed Pascal variable name")
	}
	if name[0] > 15 {
		return 0, 1, nil
	}
	if cache == nil {
		return 0, 0, ErrInvalidVariableCache
	}
	id, status, err := t.Lookup(name, cache)
	if err != nil || status == 0 {
		return id, status, err
	}
	if t == nil {
		return 0, 9, nil
	}
	if len(t.slots) > 31999 {
		return 0, 9, nil
	}
	id = uint16(len(t.slots))
	cache.Tail = id
	if len(t.slots) == t.capacity {
		t.capacity += 20
	}
	var slot VariableSlot
	binary.LittleEndian.PutUint16(slot[:2], 4)
	copy(slot[16:], name)
	t.slots = append(t.slots, slot)
	return id, 0, nil
}

func (t *VariableTable) Lookup(name []byte, cache *Record) (uint16, uint16, error) {
	if len(name) == 0 || int(name[0])+1 != len(name) {
		return 0, 0, fmt.Errorf("malformed Pascal variable name")
	}
	if cache == nil {
		return 0, 0, ErrInvalidVariableCache
	}
	if id, found := t.find(name, cache.Tail); found {
		cache.Tail = id
		return id, 0, nil
	}
	return 0, 9, nil
}

func ResolveVariableList(program *Program, start int, table *VariableTable) (int32, uint16, error) {
	if program == nil || start < 0 || start >= len(program.Records) {
		return -1, 0, fmt.Errorf("variable-list start index %d is out of range", start)
	}
	current := start
	for {
		identifierIndex := current + 1
		name, status, err := identifierAt(*program, identifierIndex)
		if err != nil || status != 0 {
			return -1, status, err
		}
		_, status, err = table.ResolveOrCreate(name, &program.Records[identifierIndex])
		if err != nil || status != 0 {
			return -1, status, err
		}
		current += 2
		if current < 0 || current >= len(program.Records) {
			return -1, 0, fmt.Errorf("variable-list index %d is out of range", current)
		}
		if program.Records[current].Kind != 4020 {
			return int32(current - start), 0, nil
		}
	}
}

func (t *VariableTable) find(name []byte, cached uint16) (uint16, bool) {
	if t == nil {
		return 0, false
	}
	if id := int(int16(cached)); id > 0 && id < len(t.slots) && equalPascalString(t.slots[id].pascalName(), name) {
		return uint16(id), true
	}
	for id, slot := range t.slots {
		if equalPascalString(slot.pascalName(), name) {
			return uint16(id), true
		}
	}
	return 0, false
}

func (s VariableSlot) ValueType() uint16 { return binary.LittleEndian.Uint16(s[:2]) }

func (s VariableSlot) pascalName() []byte {
	length := int(s[16])
	return s[16 : 17+length]
}
