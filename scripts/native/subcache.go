package native

import (
	"fmt"
)

// The rest of the sub-resource chain: the version stamp `FUN_004012A0`, the page lookup
// `FUN_00402940`, the stamp read `FUN_00402160` and the file populate `FUN_00401B80`.
// Together with subnode.go these complete the sub-resource layer.
//
// # The version is a global counter, and nothing else
//
//	void FUN_004012A0(void) { DAT_0044200C = DAT_0044200C + 1; }
//
// **That is the entire function** — one increment, no wrap check, no comparison, no
// return value. So the "version" every other function has been sampling is a
// monotonically increasing global, and a node's version is simply the counter's value at
// the moment the node was created.
//
// That single fact explains every version check in the project at once. The creator
// stores the counter; a later comparison asks "has the counter moved since this was
// taken?", and a mismatch means the data is stale. The actor store's version check, the
// sub-resource guard and the value pool's field are all the same idea at different
// scales, and none of them needs the counter to mean anything more than "a later moment".
const (
	// SubVersionCounter is the global the stamp routine increments.
	SubVersionCounter = "DAT_0044200C"
	// SubVersionCounterIsPlainInteger is the finding, recorded as a constant so a
	// reader does not go looking for a version algorithm that does not exist.
	SubVersionCounterIsPlainInteger = true
)

// StampVersion advances the global version counter and returns the new value, which is
// what the node creator stores. The reference's routine returns nothing and the caller
// reads the global, so the value is returned here for convenience and the arithmetic is
// the reference's single increment either way.
func StampVersion(current uint32) uint32 {
	return current + 1
}

// # The stamp is eight bytes
//
//	void FUN_00402160(node, offset, out) {
//	    if (FUN_0042C010(node, offset) == 0) FUN_0042BE70(node, out, 8);
//	}
//
// So the stamp is **seek-then-read-eight-bytes**, and the loader's comparison is a
// 64-bit equality. A 4-byte reading would miss a stamp whose upper half differs, and the
// width is the whole content of the check.
const (
	// SubStampBytes is the stamp's width, 8.
	SubStampBytes = 8
	// SubStampSeek is the seek routine, FUN_0042C010.
	SubStampSeek = "FUN_0042C010"
	// SubStampReader is the read routine the stamp shares with the parser,
	// FUN_0042BE70. It is also what the page fill and the populate use, so it is the
	// engine's single file-read primitive.
	SubStampReader = "FUN_0042BE70"
)

// # The page cache: four pages, 128 entries each, least-recently-used
//
//	if ((int)key < 0) return 100;                       // a negative key is refused
//	if ((int)key < node[0x46]) {                        // byte 0x118: the key bound
//	    count = node[0x142];                             // byte 0x508: the page count
//	    oldest = 0x7FFFFFFF; victim = 0;
//	    for (i = 0; i < count; i++) {
//	        if (page[i] == (key & 0xFFFFFF80)) {          // the page's key
//	            page[i].uses = node[0x141]++;             // touch
//	            *out = page[i].entry[(key - page[i].key)];// the entry within the page
//	            return 0;                                 // HIT
//	        }
//	        if (page[i].uses < oldest) { oldest = ...; victim = i; }
//	        page += 0x20A;                                 // the page stride, DWORDs
//	    }
//	    if (count < 4) { ...add a page and fill it... }
//	    else { ...evict the least-recently-used page... }
//	}
//
// So it is a **four-page cache of 128-entry pages, evicted least-recently-used**, and
// the three numbers are related: a page's key is `key & 0xFFFFFF80`, so a page covers
// `0x80` = 128 four-byte entries; the stride is `0x20A` DWORDs; and the use counter is a
// single global per node, not a per-page timestamp, so "least recently used" means
// "smallest counter value", which orders pages by insertion-or-last-touch without
// recording a time.
const (
	// SubPageCount is how many pages the cache holds, 4. The reference's test is
	// `count < 4`, so a fifth page evicts rather than extends.
	SubPageCount = 4
	// SubPageEntries is how many entries a page covers, 0x80 = 128, which is what the
	// key mask implies.
	SubPageEntries = 0x80
	// SubPageKeyMask is the mask applied to a key to get its page, 0xFFFFFF80. Clearing
	// the low seven bits is what makes a page 128 entries.
	SubPageKeyMask uint32 = 0xFFFFFF80
	// SubPageStrideDwords is a page's stride in DWORDs, 0x20A, which is 2088 bytes.
	SubPageStrideDwords = 0x20A
	// SubPageStrideBytes is that in bytes.
	SubPageStrideBytes = SubPageStrideDwords * 4
	// SubPageEntryOffset is where a page's entries begin, byte 10, so the page's own
	// header is ten bytes.
	SubPageEntryOffset = 10
	// SubPageEntryBytes is one entry's size, 4 — which is what the per-key stride
	// `* 4` says.
	SubPageEntryBytes = 4
	// SubPageHeaderBytes is a page's own header, 10 bytes, which is the entry offset.
	SubPageHeaderBytes = SubPageEntryOffset
	// SubUseCounterOffset is the per-node use counter, byte 0x504, the field every
	// page's touch writes.
	SubUseCounterOffset = 0x504
	// SubPageCountOffset is the per-node page count, byte 0x508.
	SubPageCountOffset = 0x508
	// SubPageArrayOffset is where the page array begins, byte 0x1428.
	SubPageArrayOffset = 0x1428
	// SubKeyBoundOffset is the bound a key must be under, byte 0x118.
	SubKeyBoundOffset = 0x118
	// SubPageFill and SubPageEvict are the two routines the cache calls, which are the
	// only unmapped parts of it.
	SubPageFill   = "FUN_00402C40"
	SubPageEvict  = "FUN_00402C80"
	// SubUseCounterStartsAtMax is the initial value of the LRU search, 0x7FFFFFFF, so
	// the first page examined is always the oldest if nothing has been touched.
	SubUseCounterStartsAtMax uint32 = 0x7FFFFFFF
)

// PageKey returns the page a key belongs to, which is the key with its low seven bits
// cleared. This is the whole of the paging: a key is an offset into a file, and the high
// bits select a 128-entry block of it.
func PageKey(key uint32) uint32 { return key & SubPageKeyMask }

// PageOffset returns an entry's index within its page, which is the key's low seven
// bits. So a page holds 128 consecutive four-byte entries and an entry is addressed by
// its offset from the page's key.
func PageOffset(key uint32) int { return int(key &^ SubPageKeyMask) }

// PageEntryAddress returns the byte address of an entry within a page's base, which is
// the reference's `(key - pageKey) * 4 + 10`.
func PageEntryAddress(pageBase uint32, key uint32) uint32 {
	return pageBase + uint32(PageOffset(key))*SubPageEntryBytes + SubPageEntryOffset
}

// LeastRecentlyUsed returns the index of the page with the smallest use counter, and
// whether the cache has any pages at all. The reference's initial value is 0x7FFFFFFF, so
// an untouched page is never the victim.
func LeastRecentlyUsed(uses []uint32) (index int, found bool) {
	oldest := SubUseCounterStartsAtMax
	for i, u := range uses {
		if u < oldest {
			oldest = u
			index = i
			found = true
		}
	}
	return index, found
}

// # The file's two magic numbers, and the two statuses they raise
//
//	if (FUN_0042BE70(&handle, node + 0x41, 0x400) == 0) {   // read 1024 bytes
//	    if (node[0x41] != 0x10000) return 0x6A;               // the first magic
//	    if (node[0x42] != <a value read from the file>) return 0x65;  // the second
//	    pages = node[0x45] >> 7; if (pages > 4) pages = 4;    // the page count, clamped
//	}
//
// So the sub-resource file has a **header of at least 0x108 bytes with two magic
// values**, and both mismatches are reported rather than tolerated:
//
//	0x6A  the field at byte 0x104 is not 0x10000
//	0x65  the field at byte 0x108 does not match the file's own copy of it
//
// **The first magic is `0x10000`, which is 65536 and not a small tag.** That is worth
// stating because a reader expecting a signature like `0xDUST` would read it as a
// mistranscription. The second is not a constant at all — it is compared against a value
// read from elsewhere in the same file, so it is a **consistency check between two places
// in the file** rather than a magic number, and it is the more interesting of the two.
const (
	// SubHeaderReadBytes is how much the populate reads before checking, 0x400.
	SubHeaderReadBytes = 0x400
	// SubHeaderPathBytes is the 0x100-byte path copied into the node at byte 4.
	SubHeaderPathBytes = 0x100
	// SubHeaderPathOffset is where that path copy starts, byte 4 — immediately after the
	// file handle the creator wrote.
	SubHeaderPathOffset = 4
	// SubMagic1 is the first magic value, 0x10000. **A real number, not a tag.**
	SubMagic1 uint32 = 0x10000
	// SubMagic1Status is raised when it does not match, 0x6A.
	SubMagic1Status uint16 = 0x6A
	// SubMagic2Status is raised when the second consistency check fails, 0x65.
	SubMagic2Status uint16 = 0x65
	// SubPageCountFieldOffset is the file's own page-count field, byte 0x114, which is
	// shifted right by seven and clamped to four.
	SubPageCountFieldOffset = 0x114
	// SubPageCountShift is the shift applied to that field, 7 — dividing by 128, so the
	// file states its page count in the same 128-entry units the cache pages in.
	SubPageCountShift = 7
)

// ClampSubPageCount reproduces the file's page count: shifted right by seven and clamped
// to the cache's four. So a file claiming more than four pages is truncated rather than
// refused, and a file claiming a huge count is truncated too — which is why the clamp is
// an upper bound and not a validity check.
func ClampSubPageCount(field uint32) int {
	pages := int(field >> SubPageCountShift)
	if pages > SubPageCount {
		return SubPageCount
	}
	return pages
}

// RequiredSubKey reports whether a key is in range, reproducing the lookup's guard. A
// **negative** key is refused with the decimal 100 — the same code the loader and parser
// use for a stamp mismatch, so `100` is a general "bad key" rather than only a mismatch.
// The key is compared as *signed*, which is why a high-bit-set key is refused too.
func RequiredSubKey(key int32, bound int32) (uint16, error) {
	if key < 0 {
		return StatusSubStampMismatch, fmt.Errorf(
			"the key %d is negative; the reference compares it signed, so this is refused with status %d",
			key, StatusSubStampMismatch)
	}
	if bound > 0 && key >= bound {
		return StatusSubStampMismatch, fmt.Errorf(
			"the key %d is not below the bound %d (status %d)", key, bound, StatusSubStampMismatch)
	}
	return 0, nil
}
