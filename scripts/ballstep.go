package scripts

import (
	"fmt"
	"strings"
)

// makeBall is makeball(name, speedX, speedY, speedZ, gravity, bounce, lifetime):
// FUN_00410760. Any ball already on the prop is cancelled first (FUN_0040F7D0),
// and a full table is status 8 (FUN_0040F660).
func (h *GameHost) makeBall(call *ScriptCall) (int, uint16, error) {
	args, consumed, status, err := call.Args()
	if err != nil || status != 0 {
		return 0, status, err
	}
	if len(args) != 7 || args[0].Kind != 3 {
		return 0, ScriptStatusMalformed, nil
	}
	for _, arg := range args[1:] {
		if arg.Kind != 4 {
			return 0, ScriptStatusWrongType, nil
		}
	}
	if h.Props == nil {
		return 0, 0, fmt.Errorf("props are unavailable")
	}
	name := args[0].Text
	if !h.Props.Known(name) {
		return 0, 0x0a, nil
	}
	h.Props.StopBall(name)
	slot := h.Props.freeBall()
	if slot == nil {
		return 0, 8, nil
	}
	prop := h.Props.Get(name)
	x, y, z := int16(prop.X), int16(prop.Y), int16(prop.Z)
	*slot = PropBallJob{
		Name:     name,
		Active:   true,
		VX:       int16(args[1].Int),
		VY:       int16(args[2].Int),
		VZ:       int16(args[3].Int),
		Gravity:  args[4].Int != 0,
		Bounce:   args[5].Int != 0,
		Lifetime: int16(args[6].Int),
		X:        x,
		Y:        y,
		Z:        z,
		CellX:    x >> 8,
		CellY:    y >> 8,
	}
	return consumed, 0, nil
}

// Known reports whether a prop is in the game's prop table.
func (t *ScriptProps) Known(name string) bool {
	for _, known := range t.table {
		if strings.EqualFold(known, name) {
			return true
		}
	}
	return false
}

// freeBall is the first slot FUN_0040F660 would fill, nil when all sixteen are used.
func (t *ScriptProps) freeBall() *PropBallJob {
	for index := range t.Balls {
		if !t.Balls[index].Active {
			return &t.Balls[index]
		}
	}
	return nil
}

// StepBalls runs one pass of every active ball, in slot order: FUN_00410980 is the
// second loop table FUN_0040F4E0 runs at the pump rate.
func (h *GameHost) StepBalls() error {
	if h.Props == nil {
		return nil
	}
	for index := range h.Props.Balls {
		ball := &h.Props.Balls[index]
		if !ball.Active {
			continue
		}
		if err := h.stepBall(ball); err != nil {
			return err
		}
	}
	return nil
}

// stepBall is one pass of FUN_00410980 for a ball. The actor boxes are not tested:
// the recovered code tests an actor's second half-size, which no decoded write sets,
// so no actor stops a ball here.
func (h *GameHost) stepBall(ball *PropBallJob) error {
	ball.Lifetime--
	if ball.Lifetime < 0 {
		ball.Active = false
		return h.endBall(ball.Name, "frames")
	}
	oldX, oldY, oldZ := ball.X, ball.Y, ball.Z
	oldCellX, oldCellY := ball.CellX, ball.CellY
	ball.X += ball.VX
	ball.CellX = ball.X >> 8
	ball.Y += ball.VY
	ball.CellY = ball.Y >> 8
	ball.Z += ball.VZ
	if ball.Gravity {
		ball.VZ -= 2
	}
	if ball.Bounce {
		if ball.Z <= 0 {
			ball.Z = 0
			ball.VZ = -(ball.VZ / 2)
			if absInt16(ball.VZ) < 3 {
				ball.VZ = 0
			}
		}
		if h.ballBlocked(ball.CellX, ball.CellY) {
			if oldCellX != ball.CellX {
				ball.X = int16((int(oldCellX) + int(ball.CellX) + 1) * 128)
				ball.CellX = ball.X >> 8
				ball.VX = -(ball.VX / 2)
			}
			if oldCellY != ball.CellY {
				ball.Y = int16((int(oldCellY) + int(ball.CellY) + 1) * 128)
				ball.CellY = ball.Y >> 8
				ball.VY = -(ball.VY / 2)
			}
		}
	} else if ball.Z <= 0 || h.ballBlocked(ball.CellX, ball.CellY) {
		h.placeBall(ball)
		ball.Active = false
		return h.endBall(ball.Name, "building")
	}
	if h.ballMeetsPlayer([3]int32{int32(oldX), int32(oldY), int32(oldZ)}, [3]int32{int32(ball.X), int32(ball.Y), int32(ball.Z)}) {
		h.placeBall(ball)
		ball.Active = false
		return h.endBall(ball.Name, "player")
	}
	h.placeBall(ball)
	return nil
}

// placeBall stores the ball's position in its prop, as FUN_004214A0 stores the
// prop record the step copies.
func (h *GameHost) placeBall(ball *PropBallJob) {
	prop := h.Props.Get(ball.Name)
	prop.X, prop.Y, prop.Z = int32(ball.X), int32(ball.Y), int32(ball.Z)
}

func (h *GameHost) ballBlocked(x, y int16) bool {
	return h.Env.BallBlocked != nil && h.Env.BallBlocked(int(x), int(y))
}

// ballMeetsPlayer is FUN_00410FB0: the segment from the ball's old to new position
// passes the player's box. No box, no hit (the test needs both half-sizes at least 1).
func (h *GameHost) ballMeetsPlayer(old, new [3]int32) bool {
	half := h.Props.PlayerBox
	if half[0] < 1 || half[1] < 1 || h.Env.PlayerCenter == nil {
		return false
	}
	cx, cy, cz := h.Env.PlayerCenter()
	center := [3]int32{cx, cy, cz}
	size := [3]int32{int32(half[1]), int32(half[1]), int32(half[0])}
	for axis := range center {
		low, high := center[axis]-size[axis], center[axis]+size[axis]
		if old[axis] < low && new[axis] < low || high < old[axis] && high < new[axis] {
			return false
		}
	}
	return true
}

func (h *GameHost) endBall(name, message string) error {
	if h.Env.EndBall == nil {
		return fmt.Errorf("balls have no prop message sender")
	}
	return h.Env.EndBall(name, message)
}

func absInt16(value int16) int16 {
	if value < 0 {
		return -value
	}
	return value
}
