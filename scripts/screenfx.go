package scripts

// Screen effect schedules for visualeffect (command 12009). FUN_00425D30
// validates the effect keyword and the duration, then FUN_00407160 runs the
// strip handler for the keyword. Each handler blits one strip, waits, and
// repeats; the schedule below is the number of waits and the ticks per wait.
//
// The keyword band is 24001..24014 (0x5DC1..0x5DCE). The handler table in
// FUN_00407160 is, in order: barndoorclose, barndooropen, irisclose,
// irisopen, scrolldown, scrollup, scrollright, scrolleft, venetian,
// wipedown, wipeup, wiperight, wipeleft, plain.

// ScreenEffectSchedule is the native wait schedule of one effect. Steps is
// the number of FUN_0042B730 waits. Each wait is TicksPerStep ticks at the
// 60 Hz update rate, since FUN_0042B730(n) waits n*50/3 ms.
type ScreenEffectSchedule struct {
	Steps        int
	TicksPerStep int
}

// screenEffectStageBottom is the bottom coordinate of DAT_00459700, the stage
// view rectangle that FUN_00425D30 passes to the effect. FUN_0041C6E0 is its
// only direct writer and stores 0x180 (384). The venetian wait divides by it.
const screenEffectStageBottom = 0x180

// screenEffectKeywords lists the 24001..24014 keywords by opcode, in the order
// of the FUN_00407160 switch.
var screenEffectKeywords = map[uint16]string{
	24001: "barndoorclose",
	24002: "barndooropen",
	24003: "irisclose",
	24004: "irisopen",
	24005: "scrolldown",
	24006: "scrollup",
	24007: "scrollright",
	24008: "scrolleft",
	24009: "venetian",
	24010: "wipedown",
	24011: "wipeup",
	24012: "wiperight",
	24013: "wipeleft",
	24014: "plain",
}

// ScreenEffectKeyword reports whether effect is one of the 24001..24014
// keywords. FUN_00425D30 answers 0x0E for any other operand.
func ScreenEffectKeyword(effect uint16) bool {
	_, ok := screenEffectKeywords[effect]
	return ok
}

// ScreenEffectName returns the keyword's name, or "" when effect is not one
// of the 24001..24014 keywords.
func ScreenEffectName(effect uint16) string { return screenEffectKeywords[effect] }

// ClampVisualEffectDuration applies FUN_00425D30's clamp of the duration to
// 1..1000 before the handler runs.
func ClampVisualEffectDuration(duration int) int {
	return min(max(duration, 1), 1000)
}

// VisualEffectSchedule returns the verified wait schedule for the effects
// whose strip geometry the port implements: the wipes, the scrolls, venetian,
// irisopen and irisclose. ok is false for plain and the barndoors. Their
// timing is not part of this table, so the caller keeps its existing wait.
func VisualEffectSchedule(effect uint16, duration int) (ScreenEffectSchedule, bool) {
	duration = ClampVisualEffectDuration(duration)
	steps, divisor := 0, 0
	switch effect {
	case 24003: // irisclose, FUN_00407460: duration/6, six steps.
		steps, divisor = 6, 6
	case 24004: // irisopen, FUN_004075A0: duration>>4, sixteen steps.
		steps, divisor = 16, 16
	case 24005, 24006, 24007, 24008: // scrolldown, scrollup, scrollright, scrolleft: duration/15, fifteen strips.
		steps, divisor = 15, 15
	case 24009: // venetian, FUN_00407990: duration / bottom, (height>>3)+1 steps.
		steps, divisor = screenEffectStageBottom>>3+1, screenEffectStageBottom
	case 24010, 24011, 24012, 24013: // wipedown, wipeup, wiperight, wipeleft: duration/15, fifteen strips.
		steps, divisor = 15, 15
	default:
		return ScreenEffectSchedule{}, false
	}
	return ScreenEffectSchedule{Steps: steps, TicksPerStep: max(duration/divisor, 1)}, true
}
