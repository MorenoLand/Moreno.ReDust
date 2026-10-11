package render

// NativeCameraHeading is the heading FUN_00406570 gives the camera for a direction: 1 north
// (0xC0), 2 south (0x40), 3 east (0) and 4 west (0x80), from its translation switch on
// DAT_00459A62. Any other direction has no heading in the reference, which keeps the last one.
func NativeCameraHeading(direction int16) (int16, bool) {
	switch direction {
	case 1:
		return 0xc0, true
	case 2:
		return 0x40, true
	case 3:
		return 0, true
	case 4:
		return 0x80, true
	}
	return 0, false
}

// NativeCamera is the reference's camera state, the globals that FUN_00405EA0, FUN_00405DF0 and
// FUN_00406570 keep:
//
//	Cell, Dir        DAT_00459A5E (cell x), DAT_00459A60 (cell y) and DAT_00459A62 (direction)
//	Dest, DestDir    DAT_00459A64, A66 and A68: the cell and direction the camera moves to
//	Progress         DAT_00442254: -1 at rest, 0 to 4 while a move runs
//	Pullback, Height DAT_00459A4C and DAT_00459A4E, from the open set's metadata
//	Heading          DAT_00459A6A, masked to a byte
//	Position         A78, A7A and A7C, the camera's world x, y and z
//
// FUN_00405EA0 takes its cell as (y, x, direction) in that order. The port keeps x first, as
// NativeActorCameraPosition does, because the reference's cell x is the low word of the same
// dword as cell y.
type NativeCamera struct {
	Cell     [2]int16
	Dir      int16
	Dest     [2]int16
	DestDir  int16
	Progress int16
	Pullback int16
	Height   int16
	Heading  int16
	Position [3]int16
}

// Place is FUN_00405EA0: the camera takes the cell and direction at once. A negative value
// keeps the field, as the reference's `-1 < param` tests do, and the move ends at rest. The
// reference runs one FUN_00406570 step at progress 0 before it sets rest, so the position is
// the cell's centre less the pullback along the heading.
func (c *NativeCamera) Place(cellX, cellY, dir int16) {
	if cellX >= 0 {
		c.Cell[0] = cellX
	}
	if cellY >= 0 {
		c.Cell[1] = cellY
	}
	if dir >= 0 {
		c.Dir = dir
	}
	c.Dest, c.DestDir = c.Cell, c.Dir
	c.Progress = 0
	c.Step()
	c.Progress = -1
}

// Pump is FUN_004059D0, called once per native pump. While a set is open and a move is in
// progress (progress 0 or more), it steps the camera once, and the step advances the progress.
// At rest it does nothing; the reference then only redraws.
func (c *NativeCamera) Pump(open bool) {
	if !open || c.Progress < 0 {
		return
	}
	c.Step()
}

// Step is FUN_00406570 without its draw and resource side effects. It places the camera for
// the current progress and then advances the progress. A straight move (same direction) moves
// the camera a quarter cell per progress step, 64 units, towards the destination cell; a turn
// (different direction) swings the heading 16 units per step. After progress 4 the cell and
// direction become the destination and the progress returns to rest (-1).
//
// The pullback is subtracted along the heading with the reference's Q14 products
// (FUN_00406B10 for x with the cosine table, FUN_00406B40 for y with the sine table).
func (c *NativeCamera) Step() {
	progress := int32(c.Progress)
	x := c.Cell[0]*256 + 128
	y := c.Cell[1]*256 + 128
	heading := c.Heading
	if c.Dir == c.DestDir {
		step := int16(progress * 64)
		switch c.Dir {
		case 1:
			heading = 0xc0
			if c.Dest[1] < c.Cell[1] {
				y -= step
			} else {
				y += step
			}
		case 2:
			heading = 0x40
			if c.Dest[1] < c.Cell[1] {
				y -= step
			} else {
				y += step
			}
		case 3:
			heading = 0
			if c.Cell[0] < c.Dest[0] {
				x += step
			} else {
				x -= step
			}
		case 4:
			heading = 0x80
			if c.Cell[0] < c.Dest[0] {
				x += step
			} else {
				x -= step
			}
		}
	} else {
		turn := int16(progress * 16)
		switch c.Dir {
		case 1:
			if c.DestDir == 3 {
				heading = turn + 0xc0
			} else {
				heading = 0xc0 - turn
			}
		case 2:
			if c.DestDir != 3 {
				heading = turn + 0x40
			} else {
				heading = 0x40 - turn
			}
		case 3:
			heading = turn
			if c.DestDir == 1 {
				heading = 0 - turn
			}
		case 4:
			if c.DestDir == 1 {
				heading = turn + 0x80
			} else {
				heading = 0x80 - turn
			}
		}
	}
	heading &= 0xff
	c.Heading = heading
	sin, cos := NativeQ14Trig(heading)
	pullbackX := int16(q14Shift(cos * int32(c.Pullback)))
	pullbackY := int16(q14Shift(sin * int32(c.Pullback)))
	c.Position = [3]int16{x - pullbackX, y - pullbackY, c.Height}
	c.Progress++
	if c.Progress > 4 {
		c.Cell, c.Dir = c.Dest, c.DestDir
		c.Progress = -1
	}
}

// NativeAngle is FUN_00411420: the bearing of the vector (x, y) on the 256-unit circle. Its
// first parameter is the y component and its second the x, so NativeAngle(y, x) is atan2 with
// bearing 0 along +x and 64 along +y. The reference returns it as a signed byte; the caller
// keeps the low eight bits, so the result here is the same number, sign-extended. The
// quadrant branches and the 512-entry table lookup (DAT_0045D160, nativeActorAtan) are
// transcribed from the decompile.
func NativeAngle(y, x int16) int16 {
	table := func(num, den int16) int16 { return int16(nativeActorAtan[(int32(num)<<9)/int32(den)]) }
	switch {
	case y == 0:
		if x >= 0 {
			return 0
		}
		return -128
	case x == 0:
		if y > 0 {
			return 64
		}
		return -64
	case y < 0:
		ay := -y
		if x >= 0 {
			if x == ay {
				return -32
			}
			if x < ay {
				return table(x, ay) - 64
			}
			return -table(ay, x)
		}
		ax := -x
		if ax == ay {
			return -96
		}
		if ax < ay {
			return -64 - table(ax, ay)
		}
		return table(ay, ax) - 128
	default:
		if x < 0 {
			ax := -x
			if ax == y {
				return 96
			}
			if ax < y {
				return table(ax, y) + 64
			}
			return -128 - table(y, ax)
		}
		if x == y {
			return 32
		}
		if x < y {
			return 64 - table(x, y)
		}
		return table(y, x)
	}
}
