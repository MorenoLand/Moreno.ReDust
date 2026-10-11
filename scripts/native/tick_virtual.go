package native

// The replay clock. Silent replays move game time by a fixed step per frame, so
// a replay's timing no longer depends on how busy the machine is. Normal play
// keeps the wall clock.

// virtualFrameMicros is one 60 Hz frame in microseconds.
const virtualFrameMicros = 16667

var virtualClock struct {
	on     bool
	micros uint64
}

// EnableVirtualTick switches NativeTickMilliseconds to the replay clock, which
// starts at zero and moves only when AdvanceVirtualTick is called.
func EnableVirtualTick() {
	virtualClock.on, virtualClock.micros = true, 0
}

// AdvanceVirtualTick moves the replay clock forward by one frame. It does
// nothing unless the replay clock is on.
func AdvanceVirtualTick() {
	if virtualClock.on {
		virtualClock.micros += virtualFrameMicros
	}
}

// virtualTickMilliseconds reports the replay clock and whether it is on.
func virtualTickMilliseconds() (uint32, bool) {
	if !virtualClock.on {
		return 0, false
	}
	return uint32(virtualClock.micros / 1000), true
}
