package render

// sightlineStep is the spacing, in world units, of the samples taken along the
// line from the camera to an actor: a quarter of a cell.
const sightlineStep = 64

// SightlineBlocked reports whether a world actor at target is hidden from the
// camera at point, because the straight line from the camera to the actor
// crosses a cell the player cannot stand on: a building or other
// backdrop-only cell. walkable takes cell coordinates, which are the world
// position divided by 256 (DirectionID is x, SceneID is y).
//
// The native draw path culls actors by distance alone and never tests walls,
// so ReDust applies this rule in the town to keep NPCs from showing through
// buildings. The camera's own cell and the actor's cell are not tested, so an
// NPC standing in front of a building stays visible.
func SightlineBlocked(walkable func(x, y int) bool, point [3]int16, target [3]int16) bool {
	camera := NativeActorCameraPosition(point)
	fromX, fromY := camera[0], camera[1]
	toX, toY := int(target[0]), int(target[1])
	dx, dy := toX-fromX, toY-fromY
	steps := max(absInt(dx), absInt(dy)) / sightlineStep
	if steps < 2 {
		return false
	}
	fromCellX, fromCellY := fromX>>8, fromY>>8
	toCellX, toCellY := toX>>8, toY>>8
	for step := 1; step < steps; step++ {
		x := fromX + dx*step/steps
		y := fromY + dy*step/steps
		cellX, cellY := x>>8, y>>8
		if (cellX == fromCellX && cellY == fromCellY) || (cellX == toCellX && cellY == toCellY) {
			continue
		}
		if !walkable(cellX, cellY) {
			return true
		}
	}
	return false
}
