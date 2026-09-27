package render

import (
	"fmt"
	"image"
	"strings"

	"redust/assets"
)

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
	sourceWidth, sourceHeight := int(info.Right-info.Left), int(info.Bottom-info.Top)
	if sourceWidth < 1 || sourceHeight < 1 || frame.Width != sourceHeight || frame.Height != sourceWidth {
		return ProjectedWorldActor{}, false, fmt.Errorf("world prop %q frame %d has encoded %dx%d pixels for native %dx%d extents", prop.Name, info.Resource, frame.Width, frame.Height, sourceWidth, sourceHeight)
	}
	pixels, mask, err := decodeWorldActorFrame(frame)
	if err != nil {
		return ProjectedWorldActor{}, false, fmt.Errorf("decode world prop %q frame %d pixels: %w", prop.Name, info.Resource, err)
	}
	hitMask := mask
	transposedPixels, transposedMask := make([]byte, len(pixels)), make([]bool, len(mask))
	for sourceY := range frame.Height {
		for sourceX := range frame.Width {
			source, destination := sourceY*frame.Width+sourceX, sourceX*sourceWidth+sourceY
			transposedPixels[destination], transposedMask[destination] = pixels[source], mask[source]
		}
	}
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
	width, height := metricScale*sourceWidth/depth, metricScale*sourceHeight/depth
	if width < 1 || height < 1 {
		return ProjectedWorldActor{}, false, nil
	}
	anchorX, anchorY := background.Width/2+310*lateral/depth, background.Height/2-310*(int(prop.Position[2])-camera[2])/depth
	left := anchorX - int(info.OriginX)*width/sourceWidth
	top := anchorY - int(info.OriginY)*height/sourceHeight
	bounds := image.Rect(left, top, left+width, top+height)
	visible := bounds.Intersect(image.Rect(0, 0, background.Width, background.Height))
	if visible.Empty() {
		return ProjectedWorldActor{}, false, nil
	}
	scaledPixels, scaledMask := scaleWorldActorFrame(PuppetFrame{Width: sourceWidth, Height: sourceHeight}, transposedPixels, transposedMask, width, height)
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
	return ProjectedWorldActor{Name: prop.Name, Depth: depth, Bounds: visible, pixels: scaledPixels, mask: scaledMask, propHitMask: hitMask, propHitSourceWidth: frame.Height, propHitSourceHeight: frame.Width, propHitStride: frame.Width}, true, nil
}
