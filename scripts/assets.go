package scripts

import "fmt"

// The asset layer: how the engine finds, opens, caches and allocates the files
// behind every `open*file` builtin. Four functions, each fully specified.

// The existence check, `FUN_0042C120`, called through the inverted wrapper
// `FUN_0042C2A0`. Its body is the decisive evidence for that convention:
//
//	FUN_0042BD80();                                  // the shared pump
//	FUN_0042BBD0(path, buffer);                     // build a 256-byte path
//	attributes = GetFileAttributesA(buffer);
//	if (attributes == INVALID_FILE_ATTRIBUTES) return 0x6B;
//	*param_3 = 0; *param_2 = 0;
//	return 0;
//
// So it is a single `GetFileAttributesA`, and **status 0 means the file exists**
// while the new status **`0x6B` means it does not**. That is the opposite of the
// status convention used everywhere else in the engine, and it is why both callers
// test for zero when they are looking for a hit.
//
// Note also what the check does *not* do: it compares the whole attribute word
// against `INVALID_FILE_ATTRIBUTES` and reports nothing else. It does not
// distinguish a directory from a regular file, so the stage resolver's separate
// `0x31` status must come from inspecting the attribute bits itself.
const (
	// FileAttributesInvalid is `INVALID_FILE_ATTRIBUTES`, 0xFFFFFFFF, the return
	// meaning the path could not be examined at all.
	FileAttributesInvalid uint32 = 0xFFFFFFFF
	// FileAttributeDirectory is the bit a directory sets, 0x10. It is recorded
	// because the stage resolver's 0x31 status depends on distinguishing it, even
	// though this existence check does not.
	FileAttributeDirectory uint32 = 0x10
	// FilePathBufferSize is the 256-byte buffer the path is built in.
	FilePathBufferSize = 256
)

// StatusFileNotFound is native status 0x6B, the return when `GetFileAttributesA`
// reports `INVALID_FILE_ATTRIBUTES`. It is the newest and highest verified status
// so far, and it belongs to the inverted family: zero means success.
const StatusFileNotFound uint16 = 0x6B

// FileLookupResult is the outcome of an existence check.
type FileLookupResult uint8

const (
	// FileExistsAtLookup means the attributes were readable, so the path exists.
	FileExistsAtLookup FileLookupResult = iota
	// FileMissingAtLookup means GetFileAttributesA returned INVALID_FILE_ATTRIBUTES.
	FileMissingAtLookup
)

// ResolveFileLookup applies the existence check's own logic to an attribute word.
// This is the level below the inverted wrapper, and it is where the zero-means-found
// convention is visible without any status remapping in the way.
func ResolveFileLookup(attributes uint32) (FileLookupResult, uint16) {
	if attributes == FileAttributesInvalid {
		return FileMissingAtLookup, StatusFileNotFound
	}
	return FileExistsAtLookup, FileExistsFound
}

// IsDirectory reports whether an attribute word marks a directory. The plain
// existence check does not test this, but the stage resolver's 0x31 does, so the
// test is separated and the difference is explicit.
func IsDirectory(attributes uint32) bool {
	return attributes != FileAttributesInvalid && attributes&FileAttributeDirectory != 0
}

// The allocator, `FUN_00401120`, which every file load goes through. Verified body:
//
//	*out = 1;                                   // default to "a real handle"
//	free = FUN_0042C420();                     // bytes currently available
//	if ((request - free) + 300000 < 0) return free;      // cannot satisfy
//	size = (request - free) + 500000;
//	if (allocate(&size) == 0) {
//	    if (DAT_00442010 != NULL && (u = DAT_00442010(&size), u != 0)) return u;
//	    if (size > -1) *out = 0;                // success, but no real handle
//	    return 0;
//	}
//
// Two details are worth pinning. The refusal test uses **300000** and the request
// it then makes uses **500000**; they are different numbers, so a port that
// conflated them would refuse requests the reference accepts. And the `*out` flag
// is initialised to 1 and only cleared on success, so it distinguishes "allocated"
// from "succeeded without a handle".
//
// **What this function does NOT do is pretend to know what the arithmetic means.**
// `FUN_0042C420` is not decompiled, so its return value has no established
// meaning here. The early return fires when `request - other + 300000` is
// negative, which by inspection means the request is at least 300000 *smaller*
// than whatever `other` is, so `other` is plainly not a free-space count: a
// request that fits easily would otherwise be refused. The expression is
// transcribed faithfully with neutral parameter names rather than given an
// interpretation the evidence does not support.
const (
	// AllocatorShortfall is the 300000-byte constant in the refusal test.
	AllocatorShortfall = 300000
	// AllocatorHeadroom is the 500000-byte constant in the request.
	AllocatorHeadroom = 500000
)

// AllocatorArithmetic reproduces `FUN_00401120`'s two expressions verbatim.
//
//	other := FUN_0042C420()                          // meaning not established
//	if (request - other + 300000 < 0) return early
//	size := request - other + 500000
//
// refused reports whether the early return was taken, and size is the request that
// would then be made. A refused call makes no attempt, so size is meaningless then.
func AllocatorArithmetic(request, other int32) (refused bool, size int32) {
	if request-other+AllocatorShortfall < 0 {
		return true, 0
	}
	return false, request - other + AllocatorHeadroom
}

// AllocDecision is what the allocator decided.
type AllocDecision uint8

const (
	// AllocRefused means the early return was taken or the allocation returned
	// nothing, so nothing was obtained.
	AllocRefused AllocDecision = iota
	// AllocWithoutHandle means the allocation succeeded but produced no real
	// handle, which the reference signals by clearing the out flag.
	AllocWithoutHandle
	// AllocWithHandle means the allocation produced a real handle.
	AllocWithHandle
)

// SettleAllocation applies the second half: given what the underlying allocator
// did, report the decision. gotHandle reports whether a real handle came back, and
// nullAlloc reports a null allocation, which the reference distinguishes from a
// handle-less success.
func SettleAllocation(size int32, nullAlloc bool, gotHandle bool) (AllocDecision, error) {
	if nullAlloc {
		return AllocRefused, fmt.Errorf("the allocator returned nothing for %d bytes", size)
	}
	if !gotHandle {
		return AllocWithoutHandle, nil
	}
	return AllocWithHandle, nil
}

// The free list of opened files, `FUN_00401980`. Verified body:
//
//	prev = &DAT_00442008;                          // the head pointer cell
//	node = DAT_00442008;
//	while (true) {
//	    if (node == 0) return create(path, out);   // list empty, make a new one
//	    if (FUN_0042E5B0(node + 4, path) != 0) break;   // the name matches
//	    prev = (uint *)(node + 0xD50);
//	    node = *prev;
//	}
//	next = *(uint *)(node + 0xD50);
//	*prev = next;                                 // unlink
//	*(u32 *)(node + 0xD50) = 0;
//	*(u16 *)(node + 0xD4E) = 0;
//	*out = node;
//
// So a file record is a **node in a singly-linked list of at least `0xD50` bytes**,
// it is found by its name at offset 4 using the case-folded compare, the next
// pointer lives at `0xD50`, and a 16-bit field at `0xD4E` is cleared on reuse. The
// "handle" the caller receives *is* the node pointer, so a record's identity is its
// address in this pool.
const (
	// FileNodeSize is the minimum node size, set by the next pointer at 0xD50.
	FileNodeSize = 0xD50
	// FileNodeNameOffset is the Pascal name, at offset 4.
	FileNodeNameOffset = 4
	// FileNodeNextOffset is the next pointer, at 0xD50.
	FileNodeNextOffset = 0xD50
	// FileNodeClearOffset is the 16-bit field cleared when a node is reused, at
	// 0xD4E. It sits just below the next pointer.
	FileNodeClearOffset = 0xD4E
	// FileListHeadSlot is the head pointer cell, DAT_00442008.
	FileListHeadSlot = "DAT_00442008"
)

// FileNode is one node of the free list. Nodes are pooled and reused, so a node's
// index in the pool is its identity.
type FileNode struct {
	// Name is the Pascal name the node is keyed under.
	Name string
	// Next is the pool index of the following node, or nilIndex for the end.
	Next int
	// RefCount is the 16-bit field at 0xD4E, cleared when the node is reused.
	RefCount uint16
	// Loaded records whether the node currently holds an open file.
	Loaded bool
}

// nilNodeIndex marks the end of the free list. The reference uses a null pointer;
// the Go side uses an index so the pool is a slice.
const nilNodeIndex = -1

// TakeFileNode reproduces FUN_00401980's search: walk the list from the head,
// comparing names with the case fold, and on a match unlink the node, clear its
// next pointer and its refcount, and return it. An empty list means the caller must
// create one, which is reported rather than done here since creation is a separate
// function.
func TakeFileNode(nodes []FileNode, head int, name string) (index int, node FileNode, needCreate bool, err error) {
	previous := nilNodeIndex
	current := head
	for current != nilNodeIndex {
		if current < 0 || current >= len(nodes) {
			return 0, FileNode{}, false, fmt.Errorf("the free list points at node %d, outside the %d node pool",
				current, len(nodes))
		}
		if equalASCIIFold(nodes[current].Name, name) {
			// Unlink: the predecessor takes this node's successor.
			if previous != nilNodeIndex {
				nodes[previous].Next = nodes[current].Next
			}
			node = nodes[current]
			node.Next = nilNodeIndex
			node.RefCount = 0
			node.Loaded = true
			return current, node, false, nil
		}
		previous = current
		current = nodes[current].Next
	}
	return 0, FileNode{}, true, nil
}

// The per-file sub-resource cache, `FUN_004014F0`. Each open file owns an array of
// fixed-size entries, one per sub-resource it has loaded.
//
// Verified hit path:
//
//	if (find(file, index, &slot) != 0) {                  // already loaded
//	    entries = GlobalLock(file[0x350]);
//	    entry = entries + slot * 0x10;
//	    entry.refCount += 1;                              // at entry offset 4
//	    entry.stamp = FUN_004012A0();                     // at entry offset 12
//	    *out = entry.handle;                              // at entry offset 8
//	    GlobalUnlock; return 0;
//	}
//
// Verified miss path: load, allocate `size + 20000`, and on a null allocation
// return **`0x66`**. Then parse, bump the count at `file[0x34E]`, and if the count
// has passed the capacity at `file[0x34F]`, raise the capacity by **20** and
// reallocate `capacity * 0x10` bytes as a moveable block.
const (
	// SubResourceEntrySize is the 0x10 byte per-entry stride.
	SubResourceEntrySize = 0x10
	// SubResourceRefCountOffset is the entry's reference count, at offset 4.
	SubResourceRefCountOffset = 4
	// SubResourceHandleOffset is the entry's handle, at offset 8.
	SubResourceHandleOffset = 8
	// SubResourceStampOffset is the entry's stamp, at offset 12.
	SubResourceStampOffset = 0x0C
	// SubResourceGrowth is how many entries the capacity rises by when the count
	// overtakes it, 0x14.
	SubResourceGrowth = 0x14
	// SubResourceHeadroom is the 20000 bytes added to a sub-resource's own size,
	// the same headroom the entity cache uses.
	SubResourceHeadroom = 20000
	// SubResourceCountSlot is the file's sub-resource count, at file offset
	// 0x34E, and SubResourceCapacitySlot the capacity, at 0x34F.
	SubResourceCountSlot    = 0x34E
	SubResourceCapacitySlot = 0x34F
	// StatusNullAllocation is native status 0x66, returned when the headroom
	// allocation for a sub-resource yields nothing.
	StatusNullAllocation uint16 = 0x66
)

// GrowCapacity reproduces the miss path's growth rule: while the count strictly
// exceeds the capacity, add SubResourceGrowth, and return the new capacity and the
// byte size to reallocate. The reference's test is `capacity < count`, so a count
// exactly equal to the capacity does **not** grow; the count is raised before the
// test, so a miss that fills the last slot leaves the array exactly full.
func GrowCapacity(count, capacity int) (newCapacity int, bytes int) {
	for capacity < count {
		capacity += SubResourceGrowth
	}
	return capacity, capacity * SubResourceEntrySize
}

// SubResourceEntry is one entry of a file's sub-resource cache.
type SubResourceEntry struct {
	// RefCount is the 16-bit field at entry offset 4.
	RefCount uint16
	// Handle is the 32-bit field at entry offset 8.
	Handle uint32
	// Stamp is the 32-bit field at entry offset 12.
	Stamp uint32
}

// AcquireSubResource reproduces the hit path: take a reference, refresh the stamp
// and return the handle.
func AcquireSubResource(entries []SubResourceEntry, slot int) (SubResourceEntry, error) {
	if slot < 0 || slot >= len(entries) {
		return SubResourceEntry{}, fmt.Errorf("sub-resource slot %d is outside the %d entry array", slot, len(entries))
	}
	entry := &entries[slot]
	entry.RefCount++
	return *entry, nil
}

// StampSubResource applies the hit path's stamp, which comes from a separate
// monotonic source the reference does not otherwise expose.
func StampSubResource(entries []SubResourceEntry, slot int, stamp uint32) error {
	if slot < 0 || slot >= len(entries) {
		return fmt.Errorf("sub-resource slot %d is outside the %d entry array", slot, len(entries))
	}
	entries[slot].Stamp = stamp
	return nil
}
