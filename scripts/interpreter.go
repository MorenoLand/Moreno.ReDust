package scripts

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// The general script interpreter. It translates the native call and statement
// machinery as one unit:
//
//	FUN_0041C890  call a named code block across a frame chain
//	FUN_0041C9A0  search one frame's script for the block
//	FUN_0041CA70  match a block's name and bind its parameters
//	FUN_0041CC20  execute the block's statements
//	FUN_00422870  expression atoms (ExpressionValueParser), whose user calls
//	              re-enter FUN_0041C890 with a return slot
//
// Engine-raised events enter the same way scripts do: the native code builds
// source text such as `"jones", endwalk()` (FUN_00410290), compiles it with
// FUN_00435610 and passes it to a sendto handler from a "System" frame
// (FUN_0040CA60). Commands and value builtins are delegated to a ScriptHost,
// mirroring FUN_00424890 (12000..16054 statements) and FUN_004137B0
// (16000..20109 values).

// Native statuses produced by the interpreter itself.
const (
	ScriptStatusOK uint16 = 0
	// ScriptStatusMalformed is a call or header whose parentheses or
	// parameter syntax are wrong.
	ScriptStatusMalformed uint16 = 2
	// ScriptStatusStackOverflow is a for or while nest deeper than 20.
	ScriptStatusStackOverflow uint16 = 3
	// ScriptStatusPassed is passcode, or a frame with no matching block.
	ScriptStatusPassed uint16 = 4
	// ScriptStatusReturnInEvent is `return` in a block called as a statement.
	ScriptStatusReturnInEvent uint16 = 5
	// ScriptStatusNoResult is exitcode, endcode or passcode in a block called
	// for its value.
	ScriptStatusNoResult uint16 = 6
	// ScriptStatusWrongType is an operand of the wrong value type.
	ScriptStatusWrongType uint16 = 0x0e
	// ScriptStatusMissingEquals is an assignment without `=`.
	ScriptStatusMissingEquals uint16 = 0x10
	// ScriptStatusLineExpected is a statement not followed by a line break.
	ScriptStatusLineExpected uint16 = 0x1b
	// ScriptStatusMissingComma is a call with fewer arguments than parameters.
	ScriptStatusMissingComma uint16 = 0x1c
	// ScriptStatusUnbalancedIf, ...For, ...Switch and ...While are blocks left
	// open at endcode or closed without an opener.
	ScriptStatusUnbalancedIf     uint16 = 0x1d
	ScriptStatusUnbalancedFor    uint16 = 0x1e
	ScriptStatusUnbalancedSwitch uint16 = 0x1f
	ScriptStatusMissingTo        uint16 = 0x20
	ScriptStatusUnbalancedWhile  uint16 = 0x25
	// ScriptStatusInterrupted is the escape-key statement interrupt.
	ScriptStatusInterrupted uint16 = 0x35
)

// Statement opcodes handled directly by FUN_0041CC20.
const (
	opCode      uint16 = 4001
	opGlobal    uint16 = 4002
	opLocal     uint16 = 4003
	opEndCode   uint16 = 4004
	opExitCode  uint16 = 4005
	opIf        uint16 = 4006
	opEndIf     uint16 = 4007
	opElse      uint16 = 4008
	opSwitch    uint16 = 4009
	opEndSwitch uint16 = 4010
	opCase      uint16 = 4011
	opFor       uint16 = 4012
	opTo        uint16 = 4013
	opStep      uint16 = 4014
	opEndFor    uint16 = 4015
	opWhile     uint16 = 4016
	opEndWhile  uint16 = 4017
	opOpen      uint16 = 4018
	opClose     uint16 = 4019
	opComma     uint16 = 4020
	opReturn    uint16 = 4024
	opPassCode  uint16 = 4025
	opDumpLocal uint16 = 4028
	opDumpGlob  uint16 = 4029
	opAssign    uint16 = 8008
	opLine      uint16 = 6
)

// nativeLoopStackDepth is the capacity of the global for and while stacks,
// DAT_00443C30 and DAT_00443D50; a 21st entry is status 3.
const nativeLoopStackDepth = 20

// ScriptFrame is one 78-byte native context frame (0x27 shorts). A chain of
// frames is searched in order for a called block: an actor's chain is its own
// script and then its cast's shared script (FUN_0040CB60).
type ScriptFrame struct {
	// Program is the frame's script; the native frame keeps it at +0x14.
	Program *Program
	// Me is the context string at +0x1E, read by `me`.
	Me string
	// Target is the context string at +0x3E, read by `target`.
	Target string
	// Label is the diagnostic label at +0x2E, such as "Actor Script: ".
	Label string
	// Owner identifies the host object the frame belongs to.
	Owner any
	// Last is word 0, which ends the chain search.
	Last bool
}

// ScriptHost executes the opcodes the interpreter does not own.
type ScriptHost interface {
	// Command runs a 12000..16054 statement at call.Start, reporting how many
	// records it consumed.
	Command(call *ScriptCall) (consumed int, status uint16, err error)
	// Value evaluates a 16000..20109 value builtin at call.Start.
	Value(call *ScriptCall) (value Record, consumed int, status uint16, err error)
}

// ScriptCall is the execution context handed to the host for one opcode.
type ScriptCall struct {
	Interpreter *Interpreter
	Chain       []ScriptFrame
	FrameIndex  int
	Locals      *VariableTable
	Program     *Program
	Start       int
}

// Frame returns the executing frame.
func (c *ScriptCall) Frame() *ScriptFrame { return &c.Chain[c.FrameIndex] }

// Kind returns the record kind at index.
func (c *ScriptCall) Kind(index int) uint16 { return KindAt(*c.Program, index) }

// Eval evaluates the expression at index in the caller's context.
func (c *ScriptCall) Eval(index int) (Record, int, uint16, error) {
	return c.Interpreter.evaluate(c.Chain, c.FrameIndex, c.Locals, c.Program, index)
}

// Args parses `( expr , expr ... )` after the opcode, evaluating each
// argument. It returns the arguments and the records consumed including the
// opcode itself.
func (c *ScriptCall) Args() ([]ScriptValue, int, uint16, error) {
	position := c.Start + 1
	if c.Kind(position) != opOpen {
		return nil, 0, ScriptStatusMalformed, nil
	}
	position++
	var values []ScriptValue
	if c.Kind(position) == opClose {
		return values, position + 1 - c.Start, 0, nil
	}
	for {
		record, consumed, status, err := c.Eval(position)
		if err != nil || status != 0 {
			return nil, 0, status, err
		}
		value, status, err := c.Interpreter.Value(record)
		if err != nil || status != 0 {
			return nil, 0, status, err
		}
		values = append(values, value)
		position += consumed
		switch c.Kind(position) {
		case opComma:
			position++
		case opClose:
			return values, position + 1 - c.Start, 0, nil
		default:
			return nil, 0, ScriptStatusMalformed, nil
		}
	}
}

// ScriptValue is a decoded native value: type 2 boolean, 3 string, 4 number.
type ScriptValue struct {
	Kind uint16
	Int  int32
	Text string
}

func (v ScriptValue) Bool() bool { return v.Kind == 2 && v.Int != 0 }

func (v ScriptValue) String() string {
	switch v.Kind {
	case 2:
		if v.Int != 0 {
			return "true"
		}
		return "false"
	case 3:
		return fmt.Sprintf("%q", v.Text)
	case 4:
		return fmt.Sprint(v.Int)
	}
	return fmt.Sprintf("value(kind %d)", v.Kind)
}

type forEntry struct {
	variable uint16
	locals   *VariableTable
	end      int32
	step     int32
	body     int
}

// Interpreter owns the state the native interpreter keeps in globals.
type Interpreter struct {
	Global      *VariableTable
	Strings     *StringRegisters
	Expressions *ExpressionState
	Host        ScriptHost
	// EscapeInterrupt, when non-nil and true, makes each statement boundary
	// check for the escape key (DAT_00459996 and FUN_0042EB50).
	EscapeInterrupt func() bool
	// Builtins replace named script code with native behavior. The inventory
	// picker (INVEN.PRP handleselect) is one: its script polls the stage's
	// buttons and props, which the port supplies through its own UI.
	Builtins map[string]func(call *ScriptCall) (consumed int, status uint16, err error)
	// ProgramCounter is the record index of the statement being executed, the
	// value the native frame stores at +0x0C for diagnostics.
	ProgramCounter int
	forStack       []forEntry
	whileStack     []int
	runDepth       int
	// LastProgram and ProgramCounter locate the statement last executed, for
	// diagnostics.
	LastProgram *Program
}

// NewInterpreter builds an interpreter with fresh global state.
func NewInterpreter(host ScriptHost) *Interpreter {
	strings := &StringRegisters{}
	in := &Interpreter{Global: NewVariableTable(), Strings: strings, Host: host}
	in.Expressions = &ExpressionState{
		Strings:           strings,
		AvailableBytes:    func() int32 { return 0x10000 },
		SetProgramCounter: func(index int) { in.ProgramCounter = index },
	}
	return in
}

var ErrScriptValueKind = errors.New("script value has an unknown type")

// Value decodes a record, releasing its string register.
func (in *Interpreter) Value(record Record) (ScriptValue, uint16, error) {
	switch record.Kind {
	case 2, 4:
		return ScriptValue{Kind: record.Kind, Int: int32(record.Data)}, 0, nil
	case 3:
		pascal, status, err := in.Strings.Load(record)
		if err != nil || status != 0 {
			return ScriptValue{}, status, err
		}
		return ScriptValue{Kind: 3, Text: string(pascal[1:])}, 0, nil
	}
	return ScriptValue{}, 0, fmt.Errorf("%w: %d", ErrScriptValueKind, record.Kind)
}

// Record encodes a value, storing a string in a register.
func (in *Interpreter) Record(value ScriptValue) (Record, uint16, error) {
	switch value.Kind {
	case 2, 4:
		return Record{Kind: value.Kind, Data: uint32(value.Int)}, 0, nil
	case 3:
		if len(value.Text) > 0xff {
			return Record{}, 0x1a, nil
		}
		return in.Strings.Store(append([]byte{byte(len(value.Text))}, value.Text...))
	}
	return Record{}, 0, fmt.Errorf("%w: %d", ErrScriptValueKind, value.Kind)
}

type scriptContext struct {
	in    *Interpreter
	chain []ScriptFrame
	frame int
}

func (in *Interpreter) evaluate(chain []ScriptFrame, frame int, locals *VariableTable, program *Program, index int) (Record, int, uint16, error) {
	context := &scriptContext{in: in, chain: chain, frame: frame}
	parser := ExpressionValueParser{
		Context: context,
		Local:   locals,
		Global:  in.Global,
		State:   in.Expressions,
		Strings: in.Strings,
		Services: ExpressionAtomServices{
			ReadContextPascal: readFrameContextPascal,
			CallBlock:         in.callBlockValue,
			DispatchValue:     in.dispatchValue,
		},
	}
	value, consumed, status, err := in.Expressions.Evaluate(*program, index, parser.Parse)
	return value, int(consumed), uint16(status), err
}

func readFrameContextPascal(context any, offset int) ([]byte, error) {
	ctx, ok := context.(*scriptContext)
	if !ok {
		return nil, fmt.Errorf("script context has type %T", context)
	}
	frame := ctx.chain[ctx.frame]
	text := frame.Me
	if offset == 62 {
		text = frame.Target
	}
	if len(text) > 15 {
		return nil, fmt.Errorf("script context string %q exceeds its native field", text)
	}
	return append([]byte{byte(len(text))}, text...), nil
}

// callBlockValue is FUN_00422870's user-call branch: FUN_0041C890 with a
// return slot, starting the search at the current frame.
func (in *Interpreter) callBlockValue(context any, program Program, start int, local, global *VariableTable, strings *StringRegisters) (Record, uint32, uint32, error) {
	ctx, ok := context.(*scriptContext)
	if !ok {
		return Record{}, 0, 0, fmt.Errorf("script context has type %T", context)
	}
	var result Record
	consumed, status, err := in.callCode(ctx.chain, ctx.frame, ctx.chain, ctx.frame, local, &program, start, &result)
	if err != nil || status != 0 {
		return Record{}, 0, uint32(status), err
	}
	return result, uint32(consumed), 0, nil
}

func (in *Interpreter) dispatchValue(context any, program Program, start int, local, global *VariableTable, strings *StringRegisters) (Record, uint32, uint32, error) {
	ctx, ok := context.(*scriptContext)
	if !ok {
		return Record{}, 0, 0, fmt.Errorf("script context has type %T", context)
	}
	if in.Host == nil {
		return Record{}, 0, 0, fmt.Errorf("value opcode %d has no host", program.Records[start].Kind)
	}
	call := &ScriptCall{Interpreter: in, Chain: ctx.chain, FrameIndex: ctx.frame, Locals: local, Program: &program, Start: start}
	value, consumed, status, err := in.Host.Value(call)
	return value, uint32(consumed), uint32(status), err
}

// Call runs `name(args)` from program at start against chain, with arguments
// evaluated in the caller's frame and locals. It is FUN_0041C890 without a
// return slot, the form statements and sendto handlers use.
func (in *Interpreter) Call(chain []ScriptFrame, caller []ScriptFrame, callerFrame int, callerLocals *VariableTable, program *Program, start int) (int, uint16, error) {
	return in.callCode(chain, 0, caller, callerFrame, callerLocals, program, start, nil)
}

// callCode is FUN_0041C890. It measures the call through its closing
// parenthesis, then searches the chain from frame onward. A frame without the
// block, or whose block passes, hands the call to the next frame; the chain's
// last frame ends the search. A call nobody handles is status 4 unless its
// name is one of the named event boundaries (FUN_0041DC00), which may go
// unhandled.
func (in *Interpreter) callCode(chain []ScriptFrame, frame int, caller []ScriptFrame, callerFrame int, callerLocals *VariableTable, program *Program, start int, result *Record) (int, uint16, error) {
	depth := 0
	consumed := -1
	for index := start; index < len(program.Records); index++ {
		kind := program.Records[index].Kind
		if kind == opOpen {
			depth++
		}
		if kind == opClose {
			depth--
			if depth < 1 {
				consumed = index + 1 - start
				break
			}
		}
		if kind == opLine || kind == 0 {
			return 0, ScriptStatusMalformed, nil
		}
	}
	if consumed < 0 {
		return 0, ScriptStatusMalformed, nil
	}
	if name, status, err := identifierAt(*program, start); err == nil && status == 0 {
		if builtin, ok := in.Builtins[strings.ToLower(string(name[1:]))]; ok {
			call := &ScriptCall{Interpreter: in, Chain: caller, FrameIndex: callerFrame, Locals: callerLocals, Program: program, Start: start}
			_, status, err := builtin(call)
			return consumed, status, err
		}
	}
	for ; frame < len(chain); frame++ {
		status, err := in.searchFrame(chain, frame, caller, callerFrame, callerLocals, program, start, result)
		if err != nil {
			return consumed, 0, err
		}
		if status == 0 {
			return consumed, 0, nil
		}
		if status == ScriptStatusPassed {
			if !chain[frame].Last {
				status = 0
			}
			boundary, err := ResolveNamedBoundary(program, start)
			if err != nil {
				return consumed, 0, err
			}
			if boundary != 0 {
				status = 0
			}
		}
		if status != 0 {
			return consumed, status, nil
		}
		if chain[frame].Last {
			return consumed, 0, nil
		}
	}
	return consumed, ScriptStatusPassed, nil
}

// searchFrame is FUN_0041C9A0: walk the frame's code blocks, bind and run the
// first whose name matches.
func (in *Interpreter) searchFrame(chain []ScriptFrame, frame int, caller []ScriptFrame, callerFrame int, callerLocals *VariableTable, program *Program, start int, result *Record) (uint16, error) {
	script := chain[frame].Program
	if script == nil {
		return ScriptStatusPassed, nil
	}
	block, found := FindCodeMarker(script.Records)
	if !found {
		return ScriptStatusPassed, nil
	}
	locals := NewVariableTable()
	for {
		in.ProgramCounter = block
		body, status, err := in.matchBlock(script, block, locals, caller, callerFrame, callerLocals, program, start)
		if err != nil || status != 0 {
			return status, err
		}
		if body >= 0 {
			return in.execute(chain, frame, locals, script, body, result)
		}
		next, err := NextCodeOffset(script.Records, block)
		if err != nil {
			return 0, err
		}
		if next < 0 {
			return ScriptStatusPassed, nil
		}
		block += int(next)
	}
}

// matchBlock is FUN_0041CA70. It compares the block's name with the call's,
// case-insensitively; on a match it binds each parameter in the new local
// table to the argument evaluated in the caller's context, and returns the
// index of the block's first statement, past the header's line breaks.
func (in *Interpreter) matchBlock(script *Program, block int, locals *VariableTable, caller []ScriptFrame, callerFrame int, callerLocals *VariableTable, program *Program, start int) (int, uint16, error) {
	blockName, status, err := identifierAt(*script, block+1)
	if err != nil || status != 0 {
		return -1, status, err
	}
	callName, status, err := identifierAt(*program, start)
	if err != nil || status != 0 {
		return -1, status, err
	}
	if !equalPascalString(blockName, callName) {
		return -1, 0, nil
	}
	if KindAt(*script, block+2) != opOpen || KindAt(*program, start+1) != opOpen {
		return -1, ScriptStatusMalformed, nil
	}
	parameter := block + 3
	argument := start + 2
	finish := func(afterClose int) (int, uint16, error) {
		for KindAt(*script, afterClose) == opLine {
			afterClose++
		}
		if KindAt(*program, argument) != opClose {
			return -1, ScriptStatusMalformed, nil
		}
		return afterClose, 0, nil
	}
	if KindAt(*script, parameter) == opClose {
		return finish(parameter + 1)
	}
	for {
		name, status, err := identifierAt(*script, parameter)
		if err != nil || status != 0 {
			return -1, status, err
		}
		id, status, err := locals.ResolveOrCreate(name, &script.Records[parameter])
		if err != nil || status != 0 {
			return -1, status, err
		}
		value, consumed, status, err := in.evaluate(caller, callerFrame, callerLocals, program, argument)
		if err != nil || status != 0 {
			return -1, status, err
		}
		argument += consumed
		var encoded ExpressionValue
		binary.LittleEndian.PutUint16(encoded[:2], value.Kind)
		binary.LittleEndian.PutUint32(encoded[2:6], value.Data)
		if status, err := locals.WriteValue(id, encoded, in.Strings); err != nil || status != 0 {
			return -1, status, err
		}
		separator := parameter + 1
		if KindAt(*script, separator) != opComma {
			if KindAt(*script, separator) != opClose {
				return -1, ScriptStatusMalformed, nil
			}
			return finish(separator + 1)
		}
		parameter += 2
		if KindAt(*program, argument) != opComma {
			return -1, ScriptStatusMissingComma, nil
		}
		argument++
	}
}

// execute is FUN_0041CC20, the statement loop of one block invocation.
// result is the caller's return slot; nil means the block was called as a
// statement or event.
func (in *Interpreter) execute(chain []ScriptFrame, frame int, locals *VariableTable, script *Program, pc int, result *Record) (uint16, error) {
	ifDepth, forDepth, whileDepth, switchDepth := 0, 0, 0, 0
	unwind := func() {
		in.forStack = in.forStack[:len(in.forStack)-forDepth]
		in.whileStack = in.whileStack[:len(in.whileStack)-whileDepth]
	}
	noResult := func() uint16 {
		if result == nil {
			return 0
		}
		return ScriptStatusNoResult
	}
	records := script.Records
	for {
		if pc < 0 || pc >= len(records) {
			return 0, fmt.Errorf("statement index %d is outside the script", pc)
		}
		in.ProgramCounter, in.LastProgram = pc, script
		kind := records[pc].Kind
		advance := 0
		switch kind {
		case opGlobal, opLocal:
			table := locals
			if kind == opGlobal {
				table = in.Global
			}
			consumed, status, err := ResolveVariableList(script, pc, table)
			if err != nil || status != 0 {
				return status, err
			}
			advance = int(consumed)
		case opEndCode:
			switch {
			case ifDepth != 0:
				return ScriptStatusUnbalancedIf, nil
			case forDepth != 0:
				return ScriptStatusUnbalancedFor, nil
			case whileDepth != 0:
				return ScriptStatusUnbalancedWhile, nil
			case switchDepth != 0:
				return ScriptStatusUnbalancedSwitch, nil
			}
			return noResult(), nil
		case opExitCode:
			unwind()
			return noResult(), nil
		case opPassCode:
			unwind()
			if result == nil {
				return ScriptStatusPassed, nil
			}
			return ScriptStatusNoResult, nil
		case opReturn:
			if result == nil {
				return ScriptStatusReturnInEvent, nil
			}
			value, _, status, err := in.evaluate(chain, frame, locals, script, pc+1)
			if err != nil || status != 0 {
				return status, err
			}
			*result = value
			unwind()
			return 0, nil
		case opIf:
			condition, consumed, status, err := in.evaluate(chain, frame, locals, script, pc+1)
			if err != nil || status != 0 {
				return status, err
			}
			if condition.Kind != 2 {
				return ScriptStatusWrongType, nil
			}
			pc += 1 + consumed
			if condition.Data != 0 {
				ifDepth++
				break
			}
			offset, status, err := FindConditionalBranch(records, pc)
			if err != nil || status != 0 {
				return status, err
			}
			if offset >= 0 {
				ifDepth++
				advance = int(offset)
				break
			}
			offset, status, err = FindConditionalEnd(records, pc)
			if err != nil || status != 0 {
				return status, err
			}
			advance = int(offset)
		case opEndIf:
			advance = 1
			if ifDepth < 1 {
				return ScriptStatusUnbalancedIf, nil
			}
			ifDepth--
		case opElse:
			pc++
			if ifDepth < 1 {
				return ScriptStatusUnbalancedIf, nil
			}
			ifDepth--
			offset, status, err := FindConditionalEnd(records, pc)
			if err != nil || status != 0 {
				return status, err
			}
			advance = int(offset)
		case opSwitch:
			value, consumed, status, err := in.evaluate(chain, frame, locals, script, pc+1)
			if err != nil || status != 0 {
				return status, err
			}
			if value.Kind != 4 && value.Kind != 3 {
				return ScriptStatusWrongType, nil
			}
			pc += 1 + consumed
			parser := in.parserFor(chain, frame, locals)
			offset, status, err := FindSwitchCase(*script, pc, value, in.Expressions, parser.Parse, in.Strings)
			if err != nil || status != 0 {
				return status, err
			}
			if offset < 0 {
				offset, status, err = FindNestedTerminator(records, pc, opSwitch, opEndSwitch)
				if err != nil || status != 0 {
					return status, err
				}
				advance = int(offset)
				break
			}
			switchDepth++
			advance = int(offset)
		case opEndSwitch:
			advance = 1
			if switchDepth < 1 {
				return ScriptStatusUnbalancedSwitch, nil
			}
			switchDepth--
		case opCase:
			if switchDepth < 1 {
				return ScriptStatusUnbalancedSwitch, nil
			}
			switchDepth--
			offset, status, err := FindNestedTerminator(records, pc, opSwitch, opEndSwitch)
			if err != nil || status != 0 {
				return status, err
			}
			advance = int(offset)
		case opFor:
			entry, next, status, err := in.beginFor(chain, frame, locals, script, pc)
			if err != nil || status != 0 {
				return status, err
			}
			if entry == nil {
				offset, status, err := FindNestedTerminator(records, next, opFor, opEndFor)
				if err != nil || status != 0 {
					return status, err
				}
				pc = next
				advance = int(offset)
				break
			}
			forDepth++
			in.forStack = append(in.forStack, *entry)
			if len(in.forStack) > nativeLoopStackDepth-1 {
				return ScriptStatusStackOverflow, nil
			}
			pc = next
		case opEndFor:
			pc++
			if forDepth < 1 || len(in.forStack) < 1 {
				return ScriptStatusUnbalancedFor, nil
			}
			top := in.forStack[len(in.forStack)-1]
			current, status, err := top.locals.ReadValue(top.variable, in.Strings)
			if err != nil || status != 0 {
				return status, err
			}
			if current.Kind != 4 {
				return ScriptStatusWrongType, nil
			}
			next := int32(current.Data) + top.step
			var encoded ExpressionValue
			binary.LittleEndian.PutUint16(encoded[:2], 4)
			binary.LittleEndian.PutUint32(encoded[2:6], uint32(next))
			if status, err := top.locals.WriteValue(top.variable, encoded, in.Strings); err != nil || status != 0 {
				return status, err
			}
			again := next <= top.end
			if top.step < 0 {
				again = top.end <= next
			}
			if again {
				pc = top.body
			} else {
				forDepth--
				in.forStack = in.forStack[:len(in.forStack)-1]
			}
		case opWhile:
			pc++
			condition, consumed, status, err := in.evaluate(chain, frame, locals, script, pc)
			if err != nil || status != 0 {
				return status, err
			}
			if condition.Kind != 2 {
				return ScriptStatusWrongType, nil
			}
			if condition.Data == 0 {
				offset, status, err := FindWhileEnd(records, pc)
				if err != nil || status != 0 {
					return status, err
				}
				advance = int(offset)
				break
			}
			whileDepth++
			in.whileStack = append(in.whileStack, pc)
			if len(in.whileStack) > nativeLoopStackDepth-1 {
				return ScriptStatusStackOverflow, nil
			}
			advance = consumed
		case opEndWhile:
			pc++
			if whileDepth < 1 || len(in.whileStack) < 1 {
				return ScriptStatusUnbalancedWhile, nil
			}
			conditionStart := in.whileStack[len(in.whileStack)-1]
			condition, consumed, status, err := in.evaluate(chain, frame, locals, script, conditionStart)
			if err != nil || status != 0 {
				return status, err
			}
			if condition.Kind != 2 {
				return ScriptStatusWrongType, nil
			}
			if condition.Data == 0 {
				whileDepth--
				in.whileStack = in.whileStack[:len(in.whileStack)-1]
			} else {
				pc = conditionStart + consumed
			}
		case opDumpLocal, opDumpGlob:
			table := locals
			if kind == opDumpGlob {
				table = in.Global
			}
			consumed, status, err := RemoveVariableList(script, pc, table)
			if err != nil || status != 0 {
				return status, err
			}
			advance = int(consumed)
		default:
			if kind >= 12000 && kind <= 16054 {
				if in.Host == nil {
					return 0, fmt.Errorf("command opcode %d has no host", kind)
				}
				call := &ScriptCall{Interpreter: in, Chain: chain, FrameIndex: frame, Locals: locals, Program: script, Start: pc}
				consumed, status, err := in.Host.Command(call)
				if err != nil || status != 0 {
					return status, err
				}
				advance = consumed
				break
			}
			name, status, err := identifierAt(*script, pc)
			if err != nil || status != 0 {
				return status, err
			}
			if KindAt(*script, pc+1) == opOpen {
				if builtin, ok := in.Builtins[strings.ToLower(string(name[1:]))]; ok {
					call := &ScriptCall{Interpreter: in, Chain: chain, FrameIndex: frame, Locals: locals, Program: script, Start: pc}
					consumed, status, err := builtin(call)
					if err != nil || status != 0 {
						return status, err
					}
					advance = consumed
					break
				}
				consumed, status, err := in.callCode(chain, frame, chain, frame, locals, script, pc, nil)
				if err != nil || status != 0 {
					return status, err
				}
				advance = consumed
				break
			}
			table := locals
			id, status, err := table.Lookup(name, &records[pc])
			if err != nil {
				return 0, err
			}
			if status != 0 {
				table = in.Global
				id, status, err = table.Lookup(name, &records[pc])
				if err != nil || status != 0 {
					return status, err
				}
			}
			if KindAt(*script, pc+1) != opAssign {
				return ScriptStatusMissingEquals, nil
			}
			value, consumed, status, err := in.evaluate(chain, frame, locals, script, pc+2)
			if err != nil || status != 0 {
				return status, err
			}
			var encoded ExpressionValue
			binary.LittleEndian.PutUint16(encoded[:2], value.Kind)
			binary.LittleEndian.PutUint32(encoded[2:6], value.Data)
			if status, err := table.WriteValue(id, encoded, in.Strings); err != nil || status != 0 {
				return status, err
			}
			advance = 2 + consumed
		}
		pc += advance
		if KindAt(*script, pc) != opLine {
			return ScriptStatusLineExpected, nil
		}
		for KindAt(*script, pc) == opLine {
			pc++
		}
		if in.EscapeInterrupt != nil && in.EscapeInterrupt() {
			return ScriptStatusInterrupted, nil
		}
	}
}

func (in *Interpreter) parserFor(chain []ScriptFrame, frame int, locals *VariableTable) *ExpressionValueParser {
	return &ExpressionValueParser{
		Context: &scriptContext{in: in, chain: chain, frame: frame},
		Local:   locals,
		Global:  in.Global,
		State:   in.Expressions,
		Strings: in.Strings,
		Services: ExpressionAtomServices{
			ReadContextPascal: readFrameContextPascal,
			CallBlock:         in.callBlockValue,
			DispatchValue:     in.dispatchValue,
		},
	}
}

// beginFor is the 0xFAC branch of FUN_0041CC20:
//
//	for <local> = <start> to <end> [step <step>]
//
// The loop variable is always resolved in the local table. The start value is
// written before `to` is checked. The step defaults to 1. A loop whose range is
// already exhausted returns no entry and the caller skips to after endfor.
func (in *Interpreter) beginFor(chain []ScriptFrame, frame int, locals *VariableTable, script *Program, pc int) (*forEntry, int, uint16, error) {
	name, status, err := identifierAt(*script, pc+1)
	if err != nil || status != 0 {
		return nil, 0, status, err
	}
	id, status, err := locals.ResolveOrCreate(name, &script.Records[pc+1])
	if err != nil || status != 0 {
		return nil, 0, status, err
	}
	if KindAt(*script, pc+2) != opAssign {
		return nil, 0, ScriptStatusMissingEquals, nil
	}
	position := pc + 3
	first, consumed, status, err := in.evaluate(chain, frame, locals, script, position)
	if err != nil || status != 0 {
		return nil, 0, status, err
	}
	if first.Kind != 4 {
		return nil, 0, ScriptStatusWrongType, nil
	}
	position += consumed
	var encoded ExpressionValue
	binary.LittleEndian.PutUint16(encoded[:2], 4)
	binary.LittleEndian.PutUint32(encoded[2:6], first.Data)
	if status, err := locals.WriteValue(id, encoded, in.Strings); err != nil || status != 0 {
		return nil, 0, status, err
	}
	if KindAt(*script, position) != opTo {
		return nil, 0, ScriptStatusMissingTo, nil
	}
	last, consumed, status, err := in.evaluate(chain, frame, locals, script, position+1)
	if err != nil || status != 0 {
		return nil, 0, status, err
	}
	if last.Kind != 4 {
		return nil, 0, ScriptStatusWrongType, nil
	}
	position += 1 + consumed
	step := int32(1)
	if KindAt(*script, position) == opStep {
		value, consumed, status, err := in.evaluate(chain, frame, locals, script, position+1)
		if err != nil || status != 0 {
			return nil, 0, status, err
		}
		if value.Kind != 4 {
			return nil, 0, ScriptStatusWrongType, nil
		}
		step = int32(value.Data)
		position += 1 + consumed
	}
	start, end := int32(first.Data), int32(last.Data)
	if (step >= 1 && start > end) || (step < 0 && end > start) {
		return nil, position, 0, nil
	}
	return &forEntry{variable: id, locals: locals, end: end, step: step, body: position}, position, 0, nil
}

// RunSource compiles source text and sends it as the native System frame
// does (FUN_0040CA60): the text must be a complete statement such as
// `sendtoactor("jones", endwalk())` or a bare call `idle()` against chain.
func (in *Interpreter) RunSource(chain []ScriptFrame, source string) (uint16, error) {
	// A top-level run starts from empty expression and string state, as each
	// native message dispatch does; a nested run (a command that sends a
	// message) shares its caller's.
	if in.runDepth == 0 {
		in.Expressions.Top = 0
		in.Strings.Reset()
		in.forStack, in.whileStack = in.forStack[:0], in.whileStack[:0]
	}
	in.runDepth++
	defer func() { in.runDepth-- }()
	program, err := CompileText([]byte(source))
	if err != nil {
		return 0, err
	}
	system := []ScriptFrame{{Program: &program, Me: "System", Target: "System", Label: "Boot Message: ", Last: true}}
	if len(program.Records) == 0 {
		return 0, fmt.Errorf("source %q compiled to nothing", source)
	}
	kind := program.Records[0].Kind
	if kind >= 12000 && kind <= 16054 {
		if in.Host == nil {
			return 0, fmt.Errorf("command opcode %d has no host", kind)
		}
		call := &ScriptCall{Interpreter: in, Chain: system, Locals: NewVariableTable(), Program: &program, Start: 0}
		_, status, err := in.Host.Command(call)
		return status, err
	}
	_, status, err := in.Call(chain, system, 0, NewVariableTable(), &program, 0)
	return status, err
}

// SetGlobalNumber declares a global if needed and stores a number in it, the
// way an engine-owned value such as day or clock is published to scripts.
// SetGlobalBool declares a global holding a script boolean (kind 2).
func (in *Interpreter) SetGlobalBool(name string, value bool) error {
	pascal := append([]byte{byte(len(name))}, name...)
	id, status, err := in.Global.ResolveOrCreate(pascal, &Record{})
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("global %q cannot be declared: status %#x", name, status)
	}
	var encoded ExpressionValue
	binary.LittleEndian.PutUint16(encoded[:2], 2)
	binary.LittleEndian.PutUint32(encoded[2:6], boolWord(value))
	if status, err = in.Global.WriteValue(id, encoded, in.Strings); err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("global %q cannot be written: status %#x", name, status)
	}
	return nil
}

func (in *Interpreter) SetGlobalNumber(name string, value int32) error {
	pascal := append([]byte{byte(len(name))}, name...)
	id, status, err := in.Global.ResolveOrCreate(pascal, &Record{})
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("global %q cannot be declared: status %#x", name, status)
	}
	var encoded ExpressionValue
	binary.LittleEndian.PutUint16(encoded[:2], 4)
	binary.LittleEndian.PutUint32(encoded[2:6], uint32(value))
	status, err = in.Global.WriteValue(id, encoded, in.Strings)
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("global %q cannot be written: status %#x", name, status)
	}
	return nil
}

// SendParts reads a sendto-shaped call, `op(target, message(args))`, without
// running the message: it evaluates the target and returns it with the
// message's name and the records the whole call spans.
func (c *ScriptCall) SendParts() (target ScriptValue, message string, consumed int, status uint16, err error) {
	if c.Kind(c.Start+1) != opOpen {
		return ScriptValue{}, "", 0, ScriptStatusMalformed, nil
	}
	record, used, status, err := c.Eval(c.Start + 2)
	if err != nil || status != 0 {
		return ScriptValue{}, "", 0, status, err
	}
	target, status, err = c.Interpreter.Value(record)
	if err != nil || status != 0 {
		return ScriptValue{}, "", 0, status, err
	}
	at := c.Start + 2 + used
	if c.Kind(at) != opComma {
		return ScriptValue{}, "", 0, ScriptStatusMissingComma, nil
	}
	name, status, err := identifierAt(*c.Program, at+1)
	if err != nil || status != 0 {
		return ScriptValue{}, "", 0, status, err
	}
	end, status, err := ParenthesizedBlockScan(c.Program.Records, c.Start+1)
	if err != nil || status != 0 {
		return ScriptValue{}, "", 0, status, err
	}
	return target, string(name[1:]), end - c.Start, 0, nil
}

// SetGlobalString declares a global if needed and stores a string in it.
func (in *Interpreter) SetGlobalString(name, value string) error {
	pascal := append([]byte{byte(len(name))}, name...)
	id, status, err := in.Global.ResolveOrCreate(pascal, &Record{})
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("global %q cannot be declared: status %#x", name, status)
	}
	record, status, err := in.Record(ScriptValue{Kind: 3, Text: value})
	if err != nil || status != 0 {
		return fmt.Errorf("global %q string: status %#x %v", name, status, err)
	}
	var encoded ExpressionValue
	binary.LittleEndian.PutUint16(encoded[:2], record.Kind)
	binary.LittleEndian.PutUint32(encoded[2:6], record.Data)
	status, err = in.Global.WriteValue(id, encoded, in.Strings)
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("global %q cannot be written: status %#x", name, status)
	}
	return nil
}

// GlobalValue reads a global's value; ok is false when it was never declared.
func (in *Interpreter) GlobalValue(name string) (ScriptValue, bool, error) {
	pascal := append([]byte{byte(len(name))}, name...)
	id, status, err := in.Global.Lookup(pascal, &Record{})
	if err != nil || status != 0 {
		return ScriptValue{}, false, err
	}
	record, status, err := in.Global.ReadValue(id, in.Strings)
	if err != nil || status != 0 {
		return ScriptValue{}, false, err
	}
	value, status, err := in.Value(record)
	if err != nil || status != 0 {
		return ScriptValue{}, false, err
	}
	return value, true, nil
}

// GlobalVariable is the persisted form of one script global.
type GlobalVariable struct {
	Name string `json:"name"`
	Kind uint16 `json:"kind"`
	Int  int32  `json:"int,omitempty"`
	Text string `json:"text,omitempty"`
}

// SnapshotGlobals returns every declared global, for saves.
func (in *Interpreter) SnapshotGlobals() ([]GlobalVariable, error) {
	var out []GlobalVariable
	for id := range in.Global.slots {
		slot := in.Global.slots[id]
		name := slot.pascalName()
		variable := GlobalVariable{Name: string(name[1:]), Kind: slot.ValueType()}
		record, status, err := in.Global.ReadValue(uint16(id), in.Strings)
		if err != nil || status != 0 {
			return nil, fmt.Errorf("read global %q: status %#x %v", variable.Name, status, err)
		}
		value, status, err := in.Value(record)
		if err != nil || status != 0 {
			return nil, fmt.Errorf("decode global %q: status %#x %v", variable.Name, status, err)
		}
		variable.Int, variable.Text = value.Int, value.Text
		out = append(out, variable)
	}
	return out, nil
}

// RestoreGlobals replaces the global table with saved values.
func (in *Interpreter) RestoreGlobals(variables []GlobalVariable) error {
	in.Global = NewVariableTable()
	for _, variable := range variables {
		switch variable.Kind {
		case 3:
			if err := in.SetGlobalString(variable.Name, variable.Text); err != nil {
				return err
			}
		case 2:
			if err := in.SetGlobalNumber(variable.Name, variable.Int); err != nil {
				return err
			}
			id, _, _ := in.Global.Lookup(append([]byte{byte(len(variable.Name))}, variable.Name...), &Record{})
			var encoded ExpressionValue
			binary.LittleEndian.PutUint16(encoded[:2], 2)
			binary.LittleEndian.PutUint32(encoded[2:6], uint32(variable.Int))
			if status, err := in.Global.WriteValue(id, encoded, in.Strings); err != nil || status != 0 {
				return fmt.Errorf("restore global %q: status %#x %v", variable.Name, status, err)
			}
		default:
			if err := in.SetGlobalNumber(variable.Name, variable.Int); err != nil {
				return err
			}
		}
	}
	return nil
}

// Site renders the statement last executed, with its enclosing code block name.
func (in *Interpreter) Site() string {
	program := in.LastProgram
	if program == nil || in.ProgramCounter < 0 || in.ProgramCounter >= len(program.Records) {
		return "?"
	}
	block := "?"
	for i := in.ProgramCounter; i >= 0; i-- {
		if program.Records[i].Kind == opCode && i+1 < len(program.Records) {
			if name, err := program.IdentifierPascal(i + 1); err == nil {
				block = string(name[1:])
			}
			break
		}
	}
	end := in.ProgramCounter
	for end < len(program.Records) && program.Records[end].Kind != opLine && program.Records[end].Kind != 0 {
		end++
	}
	statement := FormatProgram(Program{Records: append(append([]Record(nil), program.Records[in.ProgramCounter:end]...), Record{}), StringPool: program.StringPool})
	_ = statement
	var parts []string
	for i := in.ProgramCounter; i < end; i++ {
		one := FormatProgram(Program{Records: []Record{program.Records[i], {}}, StringPool: nil})
		if program.Records[i].Kind == 3 || program.Records[i].Kind == 5 {
			if text, err := program.LiteralPascal(i); err == nil {
				one = "\"" + string(text[1:]) + "\""
			} else if id, err := program.IdentifierPascal(i); err == nil {
				one = string(id[1:])
			}
		}
		parts = append(parts, one)
	}
	return block + ": " + strings.Join(parts, " ")
}
