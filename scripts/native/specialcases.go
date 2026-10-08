package native

import (
	. "redust/scripts"
	"fmt"
)

// The two opcodes handled outside the dispatchers' jump tables, plus the one
// builtin in the engine that is a genuine matched getter and setter.
//
// Each dispatcher special-cases exactly one opcode, and the two are different.
// Mapping them closes dispatch: 300 table cases plus these two is every opcode the
// engine can route.

// ActorVisible 16001 is the **only matched getter and setter pair in the engine**,
// and that is also why it is the command dispatcher's special case. Its setter
// takes two arguments, `name , value`, through the shared two-argument evaluator,
// while the value side's getter takes one. The uniform case blocks in the jump
// table all call the one-argument form, so the two-argument setter needed a block
// outside the table.
//
// This is worth contrasting with `currentdir` 16011, the previous entry's finding.
// That opcode looked like a getter and setter too, and turned out to be a getter
// and a setter of **different state**. `actorvisible` really is a pair, and the two
// bodies share both the resolver and the field.
//
// Setter, `FUN_0040B580`, handled at 0x004251E0 outside the command table:
//
//	FUN_00417070(..., &name, &value, &consumed);      // two arguments, comma required
//	FUN_00421FC0(&name, &DAT_00459AD0);               // name into the scratch
//	FUN_0040D730(&DAT_00459AD0, &actor);              // resolve the actor
//	if (value.Kind != 2) return 0x0E;                 // the value must be BOOLEAN
//	FUN_0040D800(&actor);                             // store the visibility
//
// Getter, `FUN_0040B620`, which is index 0 of the value dispatcher's primary table:
//
//	FUN_004220A0(..., &name, &consumed);              // one argument
//	FUN_00421FC0(&name, &DAT_00459AD0);
//	FUN_0040D730(&DAT_00459AD0, &actor);              // the same resolver
//	*out = 2;                                         // type 2, boolean
//	*(int *)(out + 1) = *(int *)actor;                // the actor's FIRST word, raw
//
// So the getter copies the field **raw**, with no normalisation, exactly as
// `propvisible` and `menuvisible` do. A field holding 7 yields a boolean 7.
const (
	// ActorVisibleOpcode is the hybrid-band opcode, 16001.
	ActorVisibleOpcode uint16 = 16001
	// ActorVisibleCommandHandler is the out-of-table setter, FUN_0040B580.
	ActorVisibleCommandHandler = "FUN_0040B580"
	// ActorVisibleValueHandler is the in-table getter, FUN_0040B620, at index 0 of
	// the value dispatcher's primary table.
	ActorVisibleValueHandler = "FUN_0040B620"
	// ActorVisibleResolver is the actor lookup both bodies call, FUN_0040D730.
	ActorVisibleResolver = "FUN_0040D730"
	// ActorVisibleStore is the setter's write, FUN_0040D800.
	ActorVisibleStore = "FUN_0040D800"
	// ActorVisibleCommandBlock is where the command dispatcher handles it, outside
	// the jump table.
	ActorVisibleCommandBlock uint32 = 0x004251E0
)

// ActorBlockSize is the buffer the resolver fills, 41 DWORDs. It is larger than an
// actor's visible state, since the same buffer serves the whole actor.
const ActorBlockSize = 41 * 4

// ActorState is the part of an actor block the visibility pair touches.
type ActorState struct {
	// Visible is the actor's first 16-bit field, which the getter reports raw and
	// the setter writes. A second 16-bit field follows it inside the same DWORD.
	Visible int32
	// Present reports whether the resolver found an actor at all.
	Present bool
}

// ResolveActorVisibility reproduces the shared resolver's contract: given whether
// the name matched an actor and that actor's first word, produce the getter's
// result. The field is copied raw.
func ResolveActorVisibility(actor ActorState) ValueResult {
	return ValueResult{Type: ValueTypeBool, Value: actor.Visible, Tail: 0}
}

// SetActorVisibility validates the setter's argument and returns the value to
// store. The argument must be a **boolean**, so a numeric argument is a type error
// rather than being coerced, and only the low 16 bits are kept because the field
// is 16 bits wide.
func SetActorVisibility(argument Record) (int32, uint16, error) {
	if argument.Kind != ValueTypeBool {
		return 0, StatusWrongOperandType, ErrWrongOperandType
	}
	return int32(int16(uint16(argument.Data))), 0, nil
}

// The setter takes the two-argument form, so the comma is required and its absence
// is the general 0x1C rather than anything specific to this opcode. A test asserts
// that, because it is the reason the opcode sits outside the jump table.
func (a ActorState) AcceptsTwoArgumentForm() bool { return true }

// VerifyActorVisibleIsOutOfTable documents why the command side is special-cased,
// and is checked by the test so the explanation cannot drift from the structure.
func VerifyActorVisibleIsOutOfTable() bool {
	// The command side has no table entry, because 16001 is its special opcode.
	if _, ok := CommandCommandHandler(ActorVisibleOpcode); ok {
		return false
	}
	// The value side does, at index 0 of its primary table.
	if !ValuePrimaryTable.Covers(ActorVisibleOpcode) {
		return false
	}
	// The two handlers are different functions, which is what a pair requires.
	command, _ := CommandCommandHandler(ActorVisibleOpcode)
	value, _ := CommandValueHandler(ActorVisibleOpcode)
	return command != value
}

// Random 20001 is the value dispatcher's special case, handled at 0x00413E7B. Its
// body, verified:
//
//	FUN_004220A0(..., &arg, &consumed);
//	if (arg.Kind != 4) return 0x0E;                  // the bound must be numeric
//	r = FUN_0042E7A0(arg.Value);
//	*out = 4;                                        // type 4, numeric
//	*(int *)(out + 1) = r;
//
// So `random` takes a numeric bound, returns a **type-4** result, and the bound is
// split into two halves and reassembled across the call, which is a compiler
// artifact of how the argument's data word is passed rather than anything about
// randomness. The generator itself is `FUN_0042E7A0`, which is unmapped, so the
// distribution is not established here; only the contract is.
//
// Note the asymmetry with the other special case: `actorvisible` is special on the
// command side and `random` on the value side, mirroring each other, and unlike
// `actorvisible` the command side has no `random` at all because 20001 is outside
// both command bands.
const (
	// RandomOpcode is the value-band opcode, 20001.
	RandomOpcode uint16 = 20001
	// RandomHandler is the out-of-table handler, FUN_00415C40.
	RandomHandler = "FUN_00415C40"
	// RandomGenerator is the number generator, FUN_0042E7A0, which is unmapped.
	RandomGenerator = "FUN_0042E7A0"
	// RandomValueBlock is where the value dispatcher handles it, outside the jump
	// table.
	RandomValueBlock uint32 = 0x00413E7B
)

// RandomResult reproduces `random`'s contract. The argument must be numeric, and
// the result is a type-4 record holding whatever the generator produced. generate
// is injected because the generator is unmapped, so the distribution is not claimed
// and only the type and status behaviour is.
func RandomResult(argument Record, generate func(bound int32) int32) (ValueResult, uint16, error) {
	if argument.Kind != ValueTypeNumeric {
		return ValueResult{}, StatusWrongOperandType, ErrWrongOperandType
	}
	if generate == nil {
		return ValueResult{}, 0, fmt.Errorf("the random generator is not available")
	}
	return NumericResult(generate(int32(argument.Data))), 0, nil
}
