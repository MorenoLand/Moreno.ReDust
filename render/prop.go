package render

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"

	"redust/assets"
)

// Props are drawn as their frame stores them. The frame's own width and height
// are the display size. The variant row names its extents in the native order
// (see assets.PropFrameInfo): Right-Left is the width along x and Bottom-Top the
// height along y, and the origin is (OriginX, OriginY). HOUSE.PRP, INVEN.PRP and
// CREDITS.PRP props all follow this.
type WorldPropSprite struct {
	Name     string
	Set      string
	Position [3]int16
	Heading  int16
	Scale    int16
	ZClip    int16
	Frame    int
	Archive  *assets.PropArchive
	View     assets.PropView
	Visible  bool
}

type FlatPropSprite struct {
	Name     string
	PropName string
	ViewName string
	Anchor   image.Point
	Archive  *assets.PropArchive
	Frame    int
}

type ProjectedFlatProp struct {
	Name   string
	Bounds image.Rectangle
	pixels []byte
	mask   []bool
}

func CompositeFlatProps(background IndexedFrame, props []FlatPropSprite) (IndexedFrame, []ProjectedFlatProp, error) {
	if background.Width < 1 || background.Height < 1 || len(background.Palette) != 256 {
		return IndexedFrame{}, nil, fmt.Errorf("flat prop background is invalid")
	}
	base, err := background.rgbaImage()
	if err != nil {
		return IndexedFrame{}, nil, err
	}
	composite := image.NewRGBA(base.Bounds())
	draw.Draw(composite, composite.Bounds(), base, image.Point{}, draw.Src)
	projected := make([]ProjectedFlatProp, 0, len(props))
	for _, prop := range props {
		if prop.Archive == nil {
			return IndexedFrame{}, nil, fmt.Errorf("flat prop %q has no archive", prop.Name)
		}
		info, err := prop.Archive.FrameInfo(prop.PropName, prop.ViewName, prop.Frame, 0)
		if err != nil {
			return IndexedFrame{}, nil, fmt.Errorf("select flat prop %q frame: %w", prop.Name, err)
		}
		data, err := prop.Archive.Resource(info.Resource)
		if err != nil {
			return IndexedFrame{}, nil, fmt.Errorf("read flat prop %q frame %d: %w", prop.Name, info.Resource, err)
		}
		frame, err := DecodePuppetFrame(data)
		if err != nil {
			return IndexedFrame{}, nil, fmt.Errorf("decode flat prop %q frame %d: %w", prop.Name, info.Resource, err)
		}
		width, height := frame.Width, frame.Height
		if int(info.Right-info.Left) != width || int(info.Bottom-info.Top) != height {
			return IndexedFrame{}, nil, fmt.Errorf("flat prop %q frame %d has encoded %dx%d pixels for native %dx%d extents", prop.Name, info.Resource, frame.Width, frame.Height, int(info.Right-info.Left), int(info.Bottom-info.Top))
		}
		pixels, mask, err := decodeWorldActorFrame(frame)
		if err != nil {
			return IndexedFrame{}, nil, fmt.Errorf("decode flat prop %q frame %d pixels: %w", prop.Name, info.Resource, err)
		}
		originX, originY := int(info.OriginX), int(info.OriginY)
		bounds := image.Rect(prop.Anchor.X-originX, prop.Anchor.Y-originY, prop.Anchor.X-originX+width, prop.Anchor.Y-originY+height)
		visible := bounds.Intersect(composite.Bounds())
		if visible.Empty() {
			continue
		}
		clippedPixels, clippedMask := make([]byte, visible.Dx()*visible.Dy()), make([]bool, visible.Dx()*visible.Dy())
		for y := 0; y < visible.Dy(); y++ {
			for x := 0; x < visible.Dx(); x++ {
				source := (visible.Min.Y-bounds.Min.Y+y)*width + visible.Min.X - bounds.Min.X + x
				destination := y*visible.Dx() + x
				clippedPixels[destination], clippedMask[destination] = pixels[source], mask[source]
				if clippedMask[destination] {
					value := color.RGBAModel.Convert(background.Palette[clippedPixels[destination]]).(color.RGBA)
					composite.SetRGBA(visible.Min.X+x, visible.Min.Y+y, value)
				}
			}
		}
		projected = append(projected, ProjectedFlatProp{Name: prop.Name, Bounds: visible, pixels: clippedPixels, mask: clippedMask})
	}
	frame := background
	frame.rgba = composite
	return frame, projected, nil
}

func HitTestFlatProps(props []ProjectedFlatProp, point image.Point) (string, bool) {
	for index := len(props) - 1; index >= 0; index-- {
		prop := props[index]
		if !point.In(prop.Bounds) {
			continue
		}
		offset := (point.Y-prop.Bounds.Min.Y)*prop.Bounds.Dx() + point.X - prop.Bounds.Min.X
		if offset >= 0 && offset < len(prop.mask) && prop.mask[offset] {
			return prop.Name, true
		}
	}
	return "", false
}

// WorldPropReachesFrame reports whether a set prop reaches the frame selection
// that steps its sequence cursor. FUN_00421820's path B runs its depth test before
// FUN_00421B70 selects the frame: the depth must be at least 0x20 and the far clip
// at most 0x600, and a prop that fails either is not stepped. The depth here is the
// same model projectWorldProp uses, which is not yet verified against FUN_004064B0.
func WorldPropReachesFrame(point [3]int16, prop WorldPropSprite) bool {
	depth, farClip := worldPropDepth(point, prop)
	return depth >= 0x20 && farClip <= 0x600
}

// worldPropDepth is the depth ahead of the camera and the far clip of a set prop,
// computed as projectWorldProp computes them.
func worldPropDepth(point [3]int16, prop WorldPropSprite) (depth, farClip int) {
	forwardX, forwardY := 0, -1
	switch point[2] {
	case assets.SetDirectionSouth:
		forwardY = 1
	case assets.SetDirectionEast:
		forwardX, forwardY = 1, 0
	case assets.SetDirectionWest:
		forwardX, forwardY = -1, 0
	}
	camera := NativeActorCameraPosition(point)
	deltaX, deltaY := int(prop.Position[0])-camera[0], int(prop.Position[1])-camera[1]
	depth = deltaX*forwardX + deltaY*forwardY
	farClip = depth - int(prop.ZClip) + 0x80
	if farClip < 0 {
		farClip = 0
	}
	return depth, farClip
}

func projectWorldProp(background IndexedFrame, point [3]int16, activeSet string, prop WorldPropSprite) (ProjectedWorldActor, bool, error) {
	if !prop.Visible || !strings.EqualFold(prop.Set, activeSet) {
		return ProjectedWorldActor{}, false, nil
	}
	if prop.Archive == nil {
		return ProjectedWorldActor{}, false, fmt.Errorf("world prop %q has no archive", prop.Name)
	}
	angle := NativeActorViewAngle(prop.Position, point, prop.Heading)
	info, err := prop.View.FrameInfo(prop.Frame, angle)
	if err != nil {
		if errors.Is(err, assets.ErrPropFrameNoVariant) {
			// FUN_00421B70 found no variant for this frame, so the projection draws nothing.
			return ProjectedWorldActor{}, false, nil
		}
		return ProjectedWorldActor{}, false, fmt.Errorf("select world prop %q frame: %w", prop.Name, err)
	}
	data, err := prop.Archive.Resource(info.Resource)
	if err != nil {
		return ProjectedWorldActor{}, false, fmt.Errorf("read world prop %q frame %d: %w", prop.Name, info.Resource, err)
	}
	frame, err := DecodePuppetFrame(data)
	if err != nil {
		return ProjectedWorldActor{}, false, fmt.Errorf("decode world prop %q frame %d: %w", prop.Name, info.Resource, err)
	}
	displayWidth, displayHeight := frame.Width, frame.Height
	if int(info.Right-info.Left) != displayWidth || int(info.Bottom-info.Top) != displayHeight {
		return ProjectedWorldActor{}, false, fmt.Errorf("world prop %q frame %d has encoded %dx%d pixels for native %dx%d extents", prop.Name, info.Resource, frame.Width, frame.Height, int(info.Right-info.Left), int(info.Bottom-info.Top))
	}
	pixels, mask, err := decodeWorldActorFrame(frame)
	if err != nil {
		return ProjectedWorldActor{}, false, fmt.Errorf("decode world prop %q frame %d pixels: %w", prop.Name, info.Resource, err)
	}
	hitMask := mask
	forwardX, forwardY := 0, -1
	switch point[2] {
	case assets.SetDirectionSouth:
		forwardY = 1
	case assets.SetDirectionEast:
		forwardX, forwardY = 1, 0
	case assets.SetDirectionWest:
		forwardX, forwardY = -1, 0
	}
	camera := NativeActorCameraPosition(point)
	deltaX, deltaY := int(prop.Position[0])-camera[0], int(prop.Position[1])-camera[1]
	depth := deltaX*forwardX + deltaY*forwardY
	farClip := depth - int(prop.ZClip) + 0x80
	if farClip < 0 {
		farClip = 0
	}
	if depth < 0x20 || farClip > 0x600 {
		return ProjectedWorldActor{}, false, nil
	}
	lateral := deltaX*(-forwardY) + deltaY*forwardX
	metricScale := int(prop.Scale) * int(info.Metric) / 1000
	width, height := metricScale*displayWidth/depth, metricScale*displayHeight/depth
	if width < 1 || height < 1 {
		return ProjectedWorldActor{}, false, nil
	}
	anchorX, anchorY := background.Width/2+310*lateral/depth, background.Height/2-310*(int(prop.Position[2])-camera[2])/depth
	left := anchorX - int(info.OriginX)*width/displayWidth
	top := anchorY - int(info.OriginY)*height/displayHeight
	bounds := image.Rect(left, top, left+width, top+height)
	visible := bounds.Intersect(image.Rect(0, 0, background.Width, background.Height))
	if visible.Empty() {
		return ProjectedWorldActor{}, false, nil
	}
	scaledPixels, scaledMask := scaleWorldActorFrame(PuppetFrame{Width: displayWidth, Height: displayHeight}, pixels, mask, width, height)
	if visible != bounds {
		clippedPixels, clippedMask := make([]byte, visible.Dx()*visible.Dy()), make([]bool, visible.Dx()*visible.Dy())
		for y := range visible.Dy() {
			for x := range visible.Dx() {
				source := (visible.Min.Y-bounds.Min.Y+y)*bounds.Dx() + visible.Min.X - bounds.Min.X + x
				destination := y*visible.Dx() + x
				clippedPixels[destination], clippedMask[destination] = scaledPixels[source], scaledMask[source]
			}
		}
		scaledPixels, scaledMask = clippedPixels, clippedMask
	}
	return ProjectedWorldActor{Name: prop.Name, Depth: depth, Bounds: visible, pixels: scaledPixels, mask: scaledMask, propHitMask: hitMask, propHitSourceWidth: frame.Width, propHitSourceHeight: frame.Height, propHitStride: frame.Width, propBounds: bounds}, true, nil
}
