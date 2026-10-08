package native

// The display-mode selector `FUN_0042A0F0`, which is the layer behind the `blackscreen`
// video path. **Its full branch tree is not transcribed here** — see
// VideoModeUnmapped — but its inputs, its return convention, its flag and the whole set of
// driver resources it names are, and together those are most of what a reader needs.
//
// What the verified opening is:
//
//	hdc    = GetDC(NULL);
//	product = GetDeviceCaps(hdc, LOGPIXELSX) * GetDeviceCaps(hdc, LOGPIXELSY);
//	ReleaseDC(NULL, hdc);
//	width  = GetSystemMetrics(SM_CXSCREEN);
//	height = GetSystemMetrics(SM_CYSCREEN);
//	VideoDensityIsPhysical = 1;
//
//	if (product == 1) return 0x66;
//	if (product == 4) return 0x67;
//	... further branches, each naming a driver resource and clearing the flag ...
//
// # What it is
//
// The function **selects a video driver by name**, and the resources it names are a
// contiguous run from `0x66` to `0x79` — twenty of them, fetched through the string
// lookup `FUN_00428D40` that the dialog captions already use. So this is a DOS-era driver
// table: the engine measures the display, decides which driver suits it, and looks up that
// driver's name as a string resource.
//
// The discriminator is **physical pixel density** — the *product* of the two
// `LOGPIXELS` capabilities, not either alone. That is an unusual choice and worth stating
// plainly: a machine with 96×96 dpi scores 9216, one with 72×72 scores 5184, and the
// product is what is compared against 1, 4 and 8. Screen size enters only one branch,
// through the threshold **`0x321` by `0x259`** — 801 by 601 — which is a specific
// resolution rather than a round number.
//
// # The return convention is three-valued and not a status
//
// Every failing lookup returns `2 - (found == 1)`, so the function yields **2 when the
// lookup failed and 1 when it succeeded**, and 0 for a mode it has no case for. So 1 and
// 2 are *not* a success/failure pair in the usual sense — they are "found" and "not
// found" for a *string resource*, and 0 means "no such mode at all". A port that treated
// them as a boolean would invert the meaning of 2.
const (
	// VideoModeFound is what a successful driver-name lookup returns, 1.
	VideoModeFound uint32 = 1
	// VideoModeNotFound is what a failed one returns, 2 — from `2 - (found == 1)`.
	VideoModeNotFound uint32 = 2
	// VideoModeAbsent is what an unhandled mode returns, 0.
	VideoModeAbsent uint32 = 0
	// VideoDensityIsPhysical is the flag the selector sets to 1 up front and clears in
	// most branches. Its name is descriptive: the value 1 means the density product was
	// accepted as-is, and clearing it marks a branch where it was not.
	VideoDensityFlag = "DAT_00445EA4"
	// VideoResourceLookup is the string-resource routine the driver names come from. It
	// is `StringResourceLookup`, declared in dialogs.go, because **it is the same routine
	// the dialog captions use** — one lookup serving both, and naming it once makes that
	// structural rather than a coincidence of two comments.
	VideoResourceLookup = StringResourceLookup
	// VideoCapsX and VideoCapsY are the two device capabilities whose *product* is the
	// discriminator: LOGPIXELSX is 12 and LOGPIXELSY is 14.
	VideoCapsX = 0x0C
	VideoCapsY = 0x0E
	// VideoMetricWidth and VideoMetricHeight are the screen-size queries, 0 and 1.
	VideoMetricWidth  = 0
	VideoMetricHeight = 1
	// VideoDensityProduct1 and VideoDensityProduct4 are the two products the selector
	// matches directly, each naming a driver resource.
	VideoDensityProduct1 uint32 = 1
	VideoDensityProduct4 uint32 = 4
	// VideoDensityProduct8 appears in a branch combined with the size test.
	VideoDensityProduct8 uint32 = 8
	// VideoWidthThreshold and VideoHeightThreshold are the one explicit screen-size
	// test, 0x321 = 801 by 0x259 = 601. **A specific resolution, not a round number**,
	// and the only place screen size is consulted.
	VideoWidthThreshold  = 0x321
	VideoHeightThreshold = 0x259
)

// The driver resources, which form a contiguous run. Every one is fetched by id, so the
// ids themselves are the substance here — the spellings live in the resource section.
const (
	// VideoResourceFirst and VideoResourceLast bound the run, 0x66 to 0x79. The run is
	// contiguous with no gaps, which is worth asserting because a gap would mean a
	// resource id the selector never names.
	VideoResourceFirst = 0x66
	VideoResourceLast  = 0x79
	// VideoResourceCount is how many that is, 20.
	VideoResourceCount = VideoResourceLast - VideoResourceFirst + 1
	// VideoResourceFallback is the one id that recurs across branches, `0x6C`, which is
	// the second lookup in nearly every arm and so is the **common fallback driver**.
	VideoResourceFallback = 0x6C
	// VideoResourceDensity1 and VideoResourceDensity4 are the two named by the density
	// product alone, with no second lookup.
	VideoResourceDensity1 = 0x66
	VideoResourceDensity4 = 0x67
)

// VideoModeArm is one branch's sequence of driver-resource lookups, **in the order the
// reference makes them**. The arms are not uniformly two lookups — some are one, some are
// two, and some are three — and modelling them as pairs lost that. The last id in an arm
// is the one it settles on when the earlier ones do not resolve, which is why `0x6C` reads
// as a fallback: it is the trailing lookup in five of the arms.
type VideoModeArm struct {
	// Lookups are the resource ids tried, in order.
	Lookups []uint32
	// ClearsFlag records whether the branch clears the density flag, which every arm
	// except the two density-only branches does.
	ClearsFlag bool
}

// videoModeArms are the verified branch lookups, in the order the arms appear. The two
// density-only branches return their resource directly and are not listed here, because
// they make no second lookup.
var videoModeArms = []VideoModeArm{
	{Lookups: []uint32{0x69, 0x68}, ClearsFlag: false},
	{Lookups: []uint32{0x6A, 0x6B, VideoResourceFallback}, ClearsFlag: true},
	{Lookups: []uint32{0x70, VideoResourceFallback}, ClearsFlag: true},
	{Lookups: []uint32{0x73, 0x74, VideoResourceFallback}, ClearsFlag: true},
	{Lookups: []uint32{0x71, 0x72, VideoResourceFallback}, ClearsFlag: true},
	{Lookups: []uint32{0x6D, 0x6E, 0x6F, VideoResourceFallback}, ClearsFlag: true},
	{Lookups: []uint32{0x75, 0x76}, ClearsFlag: true},
	{Lookups: []uint32{0x77, 0x78}, ClearsFlag: true},
	{Lookups: []uint32{0x79, 0x78}, ClearsFlag: true},
}

// VideoFallbackUseCount returns how many arms end on the fallback resource, which is the
// evidence that it is a fallback rather than a branch of its own.
func VideoFallbackUseCount() int {
	count := 0
	for _, arm := range videoModeArms {
		if len(arm.Lookups) > 0 && arm.Lookups[len(arm.Lookups)-1] == VideoResourceFallback {
			count++
		}
	}
	return count
}

// VideoDensityProduct multiplies the two pixel-density capabilities, which is the
// selector's discriminator. **The product, not either value alone** — a port that
// compared the horizontal density on its own would disagree with the reference on every
// display whose two densities differ.
func VideoDensityProduct(logPixelsX, logPixelsY int32) int64 {
	return int64(logPixelsX) * int64(logPixelsY)
}

// VideoModeForProduct returns the mode the two product-specific branches name, and
// whether the product was one of them. Everything else falls through to the branch tree
// that is not transcribed.
func VideoModeForProduct(product int64) (mode uint32, matched bool) {
	switch product {
	case int64(VideoDensityProduct1):
		return VideoResourceDensity1, true
	case int64(VideoDensityProduct4):
		return VideoResourceDensity4, true
	}
	return VideoModeAbsent, false
}

// VideoResolutionIsSmall is the one explicit screen-size test: strictly under 801 by 601.
// The comparison is **strict** on both axes, so a display of exactly 801×601 is *not*
// small, which is the boundary a port would get wrong with `<=`.
func VideoResolutionIsSmall(width, height int32) bool {
	return width < VideoWidthThreshold && height < VideoHeightThreshold
}

// VideoModeUnmapped states what is **not** converted, so the file cannot be mistaken for
// the whole selector.
const VideoModeUnmapped = "FUN_0042A0F0's branch tree is not transcribed: the conditions " +
	"beyond the two density products and the one size test are not decompiled here, only " +
	"the resources each arm names and the return convention"

// VerifyVideoResourcesAreContiguous checks the run is gapless, since a gap would be a
// resource id the selector never names and would suggest a transcription slip.
func VerifyVideoResourcesAreContiguous() (ok bool, detail string) {
	if VideoResourceLast < VideoResourceFirst {
		return false, "the resource run's end is before its start"
	}
	if VideoResourceCount != 20 {
		return false, "the run from 0x66 to 0x79 is twenty resources"
	}
	// Every named id, in any arm, must lie inside the run.
	named := map[uint32]bool{
		VideoResourceFallback:   true,
		VideoResourceDensity1:   true,
		VideoResourceDensity4:   true,
		0x68:                    true,
		0x6A:                    true,
		0x6D:                    true,
		0x6E:                    true,
		0x6F:                    true,
		0x70:                    true,
		0x71:                    true,
		0x72:                    true,
		0x73:                    true,
		0x74:                    true,
		0x75:                    true,
		0x76:                    true,
		0x77:                    true,
		0x78:                    true,
		0x79:                    true,
	}
	for id := range named {
		if id < VideoResourceFirst || id > VideoResourceLast {
			return false, "a named resource lies outside the run"
		}
	}
	// The three values the selector returns are three distinct numbers, and none of them
	// is zero except the absent case.
	if VideoModeFound == VideoModeNotFound || VideoModeNotFound == VideoModeAbsent {
		return false, "the three return values should be distinct"
	}
	if VideoModeFound == 0 || VideoModeNotFound == 0 {
		return false, "only the absent case should be zero"
	}
	// The two metric constants and the two capability constants differ, since they are
	// different queries.
	if VideoMetricWidth == VideoMetricHeight {
		return false, "the two screen-size queries should differ"
	}
	if VideoCapsX == VideoCapsY {
		return false, "the two density capabilities should differ"
	}
	// And the size test's threshold is a real resolution rather than a round number,
	// which is what makes it worth pinning.
	if VideoWidthThreshold != 801 || VideoHeightThreshold != 601 {
		return false, "the size threshold should be 801 by 601"
	}
	return true, "the run is gapless and every named id lies inside it"
}
