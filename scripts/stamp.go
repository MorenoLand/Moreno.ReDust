package scripts

import "sync"

// Version stamps. The native game keeps one process-wide counter, DAT_0044200C.
// FUN_004012A0 increments it and returns the new value. Its decompile is void, but
// its disassembly is
//
//	mov eax,[0044200C]; inc eax; mov [0044200C],eax; ret
//
// so EAX on return is the new value. Four sites store that EAX in a record:
//
//	FUN_0040C1F0  actor constructor            stores EAX at actor +0x30
//	FUN_0040A860  actorinstance copy           stores EAX at the copy's +0x30
//	FUN_00420370  shop prop constructor        stores EAX at prop +0x2E
//	FUN_0041E840  propinstance copy            stores EAX at the copy's +0x2E
//
// The disassembly has no other direct reference to DAT_0044200C. A stamp does not change
// after it is minted: the write-back guards FUN_0040D800 (actor +0x30) and FUN_004214A0
// (prop +0x2E) compare it with the table row and raise 0x0FAA and 0x132F if it moved.
//
// The port models equality only. Its counter starts at zero, so a minted stamp is never
// zero, and zero marks a record that has none. The native starting value was not read,
// and the port's values are not the native's: the native counter also advances on every
// resource tick, which the port does not share.
var stampClock struct {
	mu   sync.Mutex
	last uint32
}

// mintStamp is FUN_004012A0 as the four minting sites above use it.
func mintStamp() uint32 {
	stampClock.mu.Lock()
	stampClock.last++
	stamp := stampClock.last
	stampClock.mu.Unlock()
	return stamp
}
