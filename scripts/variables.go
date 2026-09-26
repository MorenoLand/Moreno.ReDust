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

func (t *VariableTable) ReadValue(id uint16, strings *StringRegisters) (Record, uint16, error) {
	if t == nil || int(id) >= len(t.slots) {
		return Record{}, 9, nil
	}
	slot := t.slots[id]
	value := Record{Kind: slot.ValueType(), Data: binary.LittleEndian.Uint32(slot[2:6])}
	if value.Kind != 3 {
		return value, 0, nil
	}
	offset := int(int32(value.Data))
	if strings == nil {
		return Record{}, 0, fmt.Errorf("expression string registers are unavailable")
	}
	pascal, err := t.readHeapString(offset)
	if err != nil {
		return Record{}, 0, err
	}
	return strings.Store(pascal)
}

func (t *VariableTable) WriteValue(id uint16, value ExpressionValue, strings *StringRegisters) (uint16, error) {
	if t == nil || int(int16(id)) < 0 || int(id) >= len(t.slots) {
		return 9, nil
	}
	slot := t.slots[id]
	if slot.ValueType() == 3 {
		pascal, err := t.readHeapString(int(int32(binary.LittleEndian.Uint32(slot[2:6]))))
		if err != nil {
			return 0, err
		}
		t.stringGarbage += len(pascal)
	}
	kind := binary.LittleEndian.Uint16(value[:2])
	data := binary.LittleEndian.Uint32(value[2:6])
	if kind == 3 {
		if strings == nil {
			return 0, fmt.Errorf("expression string registers are unavailable")
		}
		pascal, status, err := strings.Load(Record{Kind: 3, Data: data})
		if err != nil || status != 0 {
			return status, err
		}
		data, err = t.appendString(pascal)
		if err != nil {
			return 0, err
		}
	}
	binary.LittleEndian.PutUint16(slot[:2], kind)
	binary.LittleEndian.PutUint32(slot[2:6], data)
	t.slots[id] = slot
	if t.stringGarbage > 0x7ff {
		if err := t.compactStrings(); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func (t *VariableTable) Remove(name []byte, cache *Record) (uint16, error) {
	if len(name) == 0 || int(name[0])+1 != len(name) {
		return 0, fmt.Errorf("malformed Pascal variable name")
	}
	if name[0] > 15 {
		return 1, nil
	}
	id, status, err := t.Lookup(name, cache)
	if err != nil {
		return 0, err
	}
	if status != 0 {
		return status, fmt.Errorf("variable deletion target is not present")
	}
	slot := t.slots[id]
	if slot.ValueType() == 3 {
		pascal, err := t.readHeapString(int(int32(binary.LittleEndian.Uint32(slot[2:6]))))
		if err != nil {
			return 0, err
		}
		t.stringGarbage += len(pascal)
	}
	last := len(t.slots) - 1
	t.slots[id] = t.slots[last]
	t.slots = t.slots[:last]
	if t.stringGarbage > 0x7ff {
		if err := t.compactStrings(); err != nil {
			return 0, err
		}
	}
	return 0, nil
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

func RemoveVariableList(program *Program, start int, table *VariableTable) (int32, uint16, error) {
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
		status, err = table.Remove(name, &program.Records[identifierIndex])
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

func (t *VariableTable) readHeapString(offset int) ([]byte, error) {
	if t == nil || offset < 0 || offset >= t.stringUsed || offset >= len(t.stringHeap) {
		return nil, fmt.Errorf("variable string offset %d is outside the heap", offset)
	}
	length := int(t.stringHeap[offset])
	if length+1 > t.stringUsed-offset || length+1 > len(t.stringHeap)-offset {
		return nil, fmt.Errorf("variable string at offset %d is truncated", offset)
	}
	return append([]byte(nil), t.stringHeap[offset:offset+length+1]...), nil
}

func (t *VariableTable) appendString(pascal []byte) (uint32, error) {
	if len(pascal) == 0 || int(pascal[0])+1 != len(pascal) {
		return 0, fmt.Errorf("malformed Pascal string")
	}
	if t.stringCapacity <= t.stringUsed+len(pascal) {
		t.stringCapacity += len(pascal) + 0x800
	}
	if len(t.stringHeap) != t.stringUsed {
		return 0, fmt.Errorf("variable string heap length %d differs from used size %d", len(t.stringHeap), t.stringUsed)
	}
	offset := uint32(t.stringUsed)
	t.stringHeap = append(t.stringHeap, pascal...)
	t.stringUsed += len(pascal)
	return offset, nil
}

func (t *VariableTable) compactStrings() error {
	newCapacity := 0x800
	newHeap := make([]byte, 0, newCapacity)
	for i := range t.slots {
		if t.slots[i].ValueType() != 3 {
			continue
		}
		oldOffset := int(int32(binary.LittleEndian.Uint32(t.slots[i][2:6])))
		pascal, err := t.readHeapString(oldOffset)
		if err != nil {
			return err
		}
		if len(newHeap)+len(pascal) >= newCapacity {
			newCapacity += len(pascal) + 0x800
		}
		newOffset := len(newHeap)
		newHeap = append(newHeap, pascal...)
		binary.LittleEndian.PutUint32(t.slots[i][2:6], uint32(newOffset))
	}
	t.stringHeap = newHeap
	t.stringUsed = len(newHeap)
	t.stringCapacity = newCapacity
	t.stringGarbage = 0
	return nil
}
