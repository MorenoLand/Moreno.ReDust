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
