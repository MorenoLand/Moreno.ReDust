package scripts

import (
	"fmt"
)

// The rectangle clipper behind `pointinprop`, `FUN_0042E230`, mapped in full:
//
//	local_8 = *(u32 *)param_2;          // the second rectangle's x0,y0
//	local_4 = *(u32 *)(param_2 + 2);    // and its x1,y1
//	if (*param_2  < *param_1)  local_8  = (*param_1[0], local_8.y0);   // x0 = max
//	if (param_2[1] < param_1[1]) local_8  = (local_8.x0, param_1[1]);   // y0 = max
//	if (param_1[2] < param_2[2]) local_4  = (param_1[2], local_4.y1);   // x1 = min
//	if (param_1[3] < param_2[3]) local_4  = (local_4.x1, param_1[3]);   // y1 = min
//	if ((local_8.x0 < local_4.x1) && (local_8.y0 < local_4.y1)) {
//	    *param_3 = local_8; param_3[2] = local_4; return 1;
//	}
//	*param_3 = 0; param_3[1] = 0; return 0;
//
// So it is a **rectangle intersection**, and three properties of it are load-bearing.
//
// **Both emptiness tests are strict.** `x0 < x1` and `y0 < y1` mean a zero-width or
// zero-height intersection is *empty*, and so are rectangles that merely touch along an
// edge. A point-in-rectangle test that used `<=` would answer differently for a
// rectangle of zero width and for two abutting ones, which for a hit test means a
// click on a shared boundary is attributed to neither rectangle.
//
// **It clips, it does not merely test.** The result on success is the intersection
// rectangle, not a flag — so a caller that needs the overlap's extent gets it for free,
// and a caller that only wants the flag has to discard it.
//
// **On failure it writes two zeroed DWORDs** rather than leaving the output alone, so
// the out-parameter is always fully written and a caller cannot observe a stale
// rectangle from a previous call. That is worth stating because "not written on the
// failure path" is the more common convention and would leave the two behaviours
// equivalent only by accident.
const (
	// RectShorts is a rectangle's size in shorts, four.
	RectShorts = 4
	// RectBytes is a rectangle's size in bytes, eight, which is two DWORDs.
	RectBytes = RectShorts * 2
	// RectDwords is a rectangle's size in DWORDs, two.
	RectDwords = 2
)

// The `Rect` type is **not** declared here: `hittest.go` already has one, with the
// same four fields in the same order, and it was derived from a *different* function —
// `FUN_0042E340`, the hit test, rather than this clipper. The two agreeing on the field
// order is a cross-check rather than a coincidence, and having one type for both is
// what makes it possible to assert that they agree on the geometry as well.

// RectFromShorts builds a Rect from the four shorts in the reference's order, which is
// how a caller reading a property row's bounds converts them.
func RectFromShorts(s [RectShorts]int16) Rect {
	return Rect{Left: s[0], Top: s[1], Right: s[2], Bottom: s[3]}
}

// Shorts returns the rectangle as the four shorts the reference reads.
func (r Rect) Shorts() [RectShorts]int16 {
	return [RectShorts]int16{r.Left, r.Top, r.Right, r.Bottom}
}

// IsEmpty reports whether the rectangle fails the clipper's strict test, which is the
// only emptiness definition the engine has. A zero-area rectangle and a rectangle whose
// right is left of its left are both empty, and a rectangle of zero width is empty
// rather than a line.
func (r Rect) IsEmpty() bool {
	return !(r.Left < r.Right && r.Top < r.Bottom)
}

// Width is the rectangle's extent in x, which is **negative for an inverted rectangle**
// rather than clamped, because the reference does not normalise its inputs.
func (r Rect) Width() int32 { return int32(r.Right) - int32(r.Left) }

// Height is the rectangle's extent in y, negative for an inverted one.
func (r Rect) Height() int32 { return int32(r.Bottom) - int32(r.Top) }

// ClipRect reproduces FUN_0042E230. It returns the intersection and whether it is
// non-empty, and **writes a zeroed rectangle on the failure path**, which is what the
// reference does and what a caller can rely on when it discards the geometry.
func ClipRect(a, b Rect) (Rect, bool) {
	// The intersection's lower bounds are the maxima and its upper bounds the minima.
	result := Rect{
		Left:   a.Left,
		Top:    a.Top,
		Right:  b.Right,
		Bottom: b.Bottom,
	}
	if b.Left > result.Left {
		result.Left = b.Left
	}
	if b.Top > result.Top {
		result.Top = b.Top
	}
	if a.Right < result.Right {
		result.Right = a.Right
	}
	if a.Bottom < result.Bottom {
		result.Bottom = a.Bottom
	}
	// **Both tests are strict**, so a zero-area overlap is empty and so is an edge
	// that merely touches.
	if result.Left < result.Right && result.Top < result.Bottom {
		return result, true
	}
	// The failure path zeroes the whole out-parameter rather than leaving it.
	return Rect{}, false
}

// **The clipper cannot answer a point query, and that is a real architectural fact
// rather than a gap in the conversion.** Its emptiness test is `x0 < x1 && y0 < y1` —
// an **area** test — so a degenerate rectangle of zero width has zero area and is
// always empty. A point is exactly such a rectangle, so clipping a rectangle against a
// point always reports empty and carries no information about containment. I first
// "modelled" a point query through the clipper and its own test showed the modelling was
// vacuous: the wrapper answered *outside for every point*, including points plainly
// inside. The wrapper is removed rather than fixed.
//
// So the two functions have **different emptiness semantics**, and the difference is
// worth recording because a port that conflated them would mis-handle degenerate
// rectangles in opposite ways:
//
//   - The **clipper** is area-based: `x0 < x1 && y0 < y1`. Zero width, zero height and
//     a single point are all empty.
//   - The **hit test** `FUN_0042E340` is interval-based and half-open: the left and top
//     edges are inclusive and the right and bottom exclusive, so a zero-width rectangle
//     contains the points on its left edge.
//
// `pointinprop` and `pointinset` therefore resolve to the **hit test**, not to this
// clipper, and the clipper serves only its rectangle-intersection callers.
const (
	// PointAsRectQuery is meaningless through the clipper, and is named only so the
	// reason is greppable. A point collapses to a zero-area rectangle, which the
	// clipper's strict area test always reports empty.
	PointAsRectQuery = false
)

// ClipRectShorts is the reference's own signature, taking and returning raw shorts, for
// a caller that already has the four shorts rather than a Rect.
func ClipRectShorts(first, second [RectShorts]int16) ([RectShorts]int16, uint16) {
	result, ok := ClipRect(RectFromShorts(first), RectFromShorts(second))
	shorts := result.Shorts()
	if !ok {
		// The failure path writes zeros into both DWORDs, which is eight bytes, so the
		// whole four-short result is zeroed rather than just flagged.
		return [RectShorts]int16{}, 0
	}
	return shorts, 1
}

// VerifyRectGeometry checks the geometry the reference's copy loop and out-parameter
// imply: a rectangle is two DWORDs, and the failure path writes exactly that many.
func VerifyRectGeometry() error {
	if RectDwords != RectBytes/4 {
		return fmt.Errorf("a rectangle is %d DWORDs but %d bytes", RectDwords, RectBytes)
	}
	if RectBytes != 8 {
		return fmt.Errorf("a rectangle is %d bytes, want 8", RectBytes)
	}
	if RectShorts != 4 {
		return fmt.Errorf("a rectangle is %d shorts, want 4", RectShorts)
	}
	return nil
}
