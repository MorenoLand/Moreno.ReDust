package scripts

import "strings"

// ScriptPropsState is the saved prop state: the prop table in its order, the prop
// records the scripts have touched, the ball slots and the player's box. The native
// save keeps the prop block; the port's save carries this instead.
type ScriptPropsState struct {
	Table     []string        `json:"table"`
	Props     []PropRecord    `json:"props"`
	Balls     [16]PropBallJob `json:"balls"`
	PlayerBox [2]int16        `json:"playerBox"`
}

// Snapshot copies the prop state for a save.
func (t *ScriptProps) Snapshot() ScriptPropsState {
	state := ScriptPropsState{
		Table:     append([]string(nil), t.table...),
		Balls:     t.Balls,
		PlayerBox: t.PlayerBox,
	}
	for _, key := range t.Names() {
		state.Props = append(state.Props, *t.props[key])
	}
	return state
}

// Restore replaces the prop state with a saved one.
func (t *ScriptProps) Restore(state ScriptPropsState) {
	t.props = make(map[string]*PropRecord, len(state.Props))
	for index := range state.Props {
		record := state.Props[index]
		t.props[strings.ToLower(record.Name)] = &record
	}
	t.table = append([]string(nil), state.Table...)
	t.Balls = state.Balls
	t.PlayerBox = state.PlayerBox
}
