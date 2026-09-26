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
			return session.DispatchStatement(statement)
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
