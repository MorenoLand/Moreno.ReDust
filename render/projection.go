package render

import (
	"image"
	"math"
)

// NativeQ14Scale is the Q14 denominator, 1 << 14, matching the shift in FUN_004064B0.
const NativeQ14Scale = 1 << 14

// NativeActorFocalLength is the projection's focal length. FUN_004055E0 and FUN_004057D0
// both end by assigning 0x136 to DAT_00459A58, and 0x136 is 310, which is the constant
// projectWorldActor already uses. It is read out of the binary, not fitted.
const NativeActorFocalLength = 0x136

// NativeQ14Trig returns sin and cos of a bearing on the 256-unit circle, in Q14.
//
// The circle is FUN_00411420's: bearing 0 is +y, 64 is +x, 128 is -y and 192 is -x, with
// the diagonals at 32, 96, 160 and 224. The reference reads DAT_004423B4 for the sine table
// and DAT_0044224C for the cosine, each 256 entries indexed by the bearing masked to a byte,
// but FUN_004055E0 allocates those at startup rather than storing them in the image, so they
// are computed here instead of transcribed.
func NativeQ14Trig(bearing int16) (int32, int32) {
	// 256 units to a full turn, so each unit is 2 * (2*pi) / 256 radians.
	theta := float64(int(bearing)&0xff) * (2 * math.Pi) / 256.0
	return q14FromUnit(math.Sin(theta)), q14FromUnit(math.Cos(theta))
}

// q14FromUnit converts a fraction to Q14. The conversion truncates toward zero, which is
// what the reference's signed shift does: a value of -0.9999 must yield -16383 and not
// -16384, or the fixed-point division would round away from zero where C rounds toward it.
func q14FromUnit(value float64) int32 {
	return int32(value * NativeQ14Scale)
}

// q14Shift is the reference's Q14 reduction: shift right by 14 after adding back the low 14
// bits when the value is negative. That is what makes the division round toward zero, as C
// integer division does, rather than down.
func q14Shift(value int32) int32 {
	return (value + (value >> 31 & 0x3fff)) >> 14
}

// NativeProjection is the result of FUN_004064B0: where a world point lands on screen, and
// the signed forward depth that the cull in FUN_00421820 and FUN_0040DB70 is written
// against.
type NativeProjection struct {
	// Screen is the projected position of the point, in screen coordinates.
	Screen image.Point
	// Depth is the signed forward distance in world units. It is negative behind the
	// camera, and the reference rejects anything below 0x20 on its near plane.
	Depth int
	// Behind reports that the point was at or behind the camera plane, where the reference
	// returns before computing a screen position at all.
	Behind bool
}

// NativeProjectQ14 is a transcription of FUN_004064B0, the reference's world-to-screen
// projection. Verified against the binary:
//
//	iVar4 = object.x - DAT_00459A78;                       // camera x
//	iVar2 = object.y - DAT_00459A7A;                       // camera y
//	iVar3 = iVar2 * DAT_00442330 - iVar4 * DAT_00442348;    // dy*sin - dx*cos, lateral
//	iVar2 = iVar2 * DAT_00442348 + iVar4 * DAT_00442330;    // dy*cos + dx*sin, depth
//	both reduced by the sign-preserving (v + (v >> 31 & 0x3fff)) >> 14
//	if (depth < 1) return;                                 // behind, no screen point
//	screenX = (lateral * DAT_00459A58) / depth + DAT_00459A5C;
//	screenY = DAT_00459A5A - ((object.z - DAT_00459A7C) * DAT_00459A58) / depth;
//
// The caller supplies the four globals the reference keeps in DAT_00459A5A through A7C: the
// focal length, the screen centre and the camera position. DAT_00459A58 is 0x136, which is
// 310, the value projectWorldActor already uses, so the focal length is not a fitted number.
func NativeProjectQ14(
	objectX, objectY, objectZ int,
	cameraX, cameraY, cameraZ int,
	bearing int16,
	focal, centreX, centreY int,
) NativeProjection {
	sin, cos := NativeQ14Trig(bearing)
	deltaX := int32(objectX - cameraX)
	deltaY := int32(objectY - cameraY)

	lateralQ14 := deltaY*sin - deltaX*cos
	depthQ14 := deltaY*cos + deltaX*sin
	depth := int(q14Shift(depthQ14))

	if depth < 1 {
		// The reference returns here, before touching the screen centre, having produced
		// no usable screen position.
		return NativeProjection{Depth: depth, Behind: true}
	}
	lateral := int(q14Shift(lateralQ14))
	screenX := (lateral*focal)/depth + centreX
	screenY := centreY - ((objectZ-cameraZ)*focal)/depth
	return NativeProjection{Screen: image.Point{X: screenX, Y: screenY}, Depth: depth}
}
