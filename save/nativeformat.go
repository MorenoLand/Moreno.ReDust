package save

import (
	"encoding/binary"
	"fmt"
)

// Verified native save-container format, from the live Ghidra analysis of
// FUN_00422DE0 (the serializer reached by savegame 12077) and its helpers
// FUN_00401B70 (create) and FUN_00401AA0 (header write), plus the common-dialog
// file-type table at 0x0045DDB0 in the reference image.
//
// The native save is NOT a bespoke blob: it is the same APPL container the game
// data files use, opened with the create path and given a two-DWORD tag pair.
const (
	// NativeFileTypeRTD is the first entry of the reference file-type table at
	// 0x0045DDB0, "*.rtd" / "RTD". It is the default save extension.
	NativeFileTypeRTD = "rtd"
	// NativeFileTypeSTR is "*.str" / "STR".
	NativeFileTypeSTR = "str"
	// NativeFileTypeMOV is "*.mov" / "MOV".
	NativeFileTypeMOV = "mov"
	// NativeFileTypeTXT is "*.txt" / "TXT".
	NativeFileTypeTXT = "txt"
)

// NativeFileTypes is the verified file-type table, in the reference order that
// the common-dialog nFilterIndex indexes.
var NativeFileTypes = [...]string{
	NativeFileTypeRTD,
	NativeFileTypeSTR,
	NativeFileTypeMOV,
	NativeFileTypeTXT,
}

// Verified save-container tags. FUN_00422DE0 calls
// FUN_00401AA0(handle, 100, 0x5254444F, 0x44465254), so the container is created
// with count 100 and these two little-endian DWORD tags.
const (
	// NativeSaveTagPrimary is 0x5254444F, whose little-endian byte order spells
	// "ODTR". It is the first tag written to every save container.
	NativeSaveTagPrimary uint32 = 0x5254444F
	// NativeSaveTagSecondary is 0x44465254, the second tag.
	NativeSaveTagSecondary uint32 = 0x44465254
	// NativeSaveEntryCount is the count passed to the header writer: 100.
	NativeSaveEntryCount = 100
)

// Verified APPL container geometry, from FUN_00401AA0:
//
//	if (count < 1) count = 1;
//	pages       = (count + 0x7F) >> 7;      // ceil(count / 128)
//	countPadded = pages << 7;               // pages * 128
//	totalSize   = (pages + 2) * 0x200;      // (pages + 2) * 512
//
// FUN_00401AA0 also zeroes a 0x76-dword (472-byte) region before writing.
const (
	// NativePageEntries is the verified entries per index page: 128 (0x80).
	NativePageEntries = 128
	// NativePageSize is the verified index page size: 0x200 bytes.
	NativePageSize = 0x200
	// NativePageOverhead is the verified number of extra pages: 2.
	NativePageOverhead = 2
	// NativeHeaderZeroBytes is the verified zeroed region: 0x76 dwords.
	NativeHeaderZeroBytes = 0x76 * 4
)

// NativeSaveGeometry is the derived geometry of a native save container.
type NativeSaveGeometry struct {
	// Count is the verified entry count, 100.
	Count int
	// Pages is ceil(Count / 128).
	Pages int
	// CountPadded is Pages * 128.
	CountPadded int
	// TotalSize is (Pages + 2) * 512.
	TotalSize int
}

// ComputeNativeSaveGeometry applies the verified FUN_00401AA0 arithmetic.
func ComputeNativeSaveGeometry(count int) NativeSaveGeometry {
	if count < 1 {
		count = 1
	}
	pages := (count + NativePageEntries - 1) / NativePageEntries
	padded := pages * NativePageEntries
	return NativeSaveGeometry{
		Count:       count,
		Pages:       pages,
		CountPadded: padded,
		TotalSize:   (pages + NativePageOverhead) * NativePageSize,
	}
}

// Verified save section shapes recorded from FUN_00422DE0. These are the sizes
// the serializer emits for the sections the Go port can already reproduce.
const (
	// NativeSaveManagerRecordSize is the 0x104-byte record the serializer
	// appends for every open APPL manager. FUN_00422DE0 sizes it as
	// sVar4 * 0x104 and copies 0x41 dwords, which is exactly 260 bytes.
	NativeSaveManagerRecordSize = 0x104
	// NativeSaveManagerCopyDwords is the 0x41 dword copy loop width.
	NativeSaveManagerCopyDwords = 0x41
	// NativeSaveContextPages is the number of 256-byte script context pages the
	// serializer writes via FUN_00417820. The loop runs while the counter is
	// below 9, so it writes pages 0 through 8.
	NativeSaveContextPages = 9
	// NativeSaveContextPageSize is the verified 0x100-byte page size. This
	// matches the existing Go context page geometry.
	NativeSaveContextPageSize = 0x100
	// NativeSavePaletteEntries is the number of eight-byte palette entries the
	// serializer copies from DAT_004590FC. The loop runs while the index is
	// below 0x100, so it writes 256 entries of 8 bytes.
	NativeSavePaletteEntries = 256
	// NativeSavePaletteEntrySize is the verified eight-byte entry size.
	NativeSavePaletteEntrySize = 8
)

// NativeSaveSections returns the byte size of each reproducible save section.
func NativeSaveSections() (contextBytes, paletteBytes, managerRecordBytes int) {
	return NativeSaveContextPages * NativeSaveContextPageSize,
		NativeSavePaletteEntries * NativeSavePaletteEntrySize,
		NativeSaveManagerRecordSize
}

// SaveContainerHeader is the verified leading metadata of a native save
// container. Only the fields the evidence establishes are represented.
type SaveContainerHeader struct {
	TagPrimary   uint32
	TagSecondary uint32
	Geometry     NativeSaveGeometry
}

// BuildSaveContainerHeader returns the header for a native save container.
func BuildSaveContainerHeader() (SaveContainerHeader, error) {
	geometry := ComputeNativeSaveGeometry(NativeSaveEntryCount)
	// Verified: count 100 rounds up to a single 128-entry page, and the
	// container is (1 + 2) * 512 bytes.
	if geometry.Pages != 1 || geometry.CountPadded != NativePageEntries || geometry.TotalSize != 3*NativePageSize {
		return SaveContainerHeader{}, fmt.Errorf("native save geometry is not the verified single-page shape: %+v", geometry)
	}
	return SaveContainerHeader{
		TagPrimary:   NativeSaveTagPrimary,
		TagSecondary: NativeSaveTagSecondary,
		Geometry:     geometry,
	}, nil
}

// EncodeSaveContainerTags writes the two little-endian tags the native header
// writer emits, in the verified order.
func EncodeSaveContainerTags(header SaveContainerHeader) []byte {
	out := make([]byte, 0, 8)
	out = binary.LittleEndian.AppendUint32(out, header.TagPrimary)
	out = binary.LittleEndian.AppendUint32(out, header.TagSecondary)
	return out
}

// DecodeSaveContainerTags reads the two tags back and validates them against the
// verified values, so a mismatched file is rejected rather than misread.
func DecodeSaveContainerTags(data []byte) (SaveContainerHeader, error) {
	if len(data) < 8 {
		return SaveContainerHeader{}, fmt.Errorf("save container header needs 8 bytes, got %d", len(data))
	}
	header, err := BuildSaveContainerHeader()
	if err != nil {
		return SaveContainerHeader{}, err
	}
	primary := binary.LittleEndian.Uint32(data[0:4])
	secondary := binary.LittleEndian.Uint32(data[4:8])
	if primary != NativeSaveTagPrimary || secondary != NativeSaveTagSecondary {
		return SaveContainerHeader{}, fmt.Errorf(
			"save container tags are %08x %08x, want %08x %08x",
			primary, secondary, NativeSaveTagPrimary, NativeSaveTagSecondary)
	}
	header.TagPrimary, header.TagSecondary = primary, secondary
	return header, nil
}
