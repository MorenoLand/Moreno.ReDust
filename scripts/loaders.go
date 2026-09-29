package scripts

import "fmt"

// The three file loaders behind the open*file family, and the resource-slot
// lookup that the entity cache depends on.
//
// The two simple loaders are **the same function with different diagnostics**.
// Verified FUN_0040C160 (cast) and FUN_004202E0 (shop):
//
//	if (FUN_00401980(path, record) != 0)        FUN_0042C470(0, 0xFA3 / 0x1327);
//	if (FUN_004014F0(record[0], 0, &record[1]))  FUN_0042C470(0, 0xFA4 / 0x1328);
//	header = GlobalLock(record[1]);
//	FUN_0042E6C0(header + 0x928, &record[3]);   // name
//	record[2] = *(uint *)(header + 0x924);      // a value just before the name
//	GlobalUnlock;
//
// So both produce a 28-byte record: a file handle, a header handle, the DWORD at
// header offset 0x924, and a 16-byte Pascal name. **The name sits at byte 12 of the
// record, which is the same offset as a flat table row and a prop record.** Three
// independent tables in the engine put the name at offset 12, and a fourth at 78;
// the offset is not uniform, so each is recorded from its own loader rather than
// assumed.

// CastRecordSize and ShopRecordSize are both 28, matching the open*file family's
// 0x1C stride for those two members.
const (
	CastRecordSize = 28
	ShopRecordSize = 28
)

// The shared record layout for the cast and shop loaders, in bytes.
const (
	// SimpleRecordFileHandle is the file handle at byte 0, from FUN_00401980.
	SimpleRecordFileHandle = 0
	// SimpleRecordHeaderHandle is the header handle at byte 4, from
	// FUN_004014F0.
	SimpleRecordHeaderHandle = 4
	// SimpleRecordValue is the DWORD copied from header offset 0x924, at byte 8.
	SimpleRecordValue = 8
	// SimpleRecordName is the Pascal name at byte 12, copied from header offset
	// 0x928. This is the offset a flat table row and a prop record share.
	SimpleRecordName = 12
	// SimpleHeaderValue is the header offset the value DWORD is read from.
	SimpleHeaderValue = 0x924
	// SimpleHeaderName is the header offset the name is read from, immediately
	// after the value.
	SimpleHeaderName = 0x928
)

// SimpleRecord is the 28-byte record the cast and shop loaders produce.
type SimpleRecord struct {
	// FileHandle is the raw resource handle the loader opened.
	FileHandle uint32
	// HeaderHandle is the header handle fetched from the file.
	HeaderHandle uint32
	// Value is the DWORD at header offset 0x924. Its meaning is not established;
	// it is the field immediately preceding the name.
	Value uint32
	// Name is the Pascal name copied from header offset 0x928.
	Name []byte
}

// ParseSimpleRecord fills a record from a locked header, reproducing both loaders
// exactly. headerNameOffset and headerValueOffset are the verified offsets, and
// they are passed in so a caller cannot accidentally pair a header with the wrong
// layout.
func ParseSimpleRecord(header []byte, headerValueOffset, headerNameOffset int) (SimpleRecord, error) {
	if headerValueOffset < 0 || headerValueOffset+4 > len(header) {
		return SimpleRecord{}, fmt.Errorf("the value field at +%#x is past the %d byte header",
			headerValueOffset, len(header))
	}
	if headerNameOffset < SimpleHeaderName {
		// Both verified loaders read the value immediately before the name, so a
		// gap would mean the pair is not the one transcribed.
		return SimpleRecord{}, fmt.Errorf("the name offset +%#x does not follow the value offset +%#x",
			headerNameOffset, headerValueOffset)
	}
	record := SimpleRecord{
		Value: readUint32(header[headerValueOffset:]),
		Name:  PascalCopy(header[headerNameOffset:]),
	}
	return record, nil
}

func readUint32(data []byte) uint32 {
	if len(data) < 4 {
		return 0
	}
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

// The diagnostics the two simple loaders raise, in the order they can reach them.
// Each loader has its own pair, so a report can name which file kind failed.
const (
	CastLoadFailedDiag   uint16 = 0x0FA3
	CastHeaderFailedDiag uint16 = 0x0FA4
	ShopLoadFailedDiag   uint16 = 0x1327
	ShopHeaderFailedDiag uint16 = 0x1328
)

// TrackSubArrayStride is the element size of the three parallel arrays the track
// loader builds, `0x68` = 104 bytes. It is the same for all three and is
// independent of the open*file record stride of 0x26.
const TrackSubArrayStride = 0x68

// TrackSubArrayCount is how many parallel arrays the track loader allocates.
const TrackSubArrayCount = 3

// The track loader's header offsets, verified from FUN_0040E340:
//
//	name            header +0x9E
//	count A (16-bit) header +0x18
//	count B (16-bit) header +0x1A
//	count C (16-bit) header +0x1C
//
// The three counts are consecutive 16-bit words, and each sizes one of the three
// sub-arrays at count * 0x68 bytes, with the same `if (size == 0) size = 1` guard
// the record array uses.
const (
	TrackHeaderName  = 0x9E
	TrackHeaderCountA = 0x18
	TrackHeaderCountB = 0x1A
	TrackHeaderCountC = 0x1C
	// TrackSubArrayMinSize is the guard applied to each sub-array allocation, the
	// same guard RequiredAllocation applies to the record array.
	TrackSubArrayMinSize = 1
)

// TrackSubArraySize returns the allocation a track sub-array of the given element
// count needs, reproducing `count * 0x68; if (size == 0) size = 1;`. The guard
// matters because `GlobalAlloc` rejects a zero-byte request, and a track file with
// any of its three counts at zero is entirely legal.
func TrackSubArraySize(elements int) int {
	size := elements * TrackSubArrayStride
	if size == 0 {
		return TrackSubArrayMinSize
	}
	return size
}

// TrackSubArraySizes returns the three sub-array sizes for a track header. Each is
// independent, so a track file can have one populated array and two empty ones.
func TrackSubArraySizes(header []byte) ([TrackSubArrayCount]int, error) {
	var sizes [TrackSubArrayCount]int
	offsets := [TrackSubArrayCount]int{TrackHeaderCountA, TrackHeaderCountB, TrackHeaderCountC}
	for i, offset := range offsets {
		if offset < 0 || offset+2 > len(header) {
			return sizes, fmt.Errorf("track count %d at +%#x is past the %d byte header", i, offset, len(header))
		}
		// The counts are signed 16-bit, and a negative one is treated as no
		// elements rather than as a huge unsigned count.
		count := int(int16(uint16(header[offset]) | uint16(header[offset+1])<<8))
		if count < 0 {
			count = 0
		}
		sizes[i] = TrackSubArraySize(count)
	}
	return sizes, nil
}

// FUN_00418A00, the resource-slot lookup behind the entity cache, is now fully
// specified and it **confirms the cache geometry recorded earlier from a second,
// independent source**:
//
//	i = 0;
//	entry = GlobalLock(DAT_00459EE0);
//	if (0 < DAT_00459EE8)
//	    do {
//	        if (entry[1] == index && entry[0] == context) { *slot = i; return 1; }
//	        entry += 8;                              // eight DWORDs, 32 bytes
//	        i++;
//	    } while (i < DAT_00459EE8);
//	GlobalUnlock; return 0;
//
// So the cache is keyed on a **(context, index) pair**, the stride is eight DWORDs
// which is exactly the 0x20 already recorded, the count is DAT_00459EE8, and the
// return is a **boolean: 1 for found, 0 for not**. It also names the two fields the
// earlier entry left unidentified: the context at offset 0 and the index at
// offset 4, which precede the stamp, handle and refcount.
const (
	// ResourceSlotFound is the return for a cache hit.
	ResourceSlotFound uint16 = 1
	// ResourceSlotMissing is the return for a miss.
	ResourceSlotMissing uint16 = 0
	// ResourceCacheCountSlot is the global holding the entry count, DAT_00459EE8.
	ResourceCacheCountSlot = "DAT_00459EE8"
	// ResourceContextOffset and ResourceIndexOffset are the two key fields, which
	// together identify a cached resource.
	ResourceContextOffset = 0
	ResourceIndexOffset   = 4
)

// ResourceKey is a (context, index) pair, the cache's key.
type ResourceKey struct {
	// Context is the owning context handle, at entry offset 0.
	Context uint32
	// Index is the index within that context, at entry offset 4.
	Index int32
}

// Matches reproduces the lookup's comparison, which tests the index first and then
// the context. The order is unobservable, but it is what the reference does.
func (k ResourceKey) Matches(context uint32, index int32) bool {
	return k.Index == index && k.Context == context
}

// FindResourceSlot walks the cache the way FUN_00418A00 does, returning the slot
// index and whether it was found. entries are ResourceKey values parallel to the
// table, since the Go side tracks keys rather than raw handle arithmetic.
func FindResourceSlot(entries []ResourceKey, count int, context uint32, index int32) (slot int, found bool) {
	if count <= 0 {
		return 0, false
	}
	limit := count
	if limit > len(entries) {
		limit = len(entries)
	}
	for i := 0; i < limit; i++ {
		if entries[i].Matches(context, index) {
			return i, true
		}
	}
	return 0, false
}
