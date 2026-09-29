package scripts

import "fmt"

// The sub-resource node and its three operations — find, load and parse — mapped from
// the creator `FUN_004012B0`, the finder `FUN_004017C0`, the loader `FUN_00401D80` and
// the parser `FUN_00401DE0`.
//
// The creator is the one that matters, because **it initialises every field the other
// three read**, and it settles four things that were previously only half-recorded.
//
// # The node
//
//	node = GlobalAlloc(GMEM_FIXED, 0x0D54);      // 3412 bytes, FIXED not moveable
//	node[0x34D] = FUN_004012A0();                 // the version, byte 0xD34
//	node[0x34E] = 0;                             // byte 0xD38: the entry count
//	node[0x34F] = 0x14;                          // byte 0xD3C: the capacity, TWENTY
//	block = GlobalAlloc(GMEM_MOVEABLE, 0x140);   // 320 bytes
//	node[0x350] = block;                         // byte 0xD40: the table handle
//	node[0x352] = size;                          // byte 0xD48
//	*(u16 *)(node + 0x353) = kind;               // byte 0xD4C
//	*(u16 *)((byte *)node + 0xD4E) = 0;          // byte 0xD4E: the re-entrancy guard
//	node[0x354] = 0;                             // byte 0xD50
//	FUN_00401B80(path, node);                    // read the contents
//	node[0x351] = head;                          // byte 0xD44: the chain link
//	head = node;                                 // pushed at the head, newest first
//
// # Four things this settles
//
// **The version field is written, not merely read.** Every earlier derivation of byte
// `0xD34` found it being *sampled* — by the re-entrancy guards, by the `sendto` handlers,
// by the actor store. The creator is what *mints* it, from `FUN_004012A0()`. That is the
// missing half of the field's story, and it is the fourth independent confirmation of
// the offset: byte `0xD34` from a DWORD index, byte `0xD34` from a guard, and now byte
// `0xD34` from the initialiser that fills it in.
//
// **The re-entrancy guard at byte `0xD4E` is *zeroed* by the creator.** The guard was
// known only as a check with diagnostic `0x0A29`; nothing had said what put it there or
// what cleared it. The creator sets it to zero, so the guard is per-operation state that
// is armed and disarmed rather than a permanent flag — which is what makes re-entering
// the same sub-resource detectable at all.
//
// **The table is twenty 16-byte entries, inline in a 320-byte moveable block.** The
// capacity field is set to `0x14` = 20, the block is `0x140` = 320 bytes, and the finder
// steps sixteen bytes at a time. `320 / 16 = 20` exactly, so the initial table is full
// on allocation and the reallocation only matters once the count passes twenty. That is
// the **third** structure in the engine with a capacity of twenty and a grow-by-twenty
// step, after the value pool and the per-file sub-resource cache — so the pair of
// behaviours is now a house idiom in three places rather than two.
//
// **The nodes are a singly-linked list pushed at the head**, so they come back
// newest-first. `DAT_00442018` is the head pointer and each node stores the previous head
// at byte `0xD44`, which is the only chain field there is.
const (
	// SubNodeSize is the node allocation, 0x0D54 = 3412 bytes.
	SubNodeSize = 0x0D54
	// SubNodeAllocFlag is GMEM_FIXED, zero. **The node is fixed, not moveable**, which
	// is the opposite of the table block and of the 0x140 block inside it — a node holds
	// pointers, so it cannot be moved while those are live.
	SubNodeAllocFlag uint32 = 0x0000
	// SubNodeFixedAllocFlag is GMEM_FIXED spelled out, for a reader who wants the
	// contrast with GMEM_MOVEABLE.
	SubNodeFixedAllocFlag uint32 = 0x0000
	// SubTableBlockBytes is the table block's size, 0x140 = 320.
	SubTableBlockBytes = 0x140
	// SubTableEntryBytes is one entry's size, 16, which is the finder's step.
	SubTableEntryBytes = 16
	// SubTableInitialCapacity is the capacity the creator writes, 0x14 = 20. And
	// 320 / 16 = 20, so the initial block is exactly full.
	SubTableInitialCapacity = 0x14
	// SubNodeFixedAlloc is the flag the table block uses, GMEM_MOVEABLE, the opposite of
	// the node's own.
	SubTableAllocFlag uint32 = 0x0002
)

// The node's field offsets, in bytes, all of them written by the creator.
const (
	// SubNodeVersionOffset is the version field, 0xD34. Four independent derivations
	// agree on it and the creator fills it in.
	SubNodeVersionOffset = 0xD34
	// SubNodeCountOffset is the entry count, 0xD38, which the creator zeroes.
	SubNodeCountOffset = 0xD38
	// SubNodeCapacityOffset is the capacity, 0xD3C, which the creator sets to twenty.
	SubNodeCapacityOffset = 0xD3C
	// SubNodeTableOffset is the table block's handle, 0xD40.
	SubNodeTableOffset = 0xD40
	// SubNodeChainOffset is the previous-head pointer, 0xD44, giving the list.
	SubNodeChainOffset = 0xD44
	// SubNodeSizeFieldOffset is the stored resource size, 0xD48.
	SubNodeSizeFieldOffset = 0xD48
	// SubNodeKindOffset is the resource kind, 0xD4C, sixteen bits.
	SubNodeKindOffset = 0xD4C
	// SubNodeReentrancyOffset is the re-entrancy guard, 0xD4E, sixteen bits, **zeroed by
	// the creator**.
	SubNodeReentrancyOffset = 0xD4E
	// SubNodeFinalOffset is the last field the creator writes, 0xD50.
	SubNodeFinalOffset = 0xD50
	// SubReentrancyDiag is the diagnostic the guard raises, 0x0A29.
	SubReentrancyDiag uint16 = 0x0A29
	// SubVersionStamp is the routine that mints the version, FUN_004012A0.
	SubVersionStamp = "FUN_004012A0"
	// SubNodePopulate is the routine that reads the node's contents, FUN_00401B80.
	SubNodePopulate = "FUN_00401B80"
	// SubNodeExistenceCheck is the file-existence call, FUN_0042C2A0, which uses the
	// **inverted** convention recorded earlier: zero means found.
	SubNodeExistenceCheck = "FUN_0042C2A0"
	// SubNodeHead is the list head the creator pushes onto, DAT_00442018.
	SubNodeHead = "DAT_00442018"
	// SubNodeKeyCache and SubNodeIndexCache are the finder's cache, a key and an index.
	// **This is the fifth cached name lookup** in the engine, after the prop table, the
	// actor table, the stage resolver and the value pool's interning — and the fifth to
	// disagree with the others on the miss status, since this one returns 0 where the
	// name resolvers return 0x0A and the pool 9.
	SubNodeKeyCache   = "DAT_00442014"
	SubNodeIndexCache = "DAT_00442004"
)

// StatusSubStampMismatch is the status the loader and the parser return when the key and
// the stamp do not agree, **100**. It is new to the verified vocabulary and it is the
// only place the two operations use it.
//
// Note it is decimal 100, not hex: the decompilation returns a bare `100`, so a port
// that wrote `0x64` would be right about the value and wrong about where it came from.
const StatusSubStampMismatch uint16 = 100

// SubNodeLayout is the creator's initial state, which is every field it writes.
type SubNodeLayout struct {
	// Version is the stamped value, from the version routine.
	Version uint32
	// Count is the entry count, zero on a fresh node.
	Count uint16
	// Capacity is twenty on a fresh node, so the table is exactly full.
	Capacity uint16
	// TableHandle identifies the 320-byte table block.
	TableHandle uint32
	// ResourceSize is the stored size, from the existence check.
	ResourceSize uint32
	// Kind is the resource kind, sixteen bits.
	Kind uint16
	// Reentrancy is the guard, **zero** on a fresh node, which is what makes it
	// detectable state rather than a permanent flag.
	Reentrancy uint16
	// Chain is the previous head, so nodes come back newest-first.
	Chain uint32
}

// NewSubNodeLayout returns the layout a fresh node has, given the version the stamp
// routine produced and the size and kind the existence check reported.
func NewSubNodeLayout(version uint32, size uint32, kind uint16) SubNodeLayout {
	return SubNodeLayout{
		Version:      version,
		Count:        0,
		Capacity:     SubTableInitialCapacity,
		ResourceSize: size,
		Kind:         kind,
		Reentrancy:   0,
	}
}

// VerifySubNodeGeometry checks the numbers that were derived in three separate places
// and have to agree: the block size against the capacity and the stride, and the
// version offset against the two spellings already recorded.
func VerifySubNodeGeometry() (ok bool, detail string) {
	// **The fit.** 320 bytes over a 16-byte stride is exactly twenty entries, which is
	// the capacity the creator writes. If either number were wrong the other would not
	// divide.
	if SubTableBlockBytes/SubTableEntryBytes != SubTableInitialCapacity {
		return false, fmt.Sprintf("the %d byte block holds %d entries at a %d byte stride, but the capacity is %d",
			SubTableBlockBytes, SubTableBlockBytes/SubTableEntryBytes, SubTableEntryBytes, SubTableInitialCapacity)
	}
	// The version's two spellings must name one field.
	if ContextVersionDwordIndex*4 != SubNodeVersionOffset {
		return false, fmt.Sprintf("the version is at byte %#x but the recorded DWORD index gives %#x",
			SubNodeVersionOffset, ContextVersionDwordIndex*4)
	}
	if SubNodeVersionOffset != ContextVersionByteOffset {
		return false, "the node's version and the context's version are different fields"
	}
	// The fields must be inside the node and in the order the creator writes them.
	ordered := []struct {
		name   string
		offset int
	}{
		{"version", SubNodeVersionOffset},
		{"count", SubNodeCountOffset},
		{"capacity", SubNodeCapacityOffset},
		{"table", SubNodeTableOffset},
		{"chain", SubNodeChainOffset},
		{"size", SubNodeSizeFieldOffset},
		{"kind", SubNodeKindOffset},
		{"reentrancy", SubNodeReentrancyOffset},
		{"final", SubNodeFinalOffset},
	}
	for i, field := range ordered {
		if field.offset < 0 || field.offset >= SubNodeSize {
			return false, fmt.Sprintf("the %s field at +%#x is outside the %d byte node",
				field.name, field.offset, SubNodeSize)
		}
		if i > 0 && field.offset <= ordered[i-1].offset {
			return false, fmt.Sprintf("the %s field at +%#x does not follow %s at +%#x",
				field.name, field.offset, ordered[i-1].name, ordered[i-1].offset)
		}
	}
	// The guard's diagnostic, and the two allocation flags being opposites.
	if SubReentrancyDiag != 0x0A29 {
		return false, fmt.Sprintf("the guard raises %#x, want 0x0A29", SubReentrancyDiag)
	}
	if SubNodeAllocFlag == SubTableAllocFlag {
		return false, "the node is fixed and the table block moveable; the flags must differ"
	}
	// And the miss status is a fifth value, distinct from every other miss the engine
	// has, which is itself the finding.
	if StatusSubStampMismatch == StatusNameNotFound || StatusSubStampMismatch == StatusInternNotFound {
		return false, "the stamp-mismatch status should differ from the name-resolver misses"
	}
	return true, "the block holds exactly twenty entries, the version's two spellings agree, and the fields are ordered"
}

// SubNodeMissingError is the status the creator returns when the existence check says
// the file is absent. It reuses `0x6B`, the file-not-found status, because the check's
// **inverted** convention means a non-zero result is "not found" — the same convention
// recorded for this call earlier, and now confirmed from a third direction.
const SubNodeMissingStatus uint16 = StatusFileNotFound

// SubNodeExists is the existence check's answer, in the reference's inverted form: zero
// means the file **was** found.
func SubNodeExists(foundFlag int32) bool { return foundFlag == 0 }

// SubLookupOutcome is what the finder reports, and the miss case is the interesting one.
type SubLookupOutcome uint8

const (
	// SubLookupMiss means the key is not in the table. **The finder returns 0 here, not
	// a status code** — so a caller cannot tell a miss from a failure by the return
	// alone, which is the fifth distinct convention among the engine's cached lookups.
	SubLookupMiss SubLookupOutcome = iota
	// SubLookupCacheHit means the cached index was in range and still matched by key.
	SubLookupCacheHit
	// SubLookupScanHit means the scan found it and the cache was replaced.
	SubLookupScanHit
)

// SubFindKey is the finder's cached-lookup logic, in the reference's own shape: the
// cache is consulted first and re-validated by key, the scan replaces it, and the index
// must be positive and below the count on the cached path.
func SubFindKey(keys []uint32, key uint32, cachedKey, cachedIndex int32) (index int, outcome SubLookupOutcome, status uint16) {
	count := len(keys)
	// The cached test is `key == cachedKey && cachedIndex < count`, and the index must
	// also be positive, so a cache of zero or of exactly the count is rescanned.
	if cachedKey == int32(key) && cachedIndex > 0 && cachedIndex < int32(count) {
		if keys[cachedIndex] == key {
			return int(cachedIndex), SubLookupCacheHit, 1
		}
	}
	// The scan, which replaces both halves of the cache on a hit.
	for i := 0; i < count; i++ {
		if keys[i] == key {
			return i, SubLookupScanHit, 1
		}
	}
	// **The miss returns a zero status**, not a miss code.
	return 0, SubLookupMiss, 0
}

// RequiredSubBytes reproduces the parser's allocation floor: `if (bytes == 0) bytes = 1`.
// **This is the third place the engine uses that floor**, after the value pool's text
// growth and the slot table's, so it is a house idiom rather than a local guard. It
// exists because a zero-byte GlobalAlloc fails.
func RequiredSubBytes(bytes uint32) uint32 {
	if bytes == 0 {
		return 1
	}
	return bytes
}
