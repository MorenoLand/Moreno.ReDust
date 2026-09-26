package scripts

import (
	"errors"
	"fmt"
)

type ExecutionFrame struct {
	Records        []Record
	CodeStart      int
	ProgramCounter int
}

type CodeSession interface {
	EvaluateExpression(start int) (branchOffset int32, status uint16, err error)
	DispatchStatement(start int) (uint16, error)
	Close()
}

type CodeSessionFactory func() (CodeSession, error)

var ErrNoCodeSession = errors.New("code session factory returned no session")
var ErrNoVariableDeclarationDispatcher = errors.New("variable declaration dispatcher is unavailable")
var ErrEscapeInterruptInputUnavailable = errors.New("escape interrupt input is unavailable")
var ErrNoStatementResultDispatcher = errors.New("statement result dispatcher is unavailable")

type VariableDeclarationSession interface {
	DispatchVariableDeclaration(start int) (recordsConsumed int32, status uint16, err error)
}

type StatementInterruptSession interface {
	StatementInterrupt() (status uint16, err error)
}

type StatementResultSession interface {
	DispatchStatementResult(start int) (recordsConsumed int32, status uint16, err error)
}

var namedControlBoundaries = [...]string{
	"opencast", "openactor", "closecast", "closeactor", "openflat", "openstage", "closeflat", "closestage",
	"endwalk", "endturn", "endball", "endloop", "openshop", "openprop", "closeshop", "closeprop",
	"openpuppet", "closepuppet", "menustate", "boot", "idle", "menuselect", "keydown", "keyrepeat",
	"mousedown", "openset", "openfloor", "openscene", "closeset", "closefloor", "closescene",
}

func ResolveNamedBoundary(program *Program, recordIndex int) (uint16, error) {
	if program == nil || recordIndex < 0 || recordIndex >= len(program.Records) {
		return 0, fmt.Errorf("boundary record index %d is out of range", recordIndex)
	}
	record := &program.Records[recordIndex]
	if record.Kind != 5 {
		record.Tail = 1
		return 0, nil
	}
	value, err := program.IdentifierPascal(recordIndex)
	if err != nil {
		return 0, err
	}
	if record.Tail != 0 {
		return record.Tail - 1, nil
	}
	for _, name := range namedControlBoundaries {
		if equalPascalText(value, name) {
			record.Tail = 2
			return 1, nil
		}
	}
	record.Tail = 1
	return 0, nil
}

func equalPascalText(value []byte, text string) bool {
	if len(value) != len(text)+1 || int(value[0]) != len(text) {
		return false
	}
	for i := range text {
		left := value[i+1]
		if left >= 'A' && left <= 'Z' {
			left += 'a' - 'A'
		}
		if left != text[i] {
			return false
		}
	}
	return true
}

type ScriptFrameState [39]uint16

func (f *ScriptFrameState) SetBaseByteOffset(offset uint32) {
	f[10] = uint16(offset)
	f[11] = uint16(offset >> 16)
}

func (f ScriptFrameState) RecordsConsumed() int32 {
	return int32(uint32(f[6]) | uint32(f[7])<<16)
}

type ScriptExecutionState struct {
	SavedFrame ScriptFrameState
	Saved      bool
}

func (s *ScriptExecutionState) saveFrame(frame *ScriptFrameState) {
	if !s.Saved && int16(frame[12]) > 0 {
		s.SavedFrame = *frame
		s.Saved = true
	}
}

type ParenthesizedBlockRunner func(context, callState *ScriptFrameState, start int) (uint16, error)

func ExecuteParenthesizedBlock(program *Program, start int, contexts []ScriptFrameState, contextIndex int, callState *ScriptFrameState, state *ScriptExecutionState, availableBytes func() int32, run ParenthesizedBlockRunner) (uint16, int, error) {
	if availableBytes == nil {
		return 0, 0, fmt.Errorf("available-memory probe is unavailable")
	}
	if availableBytes() < 0x800 {
		return 0x2c, 0, nil
	}
	if program == nil || start < 0 || start >= len(program.Records) {
		return 0, 0, fmt.Errorf("parenthesized block start index %d is out of range", start)
	}
	end, scanStatus, err := ParenthesizedBlockScan(program.Records, start)
	if err != nil {
		return 0, 0, err
	}
	if scanStatus != 0 {
		return scanStatus, 0, nil
	}
	consumed := end - start
	if callState == nil || state == nil || run == nil {
		return 0, consumed, fmt.Errorf("parenthesized block runtime is incomplete")
	}
	if contextIndex < 0 || contextIndex >= len(contexts) {
		return 0, consumed, fmt.Errorf("script context index %d is out of range", contextIndex)
	}
	for {
		context := &contexts[contextIndex]
		status, err := run(context, callState, start)
		if err != nil {
			return 0, consumed, err
		}
		if status == 0 {
			return 0, consumed, nil
		}
		if status == 4 {
			if context[0] == 0 {
				status = 0
			}
			boundary, err := ResolveNamedBoundary(program, start)
			if err != nil {
				return 0, consumed, err
			}
			if boundary != 0 {
				status = 0
			}
		}
		if status == 0 {
			if context[0] != 0 {
				return 0, consumed, nil
			}
			contextIndex++
			if contextIndex >= len(contexts) {
				return 0, consumed, fmt.Errorf("script context stack ended at frame %d", contextIndex)
			}
			continue
		}
		if status == 4 {
			callState[12] = 4
			delta := int32(uint32(start*8) - (uint32(callState[10]) | uint32(callState[11])<<16))
			callState[6] = uint16(delta >> 3)
			callState[7] = uint16(uint32(delta>>3) >> 16)
			state.saveFrame(callState)
			return 4, consumed, nil
		}
		context[12] = status
		state.saveFrame(context)
		return status, consumed, nil
	}
}

func ExecuteCodeBlock(frame *ExecutionFrame, factory CodeSessionFactory) (uint16, error) {
	if frame == nil || frame.CodeStart < 0 || frame.CodeStart >= len(frame.Records) {
		return 4, nil
	}
	markerOffset, found := FindCodeMarker(frame.Records[frame.CodeStart:])
	if !found {
		return 4, nil
	}
	marker := frame.CodeStart + markerOffset
	if factory == nil {
		return 0, ErrNoCodeSession
	}
	session, err := factory()
	if err != nil {
		return 0, err
	}
	if session == nil {
		return 0, ErrNoCodeSession
	}
	defer session.Close()
	for {
		frame.ProgramCounter = marker - frame.CodeStart
		branchOffset, status, err := session.EvaluateExpression(marker)
		if err != nil {
			return 0, err
		}
		if status != 0 {
			return status, nil
		}
		if branchOffset >= 0 {
			statement := marker + int(branchOffset)
			if statement < 0 || statement >= len(frame.Records) {
				return 0, fmt.Errorf("statement offset %d exceeds record stream", statement)
			}
			if frame.Records[statement].Kind == 4002 || frame.Records[statement].Kind == 4003 {
				declarations, ok := session.(VariableDeclarationSession)
				if !ok {
					return 0, ErrNoVariableDeclarationDispatcher
				}
				consumed, status, err := declarations.DispatchVariableDeclaration(statement)
				if err != nil || status != 0 {
					return status, err
				}
				next := statement + int(consumed)
				if next < 0 || next >= len(frame.Records) || frame.Records[next].Kind != 6 {
					return 0x1b, nil
				}
				for next < len(frame.Records) && frame.Records[next].Kind == 6 {
					next++
				}
				frame.ProgramCounter = next - frame.CodeStart
				interrupts, ok := session.(StatementInterruptSession)
				if !ok {
					return 0, ErrEscapeInterruptInputUnavailable
				}
				status, err = interrupts.StatementInterrupt()
				if err != nil || status != 0 {
					return status, err
				}
				if next >= len(frame.Records) {
					return 4, nil
				}
				marker = next
				continue
			}
			if results, ok := session.(StatementResultSession); ok {
				consumed, status, err := results.DispatchStatementResult(statement)
				if err != nil || status != 0 {
					return status, err
				}
				next := statement + int(consumed)
				if next < 0 || next >= len(frame.Records) || frame.Records[next].Kind != 6 {
					return 0x1b, nil
				}
				for next < len(frame.Records) && frame.Records[next].Kind == 6 {
					next++
				}
				frame.ProgramCounter = next - frame.CodeStart
				interrupts, ok := session.(StatementInterruptSession)
				if !ok {
					return 0, ErrEscapeInterruptInputUnavailable
				}
				status, err = interrupts.StatementInterrupt()
				if err != nil || status != 0 {
					return status, err
				}
				if next >= len(frame.Records) {
					return 4, nil
				}
				marker = next
				continue
			}
			status, err = session.DispatchStatement(statement)
			if err != nil || status != 0 {
				return status, err
			}
			if interrupts, ok := session.(StatementInterruptSession); ok {
				status, err = interrupts.StatementInterrupt()
				if err != nil || status != 0 {
					return status, err
				}
			}
			return status, nil
		}
		nextOffset, err := NextCodeOffset(frame.Records, marker)
		if err != nil {
			return 0, err
		}
		if nextOffset < 0 {
			return 4, nil
		}
		marker += int(nextOffset)
	}
}
