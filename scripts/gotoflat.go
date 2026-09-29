package scripts

import (
	"fmt"
	"strings"
)

// Verified gotoflat (opcode 12062) argument resolution, from the live Ghidra
// decompilation of FUN_00411AD0 (DF386.EXE, preferred base 0x00400000):
//
//	if (DAT_00459A00 == 0) return 0x0B;
//	uVar2 = FUN_004220A0(param_1, param_2, param_3, &local_8, param_4);
//	if ((short)uVar2 == 0) {
//	    if (local_8 != 4) {                     // result type is not numeric
//	        uVar2 = FUN_00421FC0(&local_8, &DAT_00459AD0);
//	        if ((short)uVar2 != 0) return uVar2;
//	        local_6 = FUN_00413590(&DAT_00459AD0);
//	        if (local_6 < 0) return 10;
//	        local_6 = local_6 + 1;
//	    }
//	    iVar3 = *(int *)(DAT_00459A04 + 0xD34);
//	    uVar1 = FUN_00411C80();                  // perform the flat switch
//	    ...
//	}
//
// Two verified facts matter for the port:
//
//   - A numeric argument is used directly as a ONE-based flat index.
//   - A string argument is resolved to a ZERO-based index by a name lookup
//     (FUN_00413590) and only then incremented, so the two paths converge on
//     the same one-based value. A name that does not resolve is native status 10.
//
// The asymmetry is why NEW.FLT resource 24 can capture the current flat with
// `arg = currentflat()` and later return with `gotoflat(arg)`: currentflat
// yields the flat's name, and the name lookup routes it back to the same flat.
const (
	// GotoflatStatusNoContext is native status 0x0B, returned when no flat or
	// stage context is active.
	GotoflatStatusNoContext uint16 = 0x0B
	// GotoflatStatusUnknownFlat is native status 10, returned when a flat name
	// does not resolve.
	GotoflatStatusUnknownFlat uint16 = 10
)

// GotoflatValue is one evaluated gotoflat argument.
type GotoflatValue struct {
	// Numeric is used when Type is 4, the native numeric record kind.
	Numeric int32
	// Name is used when Type is 3, the native string record kind.
	Name string
	// Type is the evaluated value's record kind.
	Type uint16
}

// FlatNameLookup resolves a flat name to its zero-based index, mirroring
// FUN_00413590. It returns an error when the name is unknown so the caller can
// emit native status 10.
type FlatNameLookup func(name string) (int, error)

// ErrUnknownFlat is returned by a FlatNameLookup when a name has no flat.
var ErrUnknownFlat = fmt.Errorf("flat name is unknown")

// errUnknownFlat is the package-local alias used by lookup implementations.
var errUnknownFlat = ErrUnknownFlat

// ResolveGotoflatTarget converts an evaluated gotoflat argument into the
// one-based flat index the native handler passes to the flat switch, and
// reports the native status.
//
// A nil lookup is only valid when the argument is numeric.
func ResolveGotoflatTarget(active bool, value GotoflatValue, lookup FlatNameLookup) (target int, status uint16, err error) {
	if !active {
		return -1, GotoflatStatusNoContext, nil
	}
	if value.Type == 4 {
		// Verified: the numeric path is already one-based and is used as-is.
		return int(value.Numeric), 0, nil
	}
	// Verified: only a non-numeric result takes the string path.
	if lookup == nil {
		return -1, GotoflatStatusUnknownFlat, fmt.Errorf("gotoflat name lookup is unavailable")
	}
	index, lookupErr := lookup(value.Name)
	if lookupErr != nil || index < 0 {
		return -1, GotoflatStatusUnknownFlat, nil
	}
	// Verified: the lookup result is zero-based and is incremented here.
	return index + 1, 0, nil
}

// StaticPascalNames are the fixed C strings the native engine resolves through
// FUN_00427FC0 and copies into child script contexts with FUN_0042E6C0.
// Values are read from the reference image.
//
//	0x0045D488  "Stage Script: "  injected by the sendto* handler FUN_00413230
//	0x0045D9D4  "SaveGame"        the save/load key used by FUN_00422C80
const (
	StaticNameStageScript = "Stage Script: "
	StaticNameSaveGame    = "SaveGame"
)

// PascalFromCString mirrors FUN_00427FC0: the reference stores these as NUL
// terminated C strings preceded by one NUL byte, and the native code copies the
// text into a length-prefixed Pascal buffer.
func PascalFromCString(value string) []byte {
	trimmed := strings.TrimRight(value, "\x00")
	if len(trimmed) > 255 {
		trimmed = trimmed[:255]
	}
	out := make([]byte, 0, len(trimmed)+1)
	out = append(out, byte(len(trimmed)))
	return append(out, trimmed...)
}
