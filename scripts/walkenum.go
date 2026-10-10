package scripts

// walkingActors lists the actors that hold a walk or turn job, in load order. The
// native table has sixteen walk slots (DAT_00442E68); the port keeps a job on each
// actor, so an actor with a job stands in for an occupied slot.
func (t *ScriptActors) walkingActors() []*ActorRecord {
	var walking []*ActorRecord
	for _, key := range t.order {
		if actor := t.actors[key]; actor != nil && actor.Job != nil {
			walking = append(walking, actor)
		}
	}
	return walking
}

// ActiveWalkCount is countwalks: the occupied walk slots (FUN_004105E0).
func (t *ScriptActors) ActiveWalkCount() int {
	if t == nil {
		return 0
	}
	return len(t.walkingActors())
}

// ActiveWalkName is indextowalk: the actor on the 1-based walk slot, false when the
// index is outside the count (FUN_00410600 names the slot's actor at +0x32).
func (t *ScriptActors) ActiveWalkName(index int) (string, bool) {
	if t == nil {
		return "", false
	}
	walking := t.walkingActors()
	if index < 1 || index > len(walking) {
		return "", false
	}
	return walking[index-1].Name, true
}
