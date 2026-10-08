package scripts

import "redust/assets"

// NativePathWalk is the "walkonpath" job (FUN_00410290's path branch): the
// actor advances by its speed each pass along the stored polyline, taking the
// position FUN_00411620 interpolates and the heading of the segment it is on.
type NativePathWalk struct {
	Points   [][3]int16
	Segment  []int
	Total    int
	Progress int
	Speed    int16
}

// NativePathWalkState is the persisted form of a NativePathWalk.
type NativePathWalkState struct {
	Points   [][3]int16 `json:"points"`
	Segment  []int      `json:"segment"`
	Total    int        `json:"total"`
	Progress int        `json:"progress"`
	Speed    int16      `json:"speed"`
}

func NewNativePathWalk(path *assets.Path, speed int16) *NativePathWalk {
	return &NativePathWalk{Points: path.Points, Segment: path.Segment, Total: path.Total, Speed: speed}
}

func (w *NativePathWalk) Snapshot() NativePathWalkState {
	return NativePathWalkState{Points: w.Points, Segment: w.Segment, Total: w.Total, Progress: w.Progress, Speed: w.Speed}
}

func RestoreNativePathWalk(state NativePathWalkState) *NativePathWalk {
	return &NativePathWalk{Points: state.Points, Segment: state.Segment, Total: state.Total, Progress: state.Progress, Speed: state.Speed}
}

// Pass advances one pump. heading is the bearing function (FUN_004113F0).
// It returns the new position, the heading of the current segment and
// whether the walk continues.
func (w *NativePathWalk) Pass(heading func(from, to [3]int16) int16) ([3]int16, int16, bool) {
	last := len(w.Points) - 1
	w.Progress += int(w.Speed)
	if w.Progress >= w.Total || last < 1 {
		if last < 1 {
			return w.Points[0], 0, false
		}
		return w.Points[last], heading(w.Points[last-1], w.Points[last]), false
	}
	remaining := w.Progress
	for index := 1; index <= last; index++ {
		length := w.Segment[index]
		if remaining <= length {
			from, to := w.Points[index-1], w.Points[index]
			if length == 0 {
				return to, heading(from, to), true
			}
			position := [3]int16{
				int16((int(to[0])-int(from[0]))*remaining/length + int(from[0])),
				int16((int(to[1])-int(from[1]))*remaining/length + int(from[1])),
				int16((int(to[2])-int(from[2]))*remaining/length + int(from[2])),
			}
			return position, heading(from, to), true
		}
		remaining -= length
	}
	return w.Points[last], heading(w.Points[last-1], w.Points[last]), false
}
