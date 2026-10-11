package scripts

import "strings"

// ScriptPropsState is the saved prop state: the prop table in its order, the prop
// records the scripts have touched, the ball slots and the player's box. The native
// save keeps the prop block; the port's save carries this instead. Stamps is one
// stamp per table row (the +0x2E dword of each native row); a save written before stamps
// existed has none, and Restore mints them.
type ScriptPropsState struct {
	Table     []string        `json:"table"`
	Stamps    []uint32        `json:"stamps,omitempty"`
	Props     []PropRecord    `json:"props"`
	Balls     [16]PropBallJob `json:"balls"`
	PlayerBox [2]int16        `json:"playerBox"`
}

// Snapshot copies the prop state for a save.
func (t *ScriptProps) Snapshot() ScriptPropsState {
	state := ScriptPropsState{
		Table:     append([]string(nil), t.table...),
		Stamps:    append([]uint32(nil), t.stamps...),
		Balls:     t.Balls,
		PlayerBox: t.PlayerBox,
	}
	for _, key := range t.Names() {
		state.Props = append(state.Props, *t.props[key])
	}
	return state
}

// Restore replaces the prop state with a saved one. The native load reads the saved prop
// table block back (FUN_004235C0 stores it in DAT_004599E8), so the saved stamps are kept;
// how the native save lays out a row was not decoded. A save with no stamps gets a fresh
// one per row, which is what a native row always has. That is a port migration, not a
// native behaviour.
func (t *ScriptProps) Restore(state ScriptPropsState) {
	t.table = append([]string(nil), state.Table...)
	t.stamps = make([]uint32, len(t.table))
	if len(state.Stamps) == len(t.table) {
		copy(t.stamps, state.Stamps)
	} else {
		for index := range t.stamps {
			t.stamps[index] = mintStamp()
		}
	}
	t.props = make(map[string]*PropRecord, len(state.Props))
	for index := range state.Props {
		record := state.Props[index]
		if stamp, ok := t.StampOf(record.Name); ok {
			record.Stamp = stamp
		}
		t.props[strings.ToLower(record.Name)] = &record
	}
	t.Balls = state.Balls
	t.PlayerBox = state.PlayerBox
}
