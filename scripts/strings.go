package scripts

import "fmt"

type StringRegisters struct {
	slots  [20][256]byte
	active [20]bool
}

func (s *StringRegisters) Reset() {
	if s != nil {
		s.active = [20]bool{}
	}
}

func (s *StringRegisters) Store(pascal []byte) (Record, uint16, error) {
	if len(pascal) == 0 || int(pascal[0])+1 != len(pascal) {
		return Record{}, 0, fmt.Errorf("malformed Pascal string")
	}
	if s == nil {
		return Record{}, 0, fmt.Errorf("string register set is nil")
	}
	for i := range s.slots {
		if !s.active[i] {
			copy(s.slots[i][:], pascal)
			s.active[i] = true
			return Record{Kind: 3, Data: uint32(i)}, 0, nil
		}
	}
	return Record{}, 0x36, nil
}

func (s *StringRegisters) Load(value Record) ([]byte, uint16, error) {
	if value.Kind != 3 {
		return nil, 14, nil
	}
	if s == nil {
		return nil, 0, fmt.Errorf("string register set is nil")
	}
	index := int(int16(uint16(value.Data)))
	if index < 0 || index >= len(s.slots) {
		return nil, 0, fmt.Errorf("string register index %d is out of range", index)
	}
	if !s.active[index] {
		return nil, 0, fmt.Errorf("string register %d is not active", index)
	}
	length := int(s.slots[index][0])
	result := append([]byte(nil), s.slots[index][:length+1]...)
	s.active[index] = false
	return result, 0, nil
}

// Active counts the registers holding a value, for leak diagnostics.
func (s *StringRegisters) Active() int {
	count := 0
	if s != nil {
		for _, active := range s.active {
			if active {
				count++
			}
		}
	}
	return count
}
