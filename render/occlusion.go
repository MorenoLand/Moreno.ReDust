package render

import "image"

type WorldOccluder struct {
	Polygon []image.Point
	Depth   int
}

func WorldOccludersForView(set string, point [3]int16) []WorldOccluder {
	if set != "town" {
		return nil
	}
	switch point {
	case [3]int16{6, 12, 2}:
		return []WorldOccluder{
			{Polygon: []image.Point{{151, 45}, {158, 45}, {159, 165}, {162, 178}, {157, 182}, {150, 181}, {151, 162}}, Depth: 392},
			{Polygon: []image.Point{{347, 45}, {353, 44}, {353, 164}, {356, 179}, {354, 183}, {348, 181}, {346, 178}}, Depth: 392},
			{Polygon: []image.Point{{154, 46}, {349, 47}, {349, 55}, {154, 54}}, Depth: 392},
			{Polygon: []image.Point{{207, 61}, {294, 66}, {294, 84}, {205, 79}}, Depth: 392},
		}
	case [3]int16{9, 9, 4}:
		return []WorldOccluder{{Polygon: []image.Point{{58, 102}, {453, 102}, {456, 226}, {58, 230}}}}
	case [3]int16{10, 10, 4}:
		return []WorldOccluder{
			{Polygon: []image.Point{{301, 88}, {417, 85}, {442, 116}, {442, 178}, {329, 177}, {301, 154}}},
			{Polygon: []image.Point{{321, 120}, {441, 116}, {441, 179}, {329, 177}, {321, 165}}},
		}
	}
	return nil
}

func worldOccluderDepth(occluder WorldOccluder, x, horizon int) int {
	if occluder.Depth != 0 {
		return occluder.Depth
	}
	px, ground := float64(x)+0.5, float64(horizon)
	for i, j := 0, len(occluder.Polygon)-1; i < len(occluder.Polygon); j, i = i, i+1 {
		a, b := occluder.Polygon[i], occluder.Polygon[j]
		if (float64(a.X) > px) != (float64(b.X) > px) {
			y := float64(b.Y-a.Y)*(px-float64(a.X))/float64(b.X-a.X) + float64(a.Y)
			if y > ground {
				ground = y
			}
		}
	}
	if ground <= float64(horizon) {
		return 0
	}
	return int(float64(NativeActorFocalLength*62) / (ground - float64(horizon)))
}

func worldOccluderContains(polygon []image.Point, point image.Point) bool {
	inside := false
	px, py := float64(point.X)+0.5, float64(point.Y)+0.5
	for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
		a, b := polygon[i], polygon[j]
		if (float64(a.Y) > py) != (float64(b.Y) > py) && px < float64(b.X-a.X)*(py-float64(a.Y))/float64(b.Y-a.Y)+float64(a.X) {
			inside = !inside
		}
	}
	return inside
}

func applyWorldOcclusion(actor *ProjectedWorldActor, occluders []WorldOccluder, horizon int) bool {
	visible := false
	for y := 0; y < actor.Bounds.Dy(); y++ {
		for x := 0; x < actor.Bounds.Dx(); x++ {
			index := y*actor.Bounds.Dx() + x
			if !actor.mask[index] {
				continue
			}
			point := actor.Bounds.Min.Add(image.Pt(x, y))
			for _, occluder := range occluders {
				if !worldOccluderContains(occluder.Polygon, point) {
					continue
				}
				depth := worldOccluderDepth(occluder, point.X, horizon)
				if depth > 0 && actor.Depth > depth {
					actor.mask[index] = false
					break
				}
			}
			visible = visible || actor.mask[index]
		}
	}
	return visible
}
