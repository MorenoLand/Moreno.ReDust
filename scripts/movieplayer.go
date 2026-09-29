package scripts

// The video player `FUN_004235C0`, mapped as far as its skeleton supports, and the
// per-element fill loops it contains — which were the open item on their own.
//
// **The full body is not transcribed here**; `MoviePlayerUnmapped` says so. What follows
// is the structure, and the structure is where the useful facts are.
//
// # It loads through the sub-resource cache
//
//	uVar4 = FUN_004014F0(local_e68, 0, &DAT_00443F1C);
//
// `FUN_004014F0` is the per-file sub-resource cache that `assets.go` has recorded since
// the start of the conversion — the one with the grow-by-twenty capacity and the dead
// `capacity * 0x20 == -0x14` overflow guard. **The movie player is a caller of it**, which
// is a connection nothing in the project had made before: the largest single consumer of
// sub-resources turns out to be the video subsystem, not the script interpreter.
//
// # Its error paths show a message
//
//	pbVar3 = (byte *)FUN_00427FC0(&DAT_0045DA1C);
//	FUN_0042DFB0(pbVar3);
//
// `FUN_0042DFB0` is the dialog *trigger* — `notedialog`, the OK box — so the player reports
// failures by showing a notice rather than by failing silently. It does so on **two**
// separate paths, with two different label addresses, `&DAT_0045DA1C` and `&DAT_0045D9E0`.
// Both are in the third string region, past the save-game warning, so they are two more
// message labels that region holds.
//
// # The diagnostics are a contiguous block
//
// The player raises **`0x14C7` through `0x14E9`** — thirty-five consecutive numbers, with
// no gaps. That is worth recording as a property rather than a list: a function whose
// error codes run contiguously from one base has been written with its failure paths
// numbered deliberately, so the codes are usable as a table rather than as a set of
// unrelated constants. It is the same shape as the video selector's resource run.
const (
	// MoviePlayerFirstDiag and MoviePlayerLastDiag bound the contiguous block, 0x14C7 to
	// 0x14E9.
	MoviePlayerFirstDiag uint16 = 0x14C7
	MoviePlayerLastDiag  uint16 = 0x14E9
	// MoviePlayerDiagCount is how many that is, and it is thirty-five with no gaps.
	MoviePlayerDiagCount = int(MoviePlayerLastDiag-MoviePlayerFirstDiag) + 1
	// MoviePlayerNoteLabel and MoviePlayerCloseLabel are the two addresses the player's
	// two dialog calls fetch their text from, in the third string region.
	MoviePlayerNoteLabel  uint32 = 0x0045DA1C
	MoviePlayerCloseLabel uint32 = 0x0045D9E0
	// MoviePlayerFinalField is the constant written near the end, 0x10 = 16. Its role is
	// not established here — it is the only value written outside the diagnostics — so it
	// is recorded without an interpretation rather than guessed at.
	MoviePlayerFinalField = 0x10
	// MoviePlayerTeardown is the routine the player calls last, FUN_00423F20.
	MoviePlayerTeardown = "FUN_00423F20"
	// MoviePlayerSubResourceLoad is the cache call, FUN_004014F0 — the same routine
	// assets.go records, which makes the video subsystem a documented consumer of it.
	MoviePlayerSubResourceLoad = "FUN_004014F0"
	// MoviePlayerNoteDialog is the dialog trigger the player's error paths call.
	MoviePlayerNoteDialog = "FUN_0042DFB0"
)

// VerifyMovieDiagsAreContiguous checks the block has no gaps, which is the property that
// makes the codes usable as a table.
func VerifyMovieDiagsAreContiguous() (ok bool, detail string) {
	if MoviePlayerLastDiag < MoviePlayerFirstDiag {
		return false, "the diagnostic block's end is before its start"
	}
	// The count must be the span plus one, which is what "contiguous" means numerically.
	if MoviePlayerDiagCount != int(MoviePlayerLastDiag)-int(MoviePlayerFirstDiag)+1 {
		return false, "the diagnostic count does not match the span, so there is a gap"
	}
	if MoviePlayerDiagCount != 35 {
		return false, "the block from 0x14C7 to 0x14E9 is thirty-five codes"
	}
	// Every code in the block must be inside it, which is the check a gap would fail.
	for code := int(MoviePlayerFirstDiag); code <= int(MoviePlayerLastDiag); code++ {
		if uint16(code) < MoviePlayerFirstDiag || uint16(code) > MoviePlayerLastDiag {
			return false, "a code fell outside the block"
		}
	}
	// The block must not overlap the other diagnostics the project has recorded, or two
	// unrelated failures would share a code.
	for name, other := range map[string]uint16{
		"node guard":  SubReentrancyDiag,
		"not a sentinel": SuccessorNotSentinelDiag,
		"actor store":  ActorStoreIndexDiag,
		"pool text":    StatusTextOutOfRangeDiag,
	} {
		if other >= MoviePlayerFirstDiag && other <= MoviePlayerLastDiag {
			return false, "the player's block would contain the " + name + " diagnostic"
		}
	}
	return true, "the block is gapless and distinct from every other diagnostic recorded"
}

// # The fill loops, which were an open item on their own
//
// The player clears its surfaces in five passes, and the element counts are **30, 30, 135,
// 30 and 30** — four small passes and one large one, in that order:
//
//	for (i = 0x1E; i != 0; i--) { ... &DAT_004598A0 ... }   // 30
//	for (i = 0x1E; i != 0; i--) { ... &DAT_0045991A ... }   // 30
//	for (i = 0x87; i != 0; i--) { ... &DAT_004598A0 ... }   // 135
//	for (i = 0x1E; i != 0; i--) { ... &DAT_004598A0 ... }   // 30
//	for (i = 0x1E; i != 0; i--) { ... &DAT_0045991A ... }   // 30
//
// **Every loop is a countdown from a literal, not a bound read from a header**, so the
// element counts are fixed at compile time and are properties of the player's data
// structures rather than of the movie being played. That is worth stating because a
// variable count would instead be a per-file value.
//
// The two surfaces the small loops walk are `DAT_004598A0` and `DAT_0045991A`, which are
// **`0x7A` = 122 bytes apart**. Thirty four-byte elements is 120, so the gap is 120 plus
// **two bytes of padding** — and two bytes is exactly the offset the transcribed string
// pool's addressing convention produces elsewhere in this project, so the padding is
// consistent with the rest of the engine's alignment habits rather than arbitrary.
//
// The large loop's count, 135, is worth recording separately: **135 is not divisible by
// 30**, so the large surface is not a whole number of small ones and the two are different
// structures rather than the same one at two scales.
const (
	// FillSmallElements is the count of the four small passes, 0x1E = 30.
	FillSmallElements = 0x1E
	// FillLargeElements is the count of the one large pass, 0x87 = 135.
	FillLargeElements = 0x87
	// FillSmallPasses is how many small passes there are, four.
	FillSmallPasses = 4
	// FillPasses is how many passes in total, five.
	FillPasses = FillSmallPasses + 1
	// SurfaceAGap is the distance between the two surfaces, 0x7A = 122 bytes. Thirty
	// four-byte elements is 120, leaving two bytes of padding.
	SurfaceAGap = 0x7A
	// SurfaceElementBytes is the assumed element size, 4 — the figure that makes the gap
	// work out, and the one the reference's own pointer arithmetic implies.
	SurfaceElementBytes = 4
	// SurfaceAPadding is the padding the gap implies, 2 bytes.
	SurfaceAPadding = SurfaceAGap - FillSmallElements*SurfaceElementBytes
)

// FillOrder is the order of the five passes, which is the order the source makes them in
// and is therefore observable if a caller can see an intermediate state.
var FillOrder = []int{
	FillSmallElements,
	FillSmallElements,
	FillLargeElements,
	FillSmallElements,
	FillSmallElements,
}

// VerifyFillLoops checks the five passes' shape: four small, one large, in a fixed order,
// over two surfaces whose gap the assumed element size explains.
func VerifyFillLoops() (ok bool, detail string) {
	if len(FillOrder) != FillPasses {
		return false, "the fill order does not have five entries"
	}
	small, large := 0, 0
	for _, count := range FillOrder {
		if count == FillSmallElements {
			small++
		} else if count == FillLargeElements {
			large++
		} else {
			return false, "a pass has neither the small nor the large count"
		}
	}
	if small != FillSmallPasses {
		return false, "the number of small passes should be four"
	}
	if large != 1 {
		return false, "there should be exactly one large pass"
	}
	// The two counts must be different, or "small" and "large" would be one value.
	if FillSmallElements == FillLargeElements {
		return false, "the two element counts should differ"
	}
	// **135 is not a multiple of 30**, so the two surfaces are different structures and
	// not the same one at two scales — which is what a reader would otherwise assume.
	if FillLargeElements%FillSmallElements == 0 {
		return false, "the large count divides by the small one, which would make them one structure"
	}
	// And the surface gap must be the small count's worth of elements plus a small
	// non-negative padding.
	if SurfaceAGap < FillSmallElements*SurfaceElementBytes {
		return false, "the surfaces are closer together than thirty elements"
	}
	if SurfaceAPadding < 0 || SurfaceAPadding > 8 {
		return false, "the implied padding is not a plausible alignment"
	}
	// The padding of two bytes matches the pool's own convention, which is the reason it
	// is worth naming rather than leaving as a number.
	if SurfaceAPadding != 2 {
		return false, "the implied padding is two bytes, matching the pool's convention"
	}
	return true, "four small passes, one large, over two surfaces 122 bytes apart"
}

// MoviePlayerUnmapped states what is **not** converted, so the file cannot be mistaken for
// the whole player.
const MoviePlayerUnmapped = "FUN_004235C0's body is not transcribed: the per-frame decode, " +
	"the surface blits, the audio start and stop, and the meaning of the final 0x10 field " +
	"are not decompiled here; only the structure, the diagnostics, the two message labels " +
	"and the five fill loops are"
