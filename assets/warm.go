package assets

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The resource warm-up walk, shared by FUN_0040E120 (actorwarm and castwarm) and
// FUN_00421DD0 (propwarm and shopwarm). The two functions have the same shape and
// differ only in the diagnostic they raise. A record's own table lists its blocks:
// a count at +0x5A and rows of 0x20 bytes whose first dword is a resource key. Each
// block is acquired through the record's cache. The block's sub-table is a count at
// +0x72 and rows of 0x2C bytes from +0x76, whose first dword is a variant key. Each
// variant is acquired and released in turn, and then the block itself is released.
//
// A failed acquire is the native raise: FUN_004014F0 returns nonzero and the warm-up
// calls FUN_0042C470 with 0x100F (FUN_0040E120) or 0x1203 (FUN_00421DD0). Only the
// three directory failures are mapped to the diagnostic, because only those are
// verified in the port's model (key out of range, no file offset, record id not the
// key). Read and host failures are returned as plain errors.

// WarmSubTableCountOffset is the sub-table's variant count, at block +0x72.
const WarmSubTableCountOffset = 0x72

// WarmSubTableRowsOffset is where the sub-table's variant rows start, at block +0x76.
const WarmSubTableRowsOffset = 0x76

// WarmSubTableRowSize is the size of one variant row, 0x2C bytes.
const WarmSubTableRowSize = 0x2C

// ResourceDiagnosticActor is 0x100F, raised by FUN_0040E120 for actorwarm and castwarm.
const ResourceDiagnosticActor uint16 = 0x100F

// ResourceDiagnosticProp is 0x1203, raised by FUN_00421DD0 for propwarm and shopwarm.
const ResourceDiagnosticProp uint16 = 0x1203

// ResourceDiagnostic is a warm-up's resource-missing report. Code is the native
// diagnostic (0x100F or 0x1203) and Cause is the directory error that failed the
// acquire. The native reporter shows a message box and, on Cancel, ends the process.
// The port ends the run with this error, and does not model the OK branch, which
// continues the walk on a block handle the native never set.
type ResourceDiagnostic struct {
	Code  uint16
	Cause error
}

func (d *ResourceDiagnostic) Error() string {
	return fmt.Sprintf("resource diagnostic %#x: %v", d.Code, d.Cause)
}

func (d *ResourceDiagnostic) Unwrap() error { return d.Cause }

// WarmRecord is one record a warm-up walks: the cache its blocks are read through,
// and the keys of its blocks in the record's own table order.
type WarmRecord struct {
	Cache  *ResourceCache
	Blocks []uint32
}

// WarmBlocks lists the pose keys of an actor, the blocks actorwarm walks through
// FUN_0040E120. The keys are read from the actor's own record by OpenCast.
func (a CastActor) WarmBlocks() []uint32 {
	keys := make([]uint32, len(a.Poses))
	for index, pose := range a.Poses {
		keys[index] = pose.Resource
	}
	return keys
}

// WalkWarmRecords runs the warm-up walk over each record in order, with code as the
// diagnostic for a failed directory acquire. It stops at the first diagnostic.
func WalkWarmRecords(code uint16, records []WarmRecord) error {
	for _, record := range records {
		if record.Cache == nil {
			return fmt.Errorf("warm-up record has no resource cache")
		}
		for _, block := range record.Blocks {
			if err := walkWarmBlock(code, record.Cache, block); err != nil {
				return err
			}
		}
	}
	return nil
}

// walkWarmBlock acquires one block, walks its variant rows in order (acquire, then
// release each), and releases the block. The block stays cached with no references,
// as the native entry does.
func walkWarmBlock(code uint16, cache *ResourceCache, key uint32) error {
	block, err := cache.Acquire(key)
	if err != nil {
		return warmDiagnostic(code, err)
	}
	data, err := block.Bytes()
	if err != nil {
		_ = block.Close()
		return err
	}
	variants, err := warmVariantKeys(data)
	if err != nil {
		_ = block.Close()
		return fmt.Errorf("warm-up block %d: %w", key, err)
	}
	for _, variant := range variants {
		lease, err := cache.Acquire(variant)
		if err != nil {
			_ = block.Close()
			return warmDiagnostic(code, err)
		}
		if err := lease.Close(); err != nil {
			_ = block.Close()
			return fmt.Errorf("release warm-up variant %d: %w", variant, err)
		}
	}
	if err := block.Close(); err != nil {
		return fmt.Errorf("release warm-up block %d: %w", key, err)
	}
	return nil
}

// warmVariantKeys reads a block's variant keys from its sub-table. The count is
// checked against the block's length. The reference reads the rows without that
// check, so a truncated block is reported here as an error rather than read past its
// end (unverified for the reference).
func warmVariantKeys(data []byte) ([]uint32, error) {
	if len(data) < WarmSubTableRowsOffset {
		return nil, fmt.Errorf("sub-table header is truncated at %d bytes", len(data))
	}
	count := uint64(binary.LittleEndian.Uint32(data[WarmSubTableCountOffset : WarmSubTableCountOffset+4]))
	if count > uint64((len(data)-WarmSubTableRowsOffset)/WarmSubTableRowSize) {
		return nil, fmt.Errorf("sub-table count %d exceeds its %d-byte block", count, len(data))
	}
	keys := make([]uint32, int(count))
	for index := range keys {
		offset := WarmSubTableRowsOffset + index*WarmSubTableRowSize
		keys[index] = binary.LittleEndian.Uint32(data[offset : offset+4])
	}
	return keys, nil
}

// warmDiagnostic maps a directory failure to the warm-up's diagnostic. Other errors
// (a closed cache, a short read) pass through unchanged.
func warmDiagnostic(code uint16, err error) error {
	if errors.Is(err, ErrIndexOutOfRange) || errors.Is(err, ErrMissingEntry) || errors.Is(err, ErrRecordIndexMismatch) {
		return &ResourceDiagnostic{Code: code, Cause: err}
	}
	return err
}
