package story

import "redust/assets"

func SallowerDoorAt(viewResource uint32, direction int16, point uint32) (string, bool) {
	x, y := int16(point>>16), int16(point)
	if viewResource == 52 && direction == assets.SetDirectionEast && x > 144 && y > 7 && x < 387 && y < 264 {
		return "salout", true
	}
	return "", false
}

func HotLowerDoorAt(viewResource uint32, direction int16, point uint32) (string, bool) {
	x, y := int16(point>>16), int16(point)
	if viewResource == 34 && direction == assets.SetDirectionWest && x > 128 && y > 73 && x < 394 && y < 262 {
		return "hotout", true
	}
	return "", false
}

func HotLowerExitToTown(viewResource uint32, direction int16, owner string) (string, bool) {
	if viewResource == 34 && direction == assets.SetDirectionWest && owner == "hotout" {
		return "west", true
	}
	return "", false
}

func SallowerExitToTown(direction int16, owner string) bool {
	return direction == assets.SetDirectionEast && owner == "salout"
}

// SallowerKeyScripted reports whether a movement key in the Sallowers goes to
// the scene chain (sendtoscene keydown, BOOTFILE keydown) rather than to the
// hand-coded move. The chain's set script walks with currentscene("strait"),
// "left" and "right", and the scene scripts it reaches use only implemented
// statements (voicesound, passcode). Three cases stay hand-coded:
//   - Scene D1 forward with the door open: the gototown branch exits to the
//     townscene global, which the town door sets (NITE.SET "scene g8"), while the
//     hand-coded exit uses townReturnScene. On a save loaded into the Sallowers
//     the two disagreed, so the exit stays hand-coded until that is settled.
//   - Scene D6 forward: gotointerior("salupper.set") switches the set and the
//     scene script has no exitcode after it, so the chain continues into the
//     Sallower set script's currentscene("strait") in the upper set. That
//     continuation is unverified natively.
//   - Scene D5 (any key): the forward branch sends Oona mousedown(0) and reads
//     oonaphase, neither verified in the port.
func SallowerKeyScripted(viewResource uint32, direction int16, key, owner string) bool {
	if key != "uparrow" && key != "leftarrow" && key != "rightarrow" {
		return false
	}
	switch viewResource {
	case 56:
		return false
	case 52:
		return !(direction == assets.SetDirectionEast && key == "uparrow" && owner == "salout")
	case 57:
		return !(direction == assets.SetDirectionWest && key == "uparrow")
	}
	return true
}

// SallowerCursorScripted reports whether the cursor in the Sallowers follows the
// scene's setcursor script. Only scenes whose click is routed or hand-coded
// qualify: Scene D3's setcursor shows "touch" over the slots, but its click stays
// hand-coded, so the cursor stays an arrow there.
func SallowerCursorScripted(viewResource uint32) bool {
	switch viewResource {
	case 43, 46, 47, 48, 52:
		return true
	}
	return false
}

// SallowerMouseScripted reports whether a click that no door or actor took goes
// to the scene chain. Scenes B4 (rules), C1, C2 and C3 (nude) open a spot movie
// through sendtostage. Scene D1's rice door stays in SallowerDoorAt, and Scene D3
// stays hand-coded: its slot branch calls path(6, ...), which the port does not
// implement.
func SallowerMouseScripted(viewResource uint32) bool {
	switch viewResource {
	case 43, 46, 47, 48:
		return true
	}
	return false
}
