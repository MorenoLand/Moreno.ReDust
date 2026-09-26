package scripts

import "errors"

var ErrUnterminatedBlock = errors.New("script block ended before its closing parenthesis")

func ParenthesizedBlockEnd(records []Record, start int) (int, error) {
	if start < 0 || start >= len(records) {
		return 0, ErrUnterminatedBlock
	}
	depth := 0
	for i := start; i < len(records); i++ {
		switch records[i].Kind {
		case 4018:
			depth++
		case 4019:
			depth--
			if depth < 1 {
				return i + 1, nil
			}
		case 0, 6:
			return 0, ErrUnterminatedBlock
		}
	}
	return 0, ErrUnterminatedBlock
}
