package render

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sort"

	"redust/assets"
)

type WorldActorSprite struct {
	Name     string
	Position [3]int16
	Scale    int16
	Metric   uint16
	Frame    PuppetFrame
	Visible  bool
}

type ProjectedWorldActor struct {
	Name   string
	Depth  int
	Bounds image.Rectangle
	frame  PuppetFrame
	pixels []byte
	mask   []bool
}

var nativeActorAtan = func() [512]uint8 {
	var table [512]uint8
	for index := range table {
		table[index] = uint8(math.Atan(float64(index)/512) * 256 / (2 * math.Pi))
	}
	return table
}()

func NativeActorViewAngle(position [3]int16, point [3]int16, heading int16) int16 {
	forwardX, forwardY := 0, -1
	switch point[2] {
	case assets.SetDirectionSouth:
		forwardY = 1
	case assets.SetDirectionEast:
		forwardX, forwardY = 1, 0
	case assets.SetDirectionWest:
		forwardX, forwardY = -1, 0
	}
	cameraX, cameraY := int(point[0])*256+128-64*forwardX, int(point[1])*256+128-64*forwardY
	bearing := nativeActorBearing(cameraX-int(position[0]), cameraY-int(position[1]))
	return int16((int(heading) - int(bearing) + 256) % 256)
}

func nativeActorBearing(dx, dy int) int16 {
	absX, absY := absInt(dx), absInt(dy)
	if dx == 0 {
		if dy < 0 {
			return 192
		}
		return 64
	}
	if dy == 0 {
		if dx < 0 {
			return 128
		}
		return 0
	}
	switch {
	case dx > 0 && dy > 0:
		if absX >= absY {
			return nativeActorAtanAngle(absY, absX)
		}
		return 64 - nativeActorAtanAngle(absX, absY)
	case dx < 0 && dy > 0:
		if absY >= absX {
			return 64 + nativeActorAtanAngle(absX, absY)
		}
		return 128 - nativeActorAtanAngle(absY, absX)
	case dx < 0 && dy < 0:
		if absX >= absY {
			return 128 + nativeActorAtanAngle(absY, absX)
		}
		return 192 - nativeActorAtanAngle(absX, absY)
	default:
		if absY >= absX {
			return 192 + nativeActorAtanAngle(absX, absY)
		}
		return 256 - nativeActorAtanAngle(absY, absX)
	}
}

func nativeActorAtanAngle(numerator, denominator int) int16 {
	if numerator <= 0 || denominator <= 0 {
		return 0
	}
	if numerator >= denominator {
		return 32
	}
	index := numerator * 512 / denominator
	if index > 511 {
		index = 511
	}
	return int16(nativeActorAtan[index])
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func LoadCastActorFrame(workspace assets.Workspace, cast assets.Cast, actor assets.CastActor, poseName string, frameIndex int, scale int16, angle int16) (WorldActorSprite, error) {
	variant, found, err := workspace.CastPoseFrame(cast, actor, poseName, frameIndex, angle)
	if err != nil {
		return WorldActorSprite{}, err
	}
	if !found {
		return WorldActorSprite{}, fmt.Errorf("cast actor %q pose %q has no frame at angle %d", actor.Name, poseName, angle)
	}
	cache, err := workspace.OpenResourceCache(cast.Name)
	if err != nil {
		return WorldActorSprite{}, err
	}
	defer cache.Close()
	lease, err := cache.Acquire(variant.Resource)
	if err != nil {
		return WorldActorSprite{}, err
	}
	data, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return WorldActorSprite{}, err
	}
	frame, err := DecodePuppetFrame(data)
	if err != nil {
		return WorldActorSprite{}, fmt.Errorf("decode cast actor %q frame %d: %w", actor.Name, variant.Resource, err)
	}
	frame.Origin.X, frame.Origin.Y = frame.Origin.Y, frame.Origin.X
	return WorldActorSprite{Name: actor.Name, Position: actor.Position, Scale: scale, Metric: variant.Metric, Frame: frame, Visible: true}, nil
}

func CompositeWorldActors(background IndexedFrame, point [3]int16, actors []WorldActorSprite) (IndexedFrame, []ProjectedWorldActor, error) {
	projected := make([]ProjectedWorldActor, 0, len(actors))
	for _, actor := range actors {
		if !actor.Visible {
			continue
		}
		projectedActor, found, err := projectWorldActor(background, point, actor)
		if err != nil {
			return IndexedFrame{}, nil, err
		}
		if found {
			projected = append(projected, projectedActor)
		}
	}
	if len(projected) == 0 {
		return background, nil, nil
	}
	if len(background.Palette) != 256 {
		return IndexedFrame{}, nil, fmt.Errorf("SET actor palette has %d entries, want 256", len(background.Palette))
	}
	sort.SliceStable(projected, func(i, j int) bool { return projected[i].Depth > projected[j].Depth })
	base, err := background.rgbaImage()
	if err != nil {
		return IndexedFrame{}, nil, err
	}
	composite := image.NewRGBA(base.Bounds())
	draw.Draw(composite, composite.Bounds(), base, image.Point{}, draw.Src)
	for _, actor := range projected {
		for y := 0; y < actor.Bounds.Dy(); y++ {
			sourceY := y * actor.frame.Height / actor.Bounds.Dy()
			for x := 0; x < actor.Bounds.Dx(); x++ {
				sourceX := x * actor.frame.Width / actor.Bounds.Dx()
				index := sourceY*actor.frame.Width + sourceX
				if !actor.mask[index] {
					continue
				}
				destination := image.Pt(actor.Bounds.Min.X+x, actor.Bounds.Min.Y+y)
				if destination.In(composite.Bounds()) {
					composite.SetRGBA(destination.X, destination.Y, color.RGBAModel.Convert(background.Palette[actor.pixels[index]]).(color.RGBA))
				}
			}
		}
	}
	background.rgba = composite
	return background, projected, nil
}

func HitTestWorldActors(actors []ProjectedWorldActor, point image.Point) (string, bool) {
	for index := len(actors) - 1; index >= 0; index-- {
		actor := actors[index]
		if !point.In(actor.Bounds) {
			continue
		}
		x := (point.X - actor.Bounds.Min.X) * actor.frame.Width / actor.Bounds.Dx()
		y := (point.Y - actor.Bounds.Min.Y) * actor.frame.Height / actor.Bounds.Dy()
		if actor.mask[y*actor.frame.Width+x] {
			return actor.Name, true
		}
	}
	return "", false
}

func projectWorldActor(background IndexedFrame, point [3]int16, actor WorldActorSprite) (ProjectedWorldActor, bool, error) {
	forwardX, forwardY := 0, -1
	switch point[2] {
	case assets.SetDirectionSouth:
		forwardY = 1
	case assets.SetDirectionEast:
		forwardX, forwardY = 1, 0
	case assets.SetDirectionWest:
		forwardX, forwardY = -1, 0
	}
	centerX, centerY := int(point[0])*256+128, int(point[1])*256+128
	cameraX, cameraY, cameraZ := centerX-64*forwardX, centerY-64*forwardY, 62
	deltaX, deltaY := int(actor.Position[0])-cameraX, int(actor.Position[1])-cameraY
	depth := deltaX*forwardX + deltaY*forwardY
	lateral := deltaX*(-forwardY) + deltaY*forwardX
	if depth <= 0 {
		return ProjectedWorldActor{}, false, nil
	}
	pixels, mask, err := decodeWorldActorFrame(actor.Frame)
	if err != nil {
		return ProjectedWorldActor{}, false, fmt.Errorf("decode cast actor %q frame: %w", actor.Name, err)
	}
	denominator := depth * 1000
	scale := int(actor.Scale) * int(actor.Metric)
	width, height := actor.Frame.Width*scale/denominator, actor.Frame.Height*scale/denominator
	if width < 1 || height < 1 {
		return ProjectedWorldActor{}, false, nil
	}
	anchorX, anchorY := background.Width/2+310*lateral/depth, background.Height/2-310*(int(actor.Position[2])-cameraZ)/depth
	left := anchorX - actor.Frame.Origin.X*scale/denominator
	top := anchorY - actor.Frame.Origin.Y*scale/denominator
	return ProjectedWorldActor{Name: actor.Name, Depth: depth, Bounds: image.Rect(left, top, left+width, top+height), frame: actor.Frame, pixels: pixels, mask: mask}, true, nil
}

func decodeWorldActorFrame(frame PuppetFrame) ([]byte, []bool, error) {
	if frame.Width < 1 || frame.Height < 1 || len(frame.Rows) != frame.Height {
		return nil, nil, fmt.Errorf("invalid frame dimensions %dx%d", frame.Width, frame.Height)
	}
	pixels, mask := make([]byte, frame.Width*frame.Height), make([]bool, frame.Width*frame.Height)
	for y, row := range frame.Rows {
		x, position := 0, 0
		for x < frame.Width {
			if position >= len(row) {
				return nil, nil, fmt.Errorf("scanline %d ended at pixel %d of %d", y, x, frame.Width)
			}
			command := row[position]
			position++
			run, operation := int(command>>2), command&3
			if run < 1 || run > frame.Width-x {
				return nil, nil, fmt.Errorf("scanline %d run %d at pixel %d exceeds width %d", y, run, x, frame.Width)
			}
			var repeated byte
			if operation == 2 {
				if position >= len(row) {
					return nil, nil, fmt.Errorf("scanline %d repeat run has no color", y)
				}
				repeated = row[position]
				position++
			}
			if operation == 3 && run > len(row)-position {
				return nil, nil, fmt.Errorf("scanline %d literal run exceeds data", y)
			}
			for i := 0; i < run; i++ {
				index := y*frame.Width + x + i
				switch operation {
				case 2:
					pixels[index], mask[index] = repeated, true
				case 3:
					pixels[index], mask[index] = row[position+i], true
				}
			}
			if operation == 3 {
				position += run
			}
			x += run
		}
	}
	return pixels, mask, nil
}
