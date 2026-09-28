package scripts

func NativeAdvanceClockFields(clock, day, phase int) (int, int, int) {
	phase = 0
	if clock < 3 {
		return clock + 1, day, phase
	}
	return 1, day + 1, phase
}
