package scripts

// The pending-record table is the native input queue that the DFWINDOW window
// procedure fills and that stilldown reads. Its verified layout (FUN_00429160
// and FUN_0042C940) is a count at DAT_00445718 and 18-byte records at
// DAT_00443FB8: kind (u16) at +0, arg3 (i32) at +2, the point (u32) at +10,
// param5 (u16) at +14, and the flag (u16) at +16.
//
// The window procedure's button-up path enqueues kind 2 with flag 4: WM_LBUTTONUP
// and WM_RBUTTONUP both reach 0x429E67, which packs the point, converts it with
// ClientToScreen (import 0x4614A4) and calls FUN_00429160(0, 2, 0, &point, 0) at
// 0x429EB3. Bit 4 is therefore set by real input, so stilldown is not a plain
// GetKeyState test.
//
// Ported consumers: puppetevent (FUN_00409F90) clears the table on entry and
// removes one mask-0x0E record per poll (FUN_0040A2E0), and opening a set
// (FUN_004195F0) clears it. Not ported: the movie selector wait FUN_004037A0,
// whose mask-0x1E removal is FUN_00403E20; the resets inside FUN_00402CE0
// (playmovie) and FUN_00423F20 (opengame); and the kind 0x2A activation record,
// whose flag is the stale word at FUN_00429160's [ESP+0xC] (see the ledger,
// 2026-10-11).

const (
	// pendingCapacity is the most records the table holds before FUN_00429160
	// refuses one.
	pendingCapacity = 16
	// pendingButtonUpKind is the record kind the button-up path writes.
	pendingButtonUpKind = 2
	// pendingButtonUpFlag is the flag FUN_00429160's switch gives kind 2.
	pendingButtonUpFlag = 0x4
	// pendingStilldownMask is the mask FUN_0042C7E0 passes to FUN_0042C940.
	pendingStilldownMask = 0x4
)

// pendingRecord is one 18-byte table entry, with the fields the stilldown path
// reads. Arg3 and Param5 are kept as the native writes them.
type pendingRecord struct {
	Kind   uint16
	Arg3   int32
	Point  uint32
	Param5 uint16
	Flag   uint16
}

// pendingTable is the native queue. The zero value is the empty table, which is
// the state the native code starts from.
type pendingTable struct {
	records []pendingRecord
}

// enqueue appends a record as FUN_00429160 does with no removal. A table that
// already holds pendingCapacity records is not grown: the native code sets the
// count to 15, beeps (FUN_0042B720) and drops the new record, so the table
// keeps 15 records and the sixteenth is lost. It reports whether the record was
// stored.
func (t *pendingTable) enqueue(record pendingRecord) bool {
	if len(t.records) >= pendingCapacity {
		t.records = t.records[:pendingCapacity-1]
		return false
	}
	t.records = append(t.records, record)
	return true
}

// first is FUN_0042C940: the first record, in table order, whose flag shares a
// bit with mask. It does not remove the record.
func (t *pendingTable) first(mask uint16) (pendingRecord, bool) {
	for _, record := range t.records {
		if record.Flag&mask != 0 {
			return record, true
		}
	}
	return pendingRecord{}, false
}

// remove is FUN_0042C890: the first record whose flag shares a bit with mask is
// taken out, and the records after it move down one place.
func (t *pendingTable) remove(mask uint16) (pendingRecord, bool) {
	for i, record := range t.records {
		if record.Flag&mask != 0 {
			t.records = append(t.records[:i], t.records[i+1:]...)
			return record, true
		}
	}
	return pendingRecord{}, false
}

// reset is FUN_0042C9A0 without its pump: the count goes to 0.
func (t *pendingTable) reset() {
	t.records = t.records[:0]
}

// puppetPoll is one pass of the FUN_00409F90 loop. FUN_0040A2E0 begins by
// removing the first mask-0x0E record (FUN_0042C890(0x0E)). Only that removal is
// ported. The puppet presenter, not this table, interprets the event.
func (t *pendingTable) puppetPoll() {
	t.remove(0x0E)
}

// stilldown is FUN_00415E90 through FUN_0042C7E0. A pending flag-4 record makes
// the result 0 and leaves the record queued. Otherwise the result is 1 when the
// left button is down, which is GetKeyState(VK_LBUTTON) < 0 in the native code.
func (t *pendingTable) stilldown(leftDown bool) bool {
	if _, found := t.first(pendingStilldownMask); found {
		return false
	}
	return leftDown
}

// buttonUpRecord is the kind-2 record the window procedure writes on a button
// release: arg3 0, the point, param5 0, and flag 4.
func buttonUpRecord(point uint32) pendingRecord {
	return pendingRecord{Kind: pendingButtonUpKind, Arg3: 0, Point: point, Param5: 0, Flag: pendingButtonUpFlag}
}

// NoteMouse feeds the pending table and the left-button state from the game's
// per-frame mouse state. Each release is one WM_LBUTTONUP or WM_RBUTTONUP, so
// each enqueues a kind-2 record. The native record holds the screen point; the
// port passes its pointer point, which is a difference the stilldown result
// does not read.
func (h *GameHost) NoteMouse(point uint32, leftDown, leftReleased, rightReleased bool) {
	h.leftDown = leftDown
	if leftReleased {
		h.pending.enqueue(buttonUpRecord(point))
	}
	if rightReleased {
		h.pending.enqueue(buttonUpRecord(point))
	}
}
