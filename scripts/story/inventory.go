package story

import (
	. "redust/scripts"
	"encoding/binary"
	"fmt"
	"image"
	"strings"

	"redust/assets"
)

type InventoryExamination struct {
	Item, Movie, Stage, FlatName, BorderProp               string
	FadeOutTarget, FadeInTarget                            string
	ScriptResource                                         uint32
	BorderPoint                                            image.Point
	BorderDegree                                           int16
	FadeOutFrames, FadeInFrames                            int
	BlackScreen, BlackPalette, PlainAfterMovie, CloseStage bool
}

type InventoryBookContext struct {
	FlatIndex, FlatCount int
	Point                uint32
	BorderProp           string
	BorderDegree         int16
	PointInBorder        func(int16) bool
}

type InventoryBookAction struct {
	FlatIndex                                                 int
	FlatName, Sound, ReturnStage, FadeOutTarget, FadeInTarget string
	BorderDegree                                              int16
	BorderChanged, Close, DelegateStage, Plain                bool
	VisualEffect                                              uint16
	Duration, FadeOutFrames, FadeInFrames                     int
}

func ParseInventoryBookAction(program Program, context InventoryBookContext) (InventoryBookAction, bool, error) {
	start, end, err := deathCodeRange(program, "mousedown")
	if err != nil {
		return InventoryBookAction{}, false, err
	}
	if context.FlatIndex < 1 || context.FlatIndex > context.FlatCount {
		return InventoryBookAction{}, false, fmt.Errorf("book flat index %d exceeds count %d", context.FlatIndex, context.FlatCount)
	}
	action := InventoryBookAction{FlatIndex: context.FlatIndex, BorderDegree: context.BorderDegree}
	num := int32(0)
	var expression func(*int, int) (Record, error)
	var atom func(*int) (Record, error)
	atom = func(position *int) (Record, error) {
		index := *position
		if index >= end {
			return Record{}, fmt.Errorf("book expression is truncated")
		}
		record := program.Records[index]
		*position++
		switch record.Kind {
		case 4:
			return record, nil
		case LookupOpcode("true"):
			return Record{Kind: 2, Data: 1}, nil
		case LookupOpcode("false"):
			return Record{Kind: 2}, nil
		case LookupOpcode("("):
			value, err := expression(position, 0)
			if err != nil || *position >= end || program.Records[*position].Kind != LookupOpcode(")") {
				return Record{}, fmt.Errorf("book expression parentheses are invalid: %w", err)
			}
			*position++
			return value, nil
		case 5:
			name, err := program.IdentifierPascal(index)
			if err != nil || string(name[1:]) != "num" {
				return Record{}, fmt.Errorf("book expression has an unsupported variable")
			}
			return Record{Kind: 4, Data: uint32(num)}, nil
		case LookupOpcode("flattoindex"):
			if index+5 >= end || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+2].Kind != LookupOpcode("currentflat") || program.Records[index+3].Kind != LookupOpcode("(") || program.Records[index+4].Kind != LookupOpcode(")") || program.Records[index+5].Kind != LookupOpcode(")") {
				return Record{}, fmt.Errorf("book flattoindex argument differs from its native form")
			}
			*position = index + 6
			return Record{Kind: 4, Data: uint32(context.FlatIndex)}, nil
		case LookupOpcode("countflats"):
			if index+2 >= end || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+2].Kind != LookupOpcode(")") {
				return Record{}, fmt.Errorf("book countflats argument is invalid")
			}
			*position = index + 3
			return Record{Kind: 4, Data: uint32(context.FlatCount)}, nil
		case LookupOpcode("propdeg"), LookupOpcode("pointx"), LookupOpcode("pointy"):
			if index+3 >= end || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+3].Kind != LookupOpcode(")") {
				return Record{}, fmt.Errorf("book getter argument is invalid")
			}
			*position = index + 4
			if record.Kind == LookupOpcode("propdeg") {
				if program.Records[index+2].Kind != LookupOpcode("me") {
					return Record{}, fmt.Errorf("book propdeg does not target its border")
				}
				return Record{Kind: 4, Data: uint32(int32(action.BorderDegree))}, nil
			}
			value := int32(int16(context.Point >> 16))
			if record.Kind == LookupOpcode("pointy") {
				value = int32(int16(context.Point))
			}
			return Record{Kind: 4, Data: uint32(value)}, nil
		case LookupOpcode("pointinprop"):
			if index+5 >= end || program.Records[index+1].Kind != LookupOpcode("(") || program.Records[index+2].Kind != LookupOpcode("me") || program.Records[index+3].Kind != LookupOpcode(",") || program.Records[index+5].Kind != LookupOpcode(")") || context.PointInBorder == nil {
				return Record{}, fmt.Errorf("book border hit test is unavailable")
			}
			*position = index + 6
			value := uint32(0)
			if context.PointInBorder(action.BorderDegree) {
				value = 1
			}
			return Record{Kind: 2, Data: value}, nil
		}
		return Record{}, fmt.Errorf("book expression opcode %d is unsupported", record.Kind)
	}
	expression = func(position *int, minimum int) (Record, error) {
		left, err := atom(position)
		if err != nil {
			return Record{}, err
		}
		for *position < end {
			opcode := program.Records[*position].Kind
			precedence, found := ExpressionPrecedence(opcode)
			binding := 7 - int(precedence)
			if !found || binding < minimum {
				break
			}
			*position++
			right, err := expression(position, binding+1)
			if err != nil {
				return Record{}, err
			}
			var status uint16
			left, status, err = ApplyBinaryOperator(left, right, opcode, nil)
			if err != nil || status != 0 {
				return Record{}, fmt.Errorf("book expression failed with status %#x: %w", status, err)
			}
		}
		return left, nil
	}
	type condition struct{ parent, selected bool }
	stack, active, changed := []condition{}, true, false
	for index := start; index < end; index++ {
		record := program.Records[index]
		if record.Kind == LookupOpcode("if") {
			selected := false
			if active {
				position := index + 1
				value, err := expression(&position, 0)
				if err != nil || value.Kind != 2 {
					return InventoryBookAction{}, false, fmt.Errorf("book if condition is invalid: %w", err)
				}
				selected, index = value.Data != 0, position-1
			}
			stack = append(stack, condition{parent: active, selected: selected})
			active = active && selected
			continue
		}
		if record.Kind == LookupOpcode("else") || record.Kind == LookupOpcode("endif") {
			if len(stack) == 0 {
				return InventoryBookAction{}, false, fmt.Errorf("book has an unmatched conditional")
			}
			frame := stack[len(stack)-1]
			active = frame.parent && !frame.selected
			if record.Kind == LookupOpcode("endif") {
				active, stack = frame.parent, stack[:len(stack)-1]
			}
			continue
		}
		if !active {
			continue
		}
		if record.Kind == 5 && index+1 < end && program.Records[index+1].Kind == LookupOpcode("=") {
			name, _ := program.IdentifierPascal(index)
			if string(name[1:]) != "num" {
				return InventoryBookAction{}, false, fmt.Errorf("book assignment variable is unsupported")
			}
			position := index + 2
			value, err := expression(&position, 0)
			if err != nil || value.Kind != 4 {
				return InventoryBookAction{}, false, fmt.Errorf("book page assignment is invalid: %w", err)
			}
			num, index = int32(value.Data), position-1
			continue
		}
		switch record.Kind {
		case LookupOpcode("exitcode"), LookupOpcode("endcode"):
			return action, changed, nil
		case LookupOpcode("gotoflat"):
			if index+2 >= end {
				return InventoryBookAction{}, false, fmt.Errorf("book gotoflat argument is missing")
			}
			if program.Records[index+2].Kind == 3 {
				action.FlatName, err = deathStringArgument(program, index+2)
			} else {
				position := index + 2
				value, evalErr := expression(&position, 0)
				err, action.FlatIndex = evalErr, int(int32(value.Data))
			}
			changed = true
		case LookupOpcode("propdeg"):
			var degree int
			degree, err = deathNumberArgument(program, index+4)
			action.BorderDegree, action.BorderChanged, changed = int16(degree), true, true
			index += 5
		case LookupOpcode("multiplesound"):
			action.Sound, err = deathStringArgument(program, index+2)
		case LookupOpcode("visualeffect"):
			if index+4 >= end {
				return InventoryBookAction{}, false, fmt.Errorf("book effect argument is missing")
			}
			action.VisualEffect = program.Records[index+2].Kind
			action.Duration, err = deathNumberArgument(program, index+4)
			action.Plain = action.VisualEffect == LookupOpcode("plain")
		case LookupOpcode("sendtostage"):
			action.DelegateStage, changed = true, true
			index += 7
		case LookupOpcode("screentoblack"), LookupOpcode("blacktoscreen"):
			var target string
			var frames int
			target, err = deathStringArgument(program, index+2)
			if err == nil {
				frames, err = deathNumberArgument(program, index+4)
			}
			if record.Kind == LookupOpcode("screentoblack") {
				action.FadeOutTarget, action.FadeOutFrames = target, frames
			} else {
				action.FadeInTarget, action.FadeInFrames = target, frames
			}
		case LookupOpcode("openstagefile"):
			var name string
			name, err = deathStringArgument(program, index+2)
			action.ReturnStage, action.Close, changed = "DATA/"+strings.ToUpper(name), true, true
		}
		if err != nil {
			return InventoryBookAction{}, false, err
		}
	}
	return action, changed, nil
}

func ExamineInventoryItem(workspace assets.Workspace, item string, degree int16) (InventoryExamination, bool, error) {
	if item == "" {
		return InventoryExamination{}, false, nil
	}
	cache, err := workspace.OpenResourceCache("DATA/INVEN.PRP")
	if err != nil {
		return InventoryExamination{}, false, err
	}
	defer cache.Close()
	read := func(resource uint32) ([]byte, error) {
		lease, err := cache.Acquire(resource)
		if err != nil {
			return nil, err
		}
		data, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		return data, err
	}
	metadata, err := read(0)
	if err != nil {
		return InventoryExamination{}, false, err
	}
	if len(metadata) < 0x93c {
		return InventoryExamination{}, false, fmt.Errorf("inventory prop list is truncated")
	}
	countValue := binary.LittleEndian.Uint32(metadata[0x938:0x93c])
	if uint64(countValue) > uint64((len(metadata)-0x93c)/0x10) {
		return InventoryExamination{}, false, fmt.Errorf("inventory prop count exceeds its list")
	}
	count := int(countValue)
	for index := 0; index < count; index++ {
		offset := 0x93c + index*0x10
		definition, err := read(binary.LittleEndian.Uint32(metadata[offset : offset+4]))
		if err != nil {
			return InventoryExamination{}, false, err
		}
		if len(definition) < 0x2b || int(definition[0x2a])+0x2b > len(definition) {
			return InventoryExamination{}, false, fmt.Errorf("inventory definition %d is truncated", index)
		}
		name := string(definition[0x2b : 0x2b+int(definition[0x2a])])
		if !strings.EqualFold(item, name) {
			continue
		}
		resource := binary.LittleEndian.Uint32(definition[0x26:0x2a])
		data, err := read(resource)
		if err != nil {
			return InventoryExamination{}, false, err
		}
		program, err := ParseProgram(data)
		if err != nil {
			return InventoryExamination{}, false, err
		}
		sharedData, err := read(1)
		if err != nil {
			return InventoryExamination{}, false, err
		}
		shared, err := ParseProgram(sharedData)
		if err != nil {
			return InventoryExamination{}, false, err
		}
		action, found, err := ParseInventoryExamination(program, shared, name, degree)
		action.ScriptResource = resource
		return action, found, err
	}
	return InventoryExamination{}, false, fmt.Errorf("inventory has no prop %q", item)
}

func ParseInventoryExamination(program, shared Program, item string, degree int16) (InventoryExamination, bool, error) {
	action := InventoryExamination{Item: item}
	if err := parseInventoryExaminationCode(program, shared, "infoyoself", degree, &action); err != nil {
		return InventoryExamination{}, false, err
	}
	return action, action.Movie != "" || action.Stage != "", nil
}

func parseInventoryExaminationCode(program, shared Program, code string, degree int16, action *InventoryExamination) error {
	start, end, err := deathCodeRange(program, code)
	if err != nil {
		return err
	}
	type branch struct {
		kind              uint16
		parent, condition bool
	}
	stack, active := []branch{}, true
	for index := start + 2; index < end; index++ {
		record := program.Records[index]
		switch record.Kind {
		case LookupOpcode("if"), LookupOpcode("switch"):
			if index+4 >= end || program.Records[index+1].Kind != LookupOpcode("propdeg") || program.Records[index+2].Kind != LookupOpcode("(") || program.Records[index+3].Kind != LookupOpcode("me") || program.Records[index+4].Kind != LookupOpcode(")") {
				return fmt.Errorf("inventory %s has unsupported degree condition at %d", action.Item, index)
			}
			frame := branch{kind: record.Kind, parent: active}
			if record.Kind == LookupOpcode("if") {
				if index+6 >= end || program.Records[index+5].Kind != LookupOpcode("=") || program.Records[index+6].Kind != 4 {
					return fmt.Errorf("inventory %s has unsupported degree comparison", action.Item)
				}
				frame.condition = int32(degree) == int32(program.Records[index+6].Data)
				index += 6
			} else {
				index += 4
			}
			stack = append(stack, frame)
			active = frame.parent && frame.condition
			continue
		case LookupOpcode("else"):
			if len(stack) == 0 || stack[len(stack)-1].kind != LookupOpcode("if") {
				return fmt.Errorf("inventory %s has an unmatched else", action.Item)
			}
			frame := stack[len(stack)-1]
			active = frame.parent && !frame.condition
			continue
		case LookupOpcode("case"):
			if len(stack) == 0 || stack[len(stack)-1].kind != LookupOpcode("switch") || index+1 >= end || program.Records[index+1].Kind != 4 {
				return fmt.Errorf("inventory %s has an unsupported case", action.Item)
			}
			active = stack[len(stack)-1].parent && int32(degree) == int32(program.Records[index+1].Data)
			index++
			continue
		case LookupOpcode("endif"), LookupOpcode("endswitch"):
			if len(stack) == 0 {
				return fmt.Errorf("inventory %s has an unmatched branch end", action.Item)
			}
			active = stack[len(stack)-1].parent
			stack = stack[:len(stack)-1]
			continue
		}
		if !active {
			continue
		}
		if record.Kind == 5 {
			name, err := program.IdentifierPascal(index)
			if err != nil {
				return err
			}
			if string(name[1:]) == "invenmovie" {
				movie, err := deathStringArgument(program, index+2)
				if err != nil {
					return err
				}
				action.Movie = "INVEN/" + strings.ToUpper(movie)
				if err := parseInventoryExaminationCode(shared, Program{}, "invenmovie", degree, action); err != nil {
					return err
				}
			}
			continue
		}
		switch record.Kind {
		case LookupOpcode("screentoblack"), LookupOpcode("blacktoscreen"):
			target, err := deathStringArgument(program, index+2)
			if err != nil {
				return err
			}
			frames, err := deathNumberArgument(program, index+4)
			if err != nil {
				return err
			}
			if record.Kind == LookupOpcode("screentoblack") {
				action.FadeOutTarget, action.FadeOutFrames = target, frames
			} else {
				action.FadeInTarget, action.FadeInFrames = target, frames
			}
		case LookupOpcode("blackscreen"):
			action.BlackScreen = true
		case LookupOpcode("closestagefile"):
			action.CloseStage = true
		case LookupOpcode("playmovie"):
			if index+2 < end && program.Records[index+2].Kind == 3 {
				movie, err := deathStringArgument(program, index+2)
				if err != nil {
					return err
				}
				action.Movie = "INVEN/" + strings.ToUpper(movie)
			} else if code != "invenmovie" {
				return fmt.Errorf("inventory %s has an unsupported movie argument", action.Item)
			}
		case LookupOpcode("clut"):
			name, err := deathStringArgument(program, index+2)
			if err != nil || name != "black" {
				return fmt.Errorf("inventory %s has an unsupported palette", action.Item)
			}
			action.BlackPalette = true
		case LookupOpcode("visualeffect"):
			if index+4 >= end || program.Records[index+2].Kind != LookupOpcode("plain") || program.Records[index+4].Kind != 4 || program.Records[index+4].Data != 0 {
				return fmt.Errorf("inventory %s has an unsupported visual effect", action.Item)
			}
			action.PlainAfterMovie = true
		case LookupOpcode("propvisible"):
			action.BorderProp, err = deathStringArgument(program, index+2)
		case LookupOpcode("propxy"):
			var x, y int
			x, err = deathNumberArgument(program, index+4)
			if err == nil {
				y, err = deathNumberArgument(program, index+6)
			}
			action.BorderPoint = image.Pt(x, y)
		case LookupOpcode("propdeg"):
			var value int
			value, err = deathNumberArgument(program, index+4)
			action.BorderDegree = int16(value)
		case LookupOpcode("openstagefile"):
			var stage string
			stage, err = deathStringArgument(program, index+2)
			action.Stage = "INVEN/" + strings.ToUpper(stage)
		case LookupOpcode("gotoflat"):
			action.FlatName, err = deathStringArgument(program, index+2)
		}
		if err != nil {
			return err
		}
	}
	if len(stack) != 0 {
		return fmt.Errorf("inventory %s has an unterminated degree branch", action.Item)
	}
	return nil
}
