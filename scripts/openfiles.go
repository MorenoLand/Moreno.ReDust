package scripts

import "fmt"

// The open*file family. `opencastfile` 12013, `opentrackfile` 12019 and
// `openshopfile` 12056 are one repeated pattern with three different record
// strides, transcribed from FUN_0040BFA0, FUN_0040E250 and FUN_00420110:
//
//	evaluate the name argument
//	FUN_00421FC0(result, &DAT_00459BD0)      // into the shared scratch
//	FUN_004178C0(&DAT_00459BD0)              // the stage-file name resolver
//	loader(&DAT_00459BD0, &record)            // parse the file into a record
//	count += 1;                              // the kind's count global
//	size = count * stride; if (size == 0) size = 1;
//	handle = GlobalReAlloc(handle, size, GMEM_MOVEABLE);
//	lock; copy the record to record[count - 1]; unlock
//
// Two details in that sequence are easy to lose. The `if (size == 0) size = 1`
// guard exists so the first append does not ask Windows to reallocate a block to
// zero bytes, which `GlobalReAlloc` rejects. And the record is written at index
// `count - 1` *after* the increment, so the count is the new length rather than
// the index just used.
//
// The cast and shop records are 0x1C, that is 28 bytes, which is the same stride
// as a flat table row. The track record is 0x26, that is 38 bytes.

// GMEMMoveable is the flag every GlobalReAlloc in this family passes, 0x0002,
// which is GMEM_MOVEABLE. The arrays are moveable global blocks locked around
// each access.
const GMEMMoveable uint32 = 0x0002

// OpenFileKind distinguishes the three members of the family.
type OpenFileKind uint8

const (
	// OpenCastFile is opencastfile 12013.
	OpenCastFile OpenFileKind = iota
	// OpenTrackFile is opentrackfile 12019.
	OpenTrackFile
	// OpenShopFile is openshopfile 12056.
	OpenShopFile
)

// OpenFileSpec describes one verified member of the family.
type OpenFileSpec struct {
	// Kind identifies the member.
	Kind OpenFileKind
	// Opcode is the command opcode.
	Opcode uint16
	// Name is the script keyword.
	Name string
	// Handler is the opcode's native handler.
	Handler string
	// Loader parses the named file into a record. It is the one part that
	// differs structurally between members.
	Loader string
	// CountSlot is the global holding the array's length.
	CountSlot string
	// HandleSlot is the global holding the allocated block.
	HandleSlot string
	// RecordStride is the per-record stride in bytes.
	RecordStride int
	// RecordDwords is how many DWORDs the append copies. The track member
	// copies nine and then a trailing short, which together are its 38 bytes;
	// the others copy seven and stop, which are their 28.
	RecordDwords int
	// TrailingShort records whether a short follows the DWORDs, as the track
	// member's does.
	TrailingShort bool
}

// OpenFileSpecs is the verified family.
var OpenFileSpecs = []OpenFileSpec{
	{OpenCastFile, 12013, "opencastfile", "FUN_0040BFA0", "FUN_0040C160", "DAT_004599E4", "DAT_004599E0", 0x1C, 7, false},
	{OpenTrackFile, 12019, "opentrackfile", "FUN_0040E250", "FUN_0040E340", "DAT_004599FC", "DAT_004599F8", 0x26, 9, true},
	{OpenShopFile, 12056, "openshopfile", "FUN_00420110", "FUN_004202E0", "DAT_004599F4", "DAT_004599F0", 0x1C, 7, false},
}

// OpenFileSpecFor returns the verified spec for an opcode.
func OpenFileSpecFor(opcode uint16) (OpenFileSpec, bool) {
	for _, spec := range OpenFileSpecs {
		if spec.Opcode == opcode {
			return spec, true
		}
	}
	return OpenFileSpec{}, false
}

// RecordSizeBytes is the record's total byte size, which is the copied DWORDs
// plus a trailing short where the member has one. It must equal RecordStride, and
// a test asserts that for all three, because a mismatch would mean the append
// copies a different number of bytes than the stride advances.
func (s OpenFileSpec) RecordSizeBytes() int {
	if s.TrailingShort {
		return s.RecordDwords*4 + 2
	}
	return s.RecordDwords * 4
}

// RequiredAllocation returns the block size the reference requests for a given
// new count, reproducing the increment-then-multiply-then-guard sequence:
//
//	count += 1
//	size = count * stride
//	if (size == 0) size = 1
//
// The guard only fires for a count of zero, which cannot happen after the
// increment unless the stride is zero, so in practice it exists to satisfy
// GlobalReAlloc. It is reproduced because a zero-size reallocation fails.
func RequiredAllocation(newCount, stride int) int {
	size := newCount * stride
	if size == 0 {
		return 1
	}
	return size
}

// AppendRecord adds a record to a growable array, returning the new count, the
// block size the reference would request and the byte offset of the new record.
// The offset is `count * stride` computed with the *pre-increment* count, which is
// what the reference's `base + count * stride + -stride` amounts to, so the first
// record lands at offset zero.
func AppendRecord(spec OpenFileSpec, count int, record []byte) (newCount int, size int, offset int, err error) {
	if spec.RecordStride <= 0 {
		return 0, 0, 0, fmt.Errorf("%s has a non-positive stride %d", spec.Name, spec.RecordStride)
	}
	want := spec.RecordSizeBytes()
	if len(record) < want {
		return 0, 0, 0, fmt.Errorf("%s record is %d bytes, want at least %d", spec.Name, len(record), want)
	}
	if count < 0 {
		return 0, 0, 0, fmt.Errorf("%s count is negative: %d", spec.Name, count)
	}
	offset = count * spec.RecordStride
	newCount = count + 1
	size = RequiredAllocation(newCount, spec.RecordStride)
	if offset+spec.RecordStride > size {
		return 0, 0, 0, fmt.Errorf("%s: the new record at +%d overruns the %d byte allocation",
			spec.Name, offset, size)
	}
	return newCount, size, offset, nil
}

// WriteRecord copies a record into a growable block at the given offset, matching
// the reference's `n - 1` DWORD loop plus its optional trailing short. It refuses
// a block that cannot hold the record rather than writing past the end, which the
// reference's own bounds it provides.
func WriteRecord(spec OpenFileSpec, block []byte, offset int, record []byte) error {
	if offset < 0 || offset+spec.RecordStride > len(block) {
		return fmt.Errorf("%s: record at +%d does not fit a %d byte block", spec.Name, offset, len(block))
	}
	dwords := spec.RecordDwords
	for i := 0; i < dwords; i++ {
		putUint32(block[offset+i*4:], record[i*4:])
	}
	if spec.TrailingShort {
		putUint16(block[offset+dwords*4:], record[dwords*4:])
	}
	return nil
}

func putUint32(dst []byte, src []byte) {
	dst[0], dst[1], dst[2], dst[3] = src[0], src[1], src[2], src[3]
}

func putUint16(dst []byte, src []byte) {
	dst[0], dst[1] = src[0], src[1]
}

// puppetgrab 12074 is a setter whose argument must be a **boolean**, which is
// unusual among the builtins. Verified FUN_00408700:
//
//	evaluate argument
//	if (arg.Kind != 2) return 0x0E;
//	DAT_004599AC = (unsigned short)arg.Value;
//
// So a numeric argument is a type error rather than being coerced, and only the
// low 16 bits of the value are stored. `puppetgrab` 12074 is therefore a mode
// flag rather than a value.
const (
	// PuppetGrabOpcode is the command opcode.
	PuppetGrabOpcode uint16 = 12074
	// PuppetGrabSlot is the global the grab flag is stored in, DAT_004599AC.
	PuppetGrabSlot = "DAT_004599AC"
)

// SetPuppetGrab stores a puppet grab flag. The argument must be type 2; the
// reference writes only its low 16 bits, so a boolean carrying a larger value is
// truncated rather than rejected.
func SetPuppetGrab(argument Record) (uint16, error) {
	if argument.Kind != ValueTypeBool {
		return 0, ErrWrongOperandType
	}
	return uint16(argument.Data), nil
}

// closepuppetfile 12042 has a re-entrancy guard and a status of its own. Verified
// FUN_00408120:
//
//	*out = 0;
//	if (DAT_00459A92 == 0) return 0x2D;                    // no puppet open
//	saved = *(int *)(DAT_00459A9A + 0xD34);               // a field of the puppet
//	if (FUN_004083A0() == 0) {
//	    if (DAT_00459A92 == 0) return 0;                   // already gone: success
//	    if (*(int *)(DAT_00459A9A + 0xD34) != saved) return 0;  // changed: success
//	    FUN_004081A0(&DAT_00459A92);
//	    FUN_0042B7C0();
//	    FUN_004055C0(2);
//	}
//	return 0;
//
// So the close only proceeds if the puppet is still present **and** the field at
// offset 0xD34 is unchanged. Either having changed makes it return success
// without closing, which is deliberate: something else already dealt with it. A
// port that dropped the compare would close a puppet whose state had moved on.
const (
	// ClosepuppetfileOpcode is the command opcode.
	ClosepuppetfileOpcode uint16 = 12042
	// NoPuppetSlot is the global that must be non-zero for a puppet to be open.
	NoPuppetSlot = "DAT_00459A92"
	// PuppetGuardedFieldOffset is the field compared across the close call, at
	// offset 0xD34 of the puppet block.
	PuppetGuardedFieldOffset = 0xD34
	// StatusNoPuppet is native status 0x2D, returned when no puppet is open.
	StatusNoPuppet uint16 = 0x2D
	// ClosepuppetfileTearDownArgument is the literal 2 passed to the last of the
	// three teardown calls.
	ClosepuppetfileTearDownArgument = 2
)

// ClosePuppetOutcome is what closepuppetfile did.
type ClosePuppetOutcome uint8

const (
	// PuppetClosed means the puppet was torn down.
	PuppetClosed ClosePuppetOutcome = iota
	// PuppetAlreadyGone means the puppet slot was empty by the time the close
	// ran, and the reference reports success.
	PuppetAlreadyGone
	// PuppetStateChanged means the guarded field changed across the close, and
	// the reference reports success without closing.
	PuppetStateChanged
	// PuppetCloseDeferred means the underlying closer returned non-zero and the
	// teardown did not run.
	PuppetCloseDeferred
)

// ClosePuppet reproduces closepuppetfile's decision. open reports whether
// DAT_00459A92 is non-zero, guardedBefore and guardedAfter are the field at offset
// 0xD34 sampled before and after the underlying closer, and closerStatus is what
// that closer returned.
func ClosePuppet(open bool, guardedBefore, guardedAfter int32, closerStatus uint16) (ClosePuppetOutcome, uint16) {
	if !open {
		return PuppetAlreadyGone, StatusNoPuppet
	}
	if closerStatus != 0 {
		return PuppetCloseDeferred, closerStatus
	}
	if !open {
		return PuppetAlreadyGone, 0
	}
	if guardedAfter != guardedBefore {
		return PuppetStateChanged, 0
	}
	return PuppetClosed, 0
}
