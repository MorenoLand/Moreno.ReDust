package scripts

import (
	"strconv"
	"strings"
)

// FormatProgram renders a tokenized program back to source text, inverting
// CompileText: kind 3 is a quoted literal, 4 a decimal number, 5 an
// identifier, 6 a line break carrying its tab indent, and every other kind is
// the opcode's spelling. It exists for diagnostics; the interpreter executes
// records, never this text.
func FormatProgram(program Program) string {
	var out strings.Builder
	previousWord := false
	for index, record := range program.Records {
		text := ""
		word := true
		switch record.Kind {
		case 0:
			return out.String()
		case 3:
			value, err := program.LiteralPascal(index)
			if err != nil {
				text = "\"?\""
			} else {
				text = strconv.Quote(string(value[1:]))
			}
		case 4:
			text = strconv.FormatInt(int64(int32(record.Data)), 10)
		case 5:
			value, err := program.IdentifierPascal(index)
			if err != nil {
				text = "?"
			} else {
				text = string(value[1:])
			}
		case 6:
			out.WriteByte('\n')
			if record.Data > 64 {
				out.WriteString("/*indent " + strconv.FormatUint(uint64(record.Data), 10) + "*/")
			} else {
				out.WriteString(strings.Repeat("\t", int(record.Data)))
			}
			previousWord = false
			continue
		default:
			text = CommandHandlerName(record.Kind)
			if text == "" {
				text = "op" + strconv.Itoa(int(record.Kind))
			}
			word = isAlphaNumeric(text[0])
		}
		if previousWord && word {
			out.WriteByte(' ')
		}
		out.WriteString(text)
		previousWord = word
	}
	return out.String()
}
