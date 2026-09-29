package scripts

// The keyword table's *physical* layout, verified against the image, and the fourth
// string region the pointers name. `keywords.go` records the thirty-one names and their
// scan order; this file records how they are stored, which is a separate fact and the one
// that makes the recorded order checkable.

// # The pointer array
//
// The thirty-one names are not stored inline. They live in a fourth string region, and
// `0x0045D738` holds an array of **pointers** to them.
//
// The stride is **six bytes** — a four-byte pointer followed by two zero bytes — which is
// the layout `keywords.go` already records as `KeywordSecondPointerOffset`. That is worth
// restating because the stride is not any of the three a reader would try: not four (a
// plain pointer array), not twelve (a three-DWORD struct), and not one (which would be
// the case table). Six is a pointer plus two bytes of padding, and reading the array at
// any other stride produces plausible-looking garbage — which is exactly what happened
// twice while verifying this.
const (
	// KeywordPointerStride is six bytes: a pointer and two bytes of padding.
	KeywordPointerStride = 6
	// KeywordPointerBytes is how much of each entry the pointer occupies, four.
	KeywordPointerBytes = 4
	// KeywordPointerPad is the padding after it, two zero bytes. **A test asserts these
	// two are zero**, because padding that held data would mean the stride is wrong and
	// the extra two bytes are part of a longer structure.
	KeywordPointerPad = 2
	// KeywordPointerCount is how many entries, thirty-one, matching the keyword count.
	KeywordPointerCount = KeywordCount
	// KeywordPointerSpan is the array's total size: thirty-one six-byte entries.
	KeywordPointerSpan = KeywordPointerCount * KeywordPointerStride
)

// The fourth string region, which the pointer array names. **The pointers descend**, so
// this array was written bottom-up relative to the pointer array above it — which is why
// the first keyword's string sits at the *highest* address of the thirty-one.
const (
	// KeywordStringRegionFirst is the first name the table names, "opencast". It is the
	// **highest** address in the region, not the lowest.
	KeywordStringRegionFirst uint32 = 0x0045D93C
	// KeywordStringRegionLast is the last name, "closescene", at the **lowest** address.
	KeywordStringRegionLast uint32 = 0x0045D7F4
	// KeywordStringRegionSpan is the distance from the lowest name to the highest, 0x148.
	KeywordStringRegionSpan = 0x148
	// KeywordStringRegionTail is how far the region extends **past** its low bound, 11
	// bytes: the last name's ten characters and its NUL. So the region's full extent is
	// the span plus the tail, and stating both is what keeps them from being confused.
	KeywordStringRegionTail = 11
	// KeywordArrayPadding is the gap between the pointer array's end and the lowest name.
	// The array's last six-byte entry ends at 0x0045D7F1 and the name begins at
	// 0x0045D7F4, so the gap is **two bytes** — the same width as an entry's own
	// padding, which is consistent with the array being written with a trailing pad
	// rather than the strings being moved.
	KeywordArrayPadding = 2
	// KeywordStringSlotLarge and KeywordStringSlotSmall are the two slot sizes the
	// strings are packed into: twelve bytes for the longer names and eight for the
	// shorter. **Neither is a multiple of the pointer stride**, so the two arrays are
	// packed independently and neither can be derived from the other.
	KeywordStringSlotLarge  = 12
	KeywordStringSlotSmall  = 8
	// KeywordStringMaxName is the longest name, "closeactor" and "closepuppet" at ten
	// characters, so a name plus its NUL needs eleven bytes and fits the large slot with
	// one to spare.
	KeywordStringMaxName = 10
)

// keywordStringAddresses is the address each of the thirty-one keywords' names sits at,
// in the same scan order as the keyword table. Read from the image; **descending**, which
// is the property worth having recorded.
var keywordStringAddresses = [KeywordCount]uint32{
	0x0045D93C, 0x0045D930, 0x0045D924, 0x0045D918, 0x0045D90C,
	0x0045D900, 0x0045D8F4, 0x0045D8E8, 0x0045D8E0, 0x0045D8D8,
	0x0045D8D0, 0x0045D8C8, 0x0045D8BC, 0x0045D8B0, 0x0045D8A4,
	0x0045D898, 0x0045D88C, 0x0045D880, 0x0045D874, 0x0045D86C,
	0x0045D864, 0x0045D858, 0x0045D850, 0x0045D844, 0x0045D838,
	0x0045D830, 0x0045D824, 0x0045D818, 0x0045D80C, 0x0045D800,
	0x0045D7F4,
}

// VerifyKeywordPointerLayout checks the array's geometry: the stride, the pointer width,
// the padding, and the count, and that the two arrays' bounds are where the image puts
// them.
func VerifyKeywordPointerLayout() (ok bool, detail string) {
	// **The stride is a pointer plus padding.** Both halves are checked, because a
	// stride of six is only meaningful if the extra two bytes are padding — if they held
	// data, the "pointer" would really be a longer structure at a different offset.
	if KeywordPointerStride != KeywordPointerBytes+KeywordPointerPad {
		return false, "the stride should be the pointer plus its padding"
	}
	if KeywordPointerBytes != 4 {
		return false, "a pointer should be four bytes on this target"
	}
	if KeywordPointerPad != 2 {
		return false, "the padding should be two bytes"
	}
	// The count matches the keyword count, so the array and the table are the same size.
	// **If they differed, either the table or the array would have entries the other
	// does not**, and the recorded names would be partly unreachable.
	if len(keywordStringAddresses) != KeywordCount {
		return false, "the address list should have one entry per keyword"
	}
	if KeywordPointerCount != KeywordCount {
		return false, "the pointer count should match the keyword count"
	}
	// The array begins where `keywords.go` says the table begins, and the two constants
	// in different files agreeing is worth asserting rather than assuming.
	if KeywordTableAddress != KeywordTableFirst {
		return false, "the table's address and its first entry should agree"
	}
	if KeywordTableAddress != 0x0045D738 {
		return false, "the table should begin at 0x0045D738"
	}
	// **The array ends two bytes before the lowest name**, so the two are separated by
	// exactly one entry's padding rather than abutting. A port that walked the array
	// expecting a name at the next address would read padding as text.
	if KeywordTableAddress+uint32(KeywordPointerSpan) !=
		KeywordStringRegionLast-uint32(KeywordArrayPadding) {
		return false, "the array should end two bytes before the lowest name"
	}
	if KeywordArrayPadding != KeywordPointerPad {
		return false, "the array's trailing pad should match an entry's own padding"
	}
	// **The array's span is 186 bytes**, which is thirty-one six-byte entries.
	if KeywordPointerSpan != 186 {
		return false, "the array should span 186 bytes"
	}
	return true, "thirty-one six-byte entries, each a pointer and two bytes of padding"
}

// VerifyKeywordStringAddresses checks the thirty-one addresses as a sequence: strictly
// descending, inside the region's stated bounds, and spaced by the two slot sizes.
func VerifyKeywordStringAddresses() (ok bool, detail string) {
	n := len(keywordStringAddresses)
	if n != KeywordCount {
		return false, "the address list should hold thirty-one entries"
	}
	// **Every address lies inside the region's stated bounds**, and the two bounds are
	// themselves in the array — the first element is the region's high bound and the
	// last is its low bound.
	if keywordStringAddresses[0] != KeywordStringRegionFirst {
		return false, "the first name should be at the region's high bound"
	}
	if keywordStringAddresses[n-1] != KeywordStringRegionLast {
		return false, "the last name should be at the region's low bound"
	}
	if KeywordStringRegionFirst <= KeywordStringRegionLast {
		return false, "the first name should sit above the last, since the pointers descend"
	}
	// **Strictly descending, always.** Every step moves down, and none repeats — so no two
	// keywords share a string, which is the property that makes a scan of this array a
	// lookup rather than a search.
	for i := 1; i < n; i++ {
		if keywordStringAddresses[i] >= keywordStringAddresses[i-1] {
			return false, "the addresses should strictly descend"
		}
	}
	// **Every step is one of the two slot sizes.** A third step size would mean the
	// strings are packed by a rule this slice has not found, so the test names the two
	// rather than just requiring them to be equal.
	small, large := 0, 0
	for i := 1; i < n; i++ {
		step := keywordStringAddresses[i-1] - keywordStringAddresses[i]
		switch step {
		case KeywordStringSlotSmall:
			small++
		case KeywordStringSlotLarge:
			large++
		default:
			return false, "a step is neither eight nor twelve bytes"
		}
		// And every step must accommodate the name at its **lower** address, since the
		// strings run downward. A step smaller than the name it holds would be an
		// overlap, which is how a mis-strided reading shows up.
		name := controlFlowKeywordText[i]
		if step < uint32(len(name)+1) {
			return false, "a step is too small for the name it holds"
		}
	}
	// **Both slot sizes are actually used**, so neither is a fiction. Thirty steps split
	// between them, and the counts are recorded because the ratio is a property of the
	// packing rather than an accident.
	if small == 0 || large == 0 {
		return false, "both slot sizes should be used"
	}
	if small+large != n-1 {
		return false, "the step counts should cover every step"
	}
	// The longest name needs eleven bytes and fits the large slot; the shortest need
	// fewer than the small slot, so a name of five characters could sit in either.
	if KeywordStringMaxName+1 > KeywordStringSlotLarge {
		return false, "the longest name should fit the large slot"
	}
	if KeywordStringMaxName+1 <= KeywordStringSlotSmall {
		return false, "the longest name should not fit the small slot, or the split is arbitrary"
	}
	// And the span is what the bounds imply, so the two constants cannot drift apart.
	if got := KeywordStringRegionFirst - KeywordStringRegionLast; got != KeywordStringRegionSpan {
		return false, "the region's span should be the distance between its bounds"
	}
	// And the tail is the last name's length plus its NUL, so the region's full extent
	// is the span plus the tail.
	if got := KeywordStringRegionTail; got != len(controlFlowKeywordText[KeywordCount-1])+1 {
		return false, "the tail should be the last name's length plus its NUL"
	}
	return true, "thirty-one descending addresses in eight- and twelve-byte slots"
}
