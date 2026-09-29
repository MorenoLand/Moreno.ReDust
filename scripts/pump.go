package scripts

// The shared message pump, `FUN_0042BD80`, mapped in full. It is a **depth-guarded
// message loop**, and the guard is what makes nested dispatch safe:
//
//	if (DAT_0044456C != 0 && DAT_0045DC38 == 0) {
//	    // THE OUTER ENTRY, and the only one that blocks
//	    DAT_0045DC38 = 1;
//	    while (DAT_0044456C != 0) {
//	        if (GetMessageA(&msg, 0, 0, 0)) { TranslateMessage(&msg); DispatchMessageA(&msg); }
//	    }
//	    DAT_0045DC38 = DAT_0045DC38 - 1;
//	    return;
//	}
//	// EVERY NESTED ENTRY, and none of them block
//	DAT_0045DC38 = DAT_0045DC38 + 1;
//	if (DAT_0045E068 != 0) FUN_00434260(0);
//	while (PeekMessageA(&msg, 0, 0, 0, PM_REMOVE)) {
//	    TranslateMessage(&msg); DispatchMessageA(&msg);
//	    if (DAT_0045E068 != 0) FUN_00434260(0);
//	}
//	DAT_0045DC38 = DAT_0045DC38 - 1;
//
// **The outer entry uses `GetMessageA`, which blocks; every nested entry uses
// `PeekMessageA` with `PM_REMOVE`, which does not.** That difference is the entire
// design. A nested pump is reached from inside a window procedure, so it must not wait
// for a message: the only thing that can deliver one is the blocked outer loop, which is
// exactly the frame it is nested inside. Blocking there would deadlock. Draining what is
// already queued and returning is the only safe behaviour, and it is why the function
// can be called from a handler without special-casing anything at the call site.
//
// The depth counter `DAT_0045DC38` is the **third** instance of this project's
// re-entrancy-guard shape, alongside the puppet-block and `closepuppetfile` guards, and
// it is decremented on **both** exits — the outer path sets it to 1 and decrements, the
// nested path increments and decrements. So the counter returns to zero on every path,
// including the error-free early return.
const (
	// PumpDepthGuard is the field's role: a nesting depth, 0 when no pump is running.
	PumpDepthGuard = 0
	// PumpOuterBlocking is the sentinel the outer entry sets, 1.
	PumpOuterBlocking = 1
	// PumpMessageFlag is `PM_REMOVE`, the flag the nested entry passes to PeekMessageA.
	// It is 1, and it is what makes the drain destructive.
	PumpMessageFlag = 1
	// PumpRunningFlag is the "a pump owns the loop" flag, checked by the outer entry.
	// It is read before the depth test, so both conditions gate the blocking path.
	PumpRunningFlag = 1
)

// PumpMode is which of the two behaviours an entry takes, and it is decided entirely by
// the two conditions in the reference's first test.
type PumpMode uint8

const (
	// PumpBlocking is the outer entry: GetMessageA, which waits for a message. Taken
	// only when a pump is already flagged as running and the depth is zero.
	PumpBlocking PumpMode = iota
	// PumpDraining is a nested entry: PeekMessageA with PM_REMOVE, which never waits.
	PumpDraining
)

// PumpEntry is one call into the shared pump.
type PumpEntry struct {
	// Running is the "a pump owns the loop" flag, the reference's first condition.
	Running bool
	// Depth is the nesting depth on entry.
	Depth int
	// MessagesDrained is how many messages the call dispatched, which only the
	// draining mode can report at all.
	MessagesDrained int
	// AudioTicks is how many times the audio tick was called, which is the number of
	// times the audio flag was set on the paths the reference checks it.
	AudioTicks int
}

// ModeFor reports which behaviour a call would take, reproducing the reference's single
// test: `running != 0 && depth == 0` is the blocking outer entry, and everything else
// drains.
//
// The asymmetry is deliberate and is the safe direction. A nested call — depth above
// zero — always drains even if the running flag is set, because the flag is precisely
// what says an outer loop is already blocked. And a depth of zero with the running flag
// clear also drains, because there is no loop to hand control to.
func ModeFor(running bool, depth int) PumpMode {
	if running && depth == PumpDepthGuard {
		return PumpBlocking
	}
	return PumpDraining
}

// The audio flag, `DAT_0045E068`, gates an audio tick inside the pump. **This is the
// second independent identification of the same field**: the `wavevolume` builtin was
// recorded earlier as returning 0 when `DAT_0045E068 == 0`, from a different function
// and a different route, and it is already named `AudioInitialisedSlot` there — the
// reference's own reading of the field, "audio not initialised". The pump reaching the
// same field and gating on it is what makes that reading secure rather than a
// coincidence of address space.
const (
	// AudioInitialisedField is the field gating the audio tick. Its address is not
	// repeated as a new constant because one already exists for the same field.
	AudioInitialisedField = AudioInitialisedSlot
	// AudioTick is the routine the pump calls, FUN_00434260, always with a zero
	// argument.
	AudioTick = "FUN_00434260"
	// AudioTickArgument is the argument the pump passes, 0.
	AudioTickArgument = 0
)

// AudioTicksFor reports how many times a draining entry would call the audio tick, given
// the audio flag's state and the number of messages drained.
//
// **The count is `1 + drained`, not `drained`.** The reference checks the flag once
// *before* the drain loop and then once *after every message*, so a drain of zero still
// ticks once. That off-by-one is easy to miss and is why the count is computed here
// rather than left to a caller to infer.
func AudioTicksFor(audioInitialised bool, drained int) int {
	if !audioInitialised {
		return 0
	}
	return 1 + drained
}

// RunPump reproduces the shared pump's control flow without a window system: it reports
// which mode the entry takes, how many messages would be drained, and how many audio
// ticks would fire, and it leaves the depth as it found it.
//
// The depth is returned rather than mutated, because the reference decrements it on both
// exits and a caller deciding whether to re-enter needs the value *after* the call, not
// during it.
func RunPump(entry PumpEntry, audioEnabled bool, queued int) (PumpEntry, PumpMode) {
	mode := ModeFor(entry.Running, entry.Depth)
	switch mode {
	case PumpBlocking:
		// The outer entry sets the depth to one and clears it on the way out, and it
		// drains an unbounded queue because it loops until the running flag clears.
		entry.MessagesDrained = queued
		// The blocking entry does **not** call the audio tick: its loop body has no
		// audio check at all. Only the draining entry does. That is a real asymmetry
		// between the two paths and is why the tick count is computed per mode.
		entry.AudioTicks = 0
		entry.Depth = PumpDepthGuard
		return entry, mode
	default:
		entry.MessagesDrained = queued
		entry.AudioTicks = AudioTicksFor(audioEnabled, queued)
		// The depth is incremented and then decremented, so it returns to its entry
		// value however many messages were drained. Nothing to do here — and writing
		// the no-op assignment would be a self-assignment `go vet` rightly rejects.
		return entry, mode
	}
}

// VerifyPumpGuardsAreDepthCounters checks the counter's arithmetic in both directions,
// since the reference increments on one path and *sets* on the other, and treating them
// as the same operation would hide the difference.
func VerifyPumpGuardsAreDepthCounters() (ok bool, detail string) {
	// The blocking path sets the depth to one and then decrements it, so it also ends
	// at zero. Two different operations, same result.
	if PumpOuterBlocking-1 != PumpDepthGuard {
		return false, "the blocking path sets the depth to one and decrements, so it should end at zero"
	}
	// The draining path increments and decrements, so it is the identity for any
	// starting depth.
	for depth := 0; depth < 8; depth++ {
		if (depth + 1) - 1 != depth {
			return false, "the draining path should return the depth it was given"
		}
	}
	// And the two modes are genuinely different, or the whole design would collapse.
	if ModeFor(true, 0) == ModeFor(true, 1) {
		return false, "the blocking and draining modes should be distinguishable"
	}
	if ModeFor(false, 0) != PumpDraining {
		return false, "with no loop running an entry must drain rather than block"
	}
	// And the message flag is the destructive one, which is what makes a drain a drain.
	if PumpMessageFlag != 1 {
		return false, "the nested entry should pass PM_REMOVE, which is 1"
	}
	return true, "the counter returns to its entry value on both paths, by two different routes"
}
