package scripts

func NativeAdvanceClockFields(clock, day, phase int) (int, int) {
	if phase != 0 {
		return clock, day
	}
	if clock < 3 {
		return clock + 1, day
	}
	return 1, day + 1
}
