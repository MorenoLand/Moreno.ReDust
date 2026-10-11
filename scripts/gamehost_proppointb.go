package scripts

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"redust/render"
)

// CameraState is what path B of pointinprop reads from the camera and the open set:
// the camera's world position (A78, A7A and A7C), its heading (A6A), the pullback
// (A4C), the set name (DAT_00459A82) and the set's pointer rectangle.
type CameraState struct {
	Position [3]int16
	Heading  int16
	Pullback int16
	SetName  string
	Pointer  Rect
}

// Path B's constants are the reference's, as FUN_004055E0 and FUN_004057D0 set them. The
// focal length is 0x136. The screen centre is half the stage view: FUN_004055E0 takes
// DAT_00459A5C and A5A from (right - left) / 2 and (bottom - top) / 2 of the stage view,
// which is StageViewRect (512 by 384), so the centre is (256, 192).
var (
	pathBCentreX = int(StageViewRect.Right-StageViewRect.Left) / 2
	pathBCentreY = int(StageViewRect.Bottom-StageViewRect.Top) / 2
)

// Native walk faults of FUN_004396F0 (the scaled walker). They are raised through
// FUN_0042C470 and cannot be reached for a point inside the box, so they are errors.
var (
	errPropScaleTooLarge        = errors.New("prop scaled walk scale is over 0x7ffffff (native 0x204)")
	errPropScaledColumnNotFound = errors.New("prop scaled walk ran past the frame's columns (native 0x205)")
)

// projectedPropHit is path B of FUN_00421820 with the hit test of FUN_0041FA40, for a
// projected record (word 9 non-zero). The reference runs these steps in order, and each
// false answer ends the test:
//
//	DAT_00459A24 and DAT_00459A26 both set (the set is open and visible)
//	FUN_0042E5B0(owner, DAT_00459A82): the owner equals the open set's name
//	FUN_004064B0 projects the prop: depth < 0x20 is false
//	theta = FUN_00411420(camera y - prop y, camera x - prop x), the bearing to the camera
//	sVar8 = clamp0(depth - ZClip - A4C + 0x80), false when over 0x600
//	FUN_00421B70 selects the variant for angle (word 12 - A6A + A76 - theta) & 0xFF
//	scale = (Scale * Metric) / 1000, scaled width and height are scale * extent / depth
//	the box is the projected anchor less the origin, clipped to the set's pointer rectangle
//	FUN_0042E340: the point must be inside the clipped box
//	FUN_004396F0, the scaled walker, with the flip word sVar8 >> 6 and the scale
//
// The reference also stores the depth in word 20 and steps the frame cursor inside
// FUN_00421B70. Neither write is replicated here, so pointinprop does not change the record.
func (h *GameHost) projectedPropHit(prop PropPointRecord, point uint32) (bool, error) {
	_, open := h.setRect()
	if !PointInSetWillTest(SetState{Active: open, Visible: h.setVisible}) {
		return false, nil
	}
	if h.Env.CameraState == nil || prop.FrameAt == nil {
		return false, fmt.Errorf("%w: pointinprop of a projected prop (word 9 != 0) with a set open", ErrHostOpcodeUnimplemented)
	}
	camera := h.Env.CameraState()
	if !strings.EqualFold(prop.Owner, camera.SetName) {
		return false, nil
	}
	heading := camera.Heading
	projection := render.NativeProjectQ14(
		int(prop.Position[0]), int(prop.Position[1]), int(prop.Position[2]),
		int(camera.Position[0]), int(camera.Position[1]), int(camera.Position[2]),
		heading, render.NativeActorFocalLength, pathBCentreX, pathBCentreY)
	depth := int16(projection.Depth)
	if depth < 0x20 {
		return false, nil
	}
	theta := render.NativeAngle(camera.Position[1]-prop.Position[1], camera.Position[0]-prop.Position[0])
	// (w12 - A6A + A76 - theta) & 0xFF. A76 is A6A, so the heading cancels.
	angle := int16(uint8(prop.Degree - heading + heading - theta))
	gate := depth - prop.ZClip - camera.Pullback + 0x80
	if gate < 0 {
		gate = 0
	}
	if gate > 0x600 {
		return false, nil
	}
	frame, err := prop.FrameAt(angle)
	if err != nil {
		return false, err
	}
	if frame == nil {
		return false, nil
	}
	info := frame.Info
	extentX := info.Right - info.Left
	extentY := info.Bottom - info.Top
	if extentX == 0 || extentY == 0 {
		return false, fmt.Errorf("%w: pointinprop of a projected prop with an empty frame extent (native divides by zero)", ErrHostOpcodeUnimplemented)
	}
	scale := int32(int16((prop.Scale * int32(int16(info.Metric))) / 1000))
	scaledX := int16((scale * int32(extentX)) / int32(depth))
	scaledY := int16((scale * int32(extentY)) / int32(depth))
	offsetX := int16((int32(info.OriginX) * int32(scaledX)) / int32(extentX))
	offsetY := int16((int32(info.OriginY) * int32(scaledY)) / int32(extentY))
	anchorX, anchorY := int16(projection.Screen.X), int16(projection.Screen.Y)
	top, left := anchorY-offsetY, anchorX-offsetX
	box := Rect{Top: top, Left: left, Bottom: top + scaledY, Right: left + scaledX}
	clipped, inside := ClipRect(camera.Pointer, box)
	if !inside || !ContainsPoint(PointFromPacked(point), clipped) {
		return false, nil
	}
	anchor := uint32(uint16(anchorY)) | uint32(uint16(anchorX))<<16
	scaleWord := int32(uint32(uint16(scaledY)) | uint32(uint16(scaledX))<<16)
	// Word 7 is sVar8 >> 6, the clamped gate's shift, and FUN_004396F0 reads it as its flip.
	return propFrameHitScaled(frame.Data, anchor, gate>>6, scaleWord, point)
}

// propFrameHitScaled is FUN_004396F0, the scaled walker that path B's pointinprop uses.
// The anchor is the projected screen point (low word y, high word x). The scale is the
// dword of the scaled size (low word y, high word x). A non-zero flip mirrors the frame's
// origin along the rows. A scale below 1 in either word is false, as the reference returns.
//
// The frame box is the anchor less the origin scaled to the size, with the frame header's
// column and row counts as the divisors. A point outside it is false. Otherwise the column
// is the point's y less the box's top, scaled by columns over the scale's y, and the row is
// its x less the box's left, scaled by rows over the scale's x. The column records are walked
// as in propFrameHit, and the run walk propRunHit answers the row.
func propFrameHitScaled(frame []byte, anchor uint32, flip int16, scale int32, point uint32) (bool, error) {
	if len(frame) < propFrameHeaderBytes {
		return false, fmt.Errorf("prop frame resource is %d bytes, shorter than its %d byte header", len(frame), propFrameHeaderBytes)
	}
	scaleY, scaleX := int16(scale), int16(scale>>16)
	if scaleY < 1 || scale < 0x10000 {
		return false, nil
	}
	if scale > 0x7ffffff {
		return false, errPropScaleTooLarge
	}
	columns := int16(binary.LittleEndian.Uint16(frame[0:2]))
	rows := int16(binary.LittleEndian.Uint16(frame[2:4]))
	originLow := int16(binary.LittleEndian.Uint16(frame[4:6]))
	originHigh := int16(binary.LittleEndian.Uint16(frame[6:8]))
	if flip != 0 {
		originHigh = rows - originHigh
	}
	top := int16(uint16(anchor)) - int16((int32(originLow)*int32(scaleY))/int32(columns))
	left := int16(uint16(anchor>>16)) - int16((int32(originHigh)*int32(scaleX))/int32(rows))
	box := Rect{Top: top, Left: left, Bottom: top + scaleY, Right: left + scaleX}
	p := PointFromPacked(point)
	if !ContainsPoint(p, box) {
		return false, nil
	}
	counter := uint16(int32(int16(p.Y-top)) * int32(columns) / int32(scaleY))
	pos := propFrameHeaderBytes
	for index := int16(0); index < columns; index++ {
		if int16(counter) < 1 {
			row := int16((int32(int16(p.X-left)) * int32(rows)) / int32(scaleX))
			return propRunHit(frame, pos+2, rows, row)
		}
		counter--
		if pos+2 > len(frame) {
			return false, errPropColumnTruncated
		}
		length := int16(binary.LittleEndian.Uint16(frame[pos : pos+2]))
		pos += 2 + int(length)
	}
	return false, errPropScaledColumnNotFound
}
