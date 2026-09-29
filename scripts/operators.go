package scripts

import "errors"

// ErrWrongOperandType is the Go-side error accompanying StatusWrongOperandType,
// the status the native handlers return for an argument of the wrong type. The
// native code carries only the status; carrying an error too lets a caller report
// which operand was wrong without re-inspecting the record.
var ErrWrongOperandType = errors.New("operand is not of the required type")

// The operator set, from live Ghidra decompilation of FUN_004220A0, the shared
// argument evaluator, FUN_004227E0, which returns an operator's precedence, and
// FUN_00422870, which evaluates a single operand.
//
// Three separate facts come out of these, and they are easy to conflate:
//
//  1. The scanner's operator RANGE is kinds 8000..8014 inclusive, fifteen slots.
//     FUN_004220A0 decides whether the record after a value continues the
//     expression with exactly this test:
//
//	if (*next < 8000) || (8014 < *next) -> not an operator, finish
//
//  2. Only 8001..8013 have a PRECEDENCE. FUN_004227E0 is a switch with thirteen
//     cases; 8000 and 8014 are not among them, so they fall to the default,
//     which raises FUN_0042C470(0, 0x106A) and returns 0xFFFF.
//
//  3. Therefore 8000 and 8014 are RESERVED operator slots. The scanner treats
//     them as operators, so an expression containing one keeps scanning, but
//     asking for its precedence is a native diagnostic rather than a level.
//     That is exactly why the Go evaluator reports 0xFFFF for an unmapped
//     operator: it is the verified return value, not a placeholder.
//
// The seven precedence levels and the thirteen mapped operators are reproduced
// below as transcribed. FUN_004227E0 confirms the existing ExpressionPrecedence
// table value for value, so that function is left as the single source of truth
// and OperatorPrecedence wraps it.

// The scanner's operator range, inclusive on both ends.
const (
	// OperatorKindFirst is 0x1F40, the lowest kind the evaluator treats as an
	// operator and continues the expression over.
	OperatorKindFirst uint16 = 8000
	// OperatorKindLast is 0x1F4E, the highest. Any kind above this ends the
	// expression, as does any kind below OperatorKindFirst.
	OperatorKindLast uint16 = 8014
	// OperatorKindFirstMapped is 0x1F41, the lowest kind with a precedence.
	OperatorKindFirstMapped uint16 = 8001
	// OperatorKindLastMapped is 0x1F4D, the highest kind with a precedence.
	OperatorKindLastMapped uint16 = 8013
	// OperatorKindsTotal is the number of slots in the scanner's range.
	OperatorKindsTotal = OperatorKindLast - OperatorKindFirst + 1
	// OperatorKindsMapped is the number of those that have a precedence.
	OperatorKindsMapped = OperatorKindLastMapped - OperatorKindFirstMapped + 1
	// OperatorKindsReserved is how many fall to FUN_004227E0's default, which
	// are the two ends of the range.
	OperatorKindsReserved = OperatorKindsTotal - OperatorKindsMapped
)

// IsOperatorKind reports whether kind continues the expression as an operator,
// reproducing the scanner test in FUN_004220A0. Note this is true for the two
// reserved kinds 8000 and 8014 as well, which is deliberate: the reference's
// range check does not exclude them.
func IsOperatorKind(kind uint16) bool {
	return kind >= OperatorKindFirst && kind <= OperatorKindLast
}

// OperatorPrecedenceStatusUnmapped is the value FUN_004227E0 returns for a kind
// outside its thirteen cases, alongside raising the diagnostic. It is a distinct
// status rather than a precedence level.
const OperatorPrecedenceStatusUnmapped uint16 = 0xFFFF

// OperatorPrecedenceDiag is the second argument of the
// FUN_0042C470(0, 0x106A) raised for an unmapped operator kind. Being a
// diagnostic, it is not a recoverable status, which is why the Go port reports
// it as a distinct result rather than folding it into a level.
const OperatorPrecedenceDiag uint16 = 0x106A

// ExpressionPrecedenceLevels is the verified number of precedence levels.
// FUN_004220A0 loops `for (level = 0; level < 7; level++)`, so the count is
// exactly seven and not a coincidence of the operators present.
const ExpressionPrecedenceLevels uint8 = 7

// ValueStackDepth is the verified depth of the evaluator's value stack.
// FUN_004220A0 raises status 3 once the depth exceeds 0x27, that is 39, so there
// are forty slots with valid indices 0 through 39.
const ValueStackDepth = 0x27 + 1

// StatusValueStackOverflow is native status 3, returned when pushing would take
// the value stack past its 0x27 depth limit.
const StatusValueStackOverflow uint16 = 3

// StatusNativeStackLow is native status 0x2C, returned by the first thing
// FUN_004220A0 does: it queries the remaining C stack and refuses to evaluate
// when less than 0x800 bytes remain.
//
// The port DOES enforce this, by injecting the probe. ExpressionState carries an
// AvailableBytes func() int32 and Evaluate refuses with 0x2C when it reports
// less than the headroom, so the native guard is reproduced rather than dropped.
// A goroutine stack cannot be queried the way the native queries the C stack, so
// the value is supplied by the caller instead of read directly.
const StatusNativeStackLow uint16 = 0x2C

// NativeStackHeadroomBytes is the 0x800 the native evaluator demands.
const NativeStackHeadroomBytes = 0x800

// OperatorPrecedence returns an operator's verified precedence level and whether
// the kind has one. A kind inside the scanner's range but outside
// FUN_004227E0's cases, which is to say 8000 and 8014, reports false and the
// status FUN_004227E0 returns for it, so a caller can distinguish "not an
// operator" from "an operator the reference refuses to give a level for".
func OperatorPrecedence(kind uint16) (level uint8, status uint16, ok bool) {
	if !IsOperatorKind(kind) {
		return 0, 0, false
	}
	if precedence, mapped := ExpressionPrecedence(kind); mapped {
		return precedence, 0, true
	}
	return 0, OperatorPrecedenceStatusUnmapped, false
}

// operand kinds, from FUN_00422870, which evaluates one operand. It is a chain
// of range tests, and only these four cases produce a value without recursing
// through FUN_004220A0 for a nested expression.
const (
	// ValueKindOpenParen is 0x0FB2, the "(" token. FUN_00422870 recurses into
	// the parenthesised expression and then requires the next record to be
	// ValueKindCloseParen, returning status 2 when it is not.
	ValueKindOpenParen uint16 = 0x0FB2
	// ValueKindCloseParen is 0x0FB3, the ")" token.
	ValueKindCloseParen uint16 = 0x0FB3
	// ValueKindStringLiteral is 3, a string constant. The reference resolves it
	// with FUN_00422070 and stores it in a string register with FUN_00421F60,
	// consuming one record.
	ValueKindStringLiteral uint16 = 3
	// ValueKindNumericLiteral is 4, a numeric constant, copied straight through
	// with its result type set to 4 and one record consumed.
	ValueKindNumericLiteral uint16 = 4
	// ValueKindUnaryMinus is 0x1F42, 8002, which in operand position is unary
	// negation rather than subtraction. The reference recurses for the operand
	// and, when the result is type 4, negates its value.
	ValueKindUnaryMinus uint16 = 0x1F42
)

// StatusMismatchedParen is native status 2, returned by FUN_00422870 when the
// record after a parenthesised expression is not ")". It is the same status
// FUN_00424890 uses for a malformed command call frame, so the two share a code.
const StatusMismatchedParen uint16 = 2

// Operand result statuses, from FUN_004222E0, which applies one binary operator
// to the value stack. It takes the stack index of the operator and the operator
// kind, reads the operand to the left at that index and the operand to the right
// at the next index, and switches on `kind - 0x1F41`.
const (
	// StatusDivideByZero is native status 0x37, returned by the 8004 case when
	// the right operand is zero. It is checked AFTER both operands have been
	// confirmed type 4, so a non-numeric divisor gives 0x0E rather than 0x37.
	StatusDivideByZero uint16 = 0x37
	// StatusConcatOverflow is native status 0x1A, returned by the 8007 string
	// concatenation case when the combined length would exceed the 255-byte
	// Pascal cap.
	StatusConcatOverflow uint16 = 0x1A
	// StatusWrongOperandType is 0x0E, declared alongside the value-result
	// protocol in values.go and used here too: FUN_004222E0's arithmetic and
	// logical cases both return it, the same status the value builtins return
	// for a bad argument.
	//
	// OperatorArithmeticFirst is 0x1F41, 8001, the lowest operator kind
	// FUN_004222E0's switch handles, and the base its cases are numbered from.
	OperatorArithmeticFirst uint16 = 8001
)

// Verified operand type requirements per operator, from FUN_004222E0's cases.
//
// The four arithmetic operators 8001 `+`, 8002 `-`, 8003 `*` and 8004 `/` each
// test that the left kind is 4 and the right kind is 4, returning 0x0E
// otherwise. The two logical operators 8005 `&` and 8006 `|` each test that
// BOTH kinds are 2, not 4, so a numeric operand to a logical operator is a
// type error rather than a truthiness conversion. That is the part a port is
// most likely to "fix" by treating nonzero as true, and doing so would accept
// programs the reference rejects.
const (
	// OperatorTypeArithmetic is the required operand kind for 8001..8004.
	OperatorTypeArithmetic uint16 = 4
	// OperatorTypeLogical is the required operand kind for 8005 and 8006.
	OperatorTypeLogical uint16 = 2
)

// StringLiteralOffsetBase documents the addressing of a type-3 literal, from
// FUN_00422070:
//
//	if (*record != 3) return 0x0E;
//	FUN_0042E6C0(*(int *)(record + 1) + (int)record, out)
//
// A string literal's data word is therefore a RELATIVE offset from the start of
// its own record to the Pascal text, not an absolute address, an index into a
// table, or inline text. The reference adds the offset to the record's own
// address. The Go port models the same relationship against a pool that follows
// the record stream, which is why Program.pascalRecord subtracts the records'
// combined size; the two agree whenever the pool directly follows the records,
// which is the shipped layout.
const StringLiteralOffsetBase = "record address + data word"

// UnaryMinus applies the verified unary negation from FUN_00422870: the operand
// is negated only when it is type 4, and the result keeps the operand's type and
// tail. A non-numeric operand yields the wrong-operand-type status instead,
// matching the guard the binary operators use.
//
// The negation is on the full 32-bit data word, not a 16-bit field, which is why
// the conversion goes through int32 rather than int16.
func UnaryMinus(operand Record) (Record, uint16, error) {
	if operand.Kind != ValueTypeNumeric {
		return Record{}, StatusWrongOperandType, ErrWrongOperandType
	}
	// The negation is on the full 32-bit data word, so it is computed as a
	// signed int32 and reinterpreted, which is how a 32-bit two's complement
	// negation wraps. Computing it in unsigned space would not wrap.
	negated := -int32(operand.Data)
	return Record{Kind: operand.Kind, Data: uint32(negated), Tail: operand.Tail}, 0, nil
}
