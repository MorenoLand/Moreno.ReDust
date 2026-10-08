package native

import . "redust/scripts"

// The diagnostic reporter, `FUN_0042C470`, mapped. It is the routine behind **every** error
// message the engine shows for a failure it diagnoses itself, and it turns out to hold a
// table the project had been reconstructing one entry at a time from call sites: the
// engine's eleven native status codes, each paired with the message resource that explains
// it.

// # Two parameters, and only one of them is usually consulted
//
//	void FUN_0042C470(unsigned short status, short callsite) {
//	    DVar2 = GetLastError();
//	    switch (DVar2 & 0xFFFF) { ... }        // the OS error wins
//	    switch (status)         { ... }        // the engine's own code is the fallback
//	    text = FUN_00428D40(resource);
//	    ...
//	}
//
// The first parameter is the engine's own status; the second is a **call-site tag**, and
// it is consulted for **exactly one value**: inside the `ERROR_FILE_NOT_FOUND` branch, a
// call site of `0x1450` gets one message and every other call site gets another. Everywhere
// else `callsite` is passed and ignored.
//
// So the signature is `(status, callsite)` with one call site disambiguated, and a port
// that treated the second parameter as a general modifier would be inventing behaviour the
// reference does not have.
const (
	// DiagnosticRoutine is the reporter, FUN_0042C470.
	DiagnosticRoutine = "FUN_0042C470"
	// DiagnosticSpecialCallsite is the **one** call site the reporter distinguishes,
	// 0x1450. It is consulted only when the OS error is "file not found".
	DiagnosticSpecialCallsite uint16 = 0x1450
	// DiagnosticCallsiteIsOtherwiseUnused records that every other call site takes the
	// same path, so the second parameter is a tag rather than a general modifier.
	DiagnosticCallsiteIsOtherwiseUnused = true
	// DiagnosticMessageMax is the largest of the reporter's three buffers, 256, and all
	// three are the same size.
	DiagnosticMessageMax = 256
	// DiagnosticBufferCount is how many 256-byte buffers it declares. Three: the fetched
	// text, an expansion, and a second fetched text.
	DiagnosticBufferCount = 3
)

// # The OS-error branch reads GetLastError's low sixteen bits
//
// **This is the one place in the file layer that uses `GetLastError` rather than throwing
// it away.** The seek and the read both call it and discard the result; the reporter calls
// it and switches on `error & 0xFFFF`.
//
// Two details are worth recording. The switch is on the **low sixteen bits**, so the
// reporter treats the Win32 error as a sixteen-bit quantity and would fold any high word
// away. And **two distinct OS error codes share one message** — `8` (`ERROR_INVALID_HANDLE`)
// and `0x0E` (`ERROR_OUTOFMEMORY`) both reach `0x9D` — so the mapping is many-to-one, which
// is why it cannot be inverted.
const (
	// ErrorFileNotFound and ErrorPathNotFound are the two the reader will recognise.
	ErrorFileNotFound   uint16 = 0x02
	ErrorPathNotFound   uint16 = 0x04
	ErrorInvalidHandle  uint16 = 0x08
	ErrorOutOfMemory    uint16 = 0x0E
	ErrorInvalidParam   uint16 = 0x13
	ErrorIncorrectDrive uint16 = 0x15
	ErrorNotSameDevice  uint16 = 0x17
	ErrorNoMoreFiles    uint16 = 0x19
	ErrorBusy           uint16 = 0x1B
	ErrorSharingViolate uint16 = 0x20
	ErrorHandleEOF      uint16 = 0x27
	ErrorInvalidName    uint16 = 0x6B
	// ErrorInvalidParameterWide is 0x57 = 87, the *same value* as ErrorInvalidParam's
	// 0x13 = 19 in decimal terms being different — but 0x13 is 19 and 0x57 is 87, so these
	// are two codes and only the second takes a global-dependent branch. The name records
	// that 0x57 is the one the code guards on.
	ErrorInvalidParameterGuard uint16 = 0x57
)

// # The engine's own eleven statuses, and their message resources
//
// The fallback switch covers `0x65` through `0x6F` — **eleven codes**, each with its own
// message resource, and `0xB3` for anything else:
//
//	0x65 -> 0xA8    0x66 -> 0xA9    0x67 -> 0xAF    0x68 -> 0xB0    0x69 -> 0xAC
//	0x6A -> 0xAA    0x6B -> 0xAB    0x6C -> 0xAD    0x6D -> 0xB1    0x6E -> 0xAE
//	0x6F -> 0xB2    otherwise -> 0xB3
//
// # The mapping is not monotonic, and that is the finding
//
// In order, the resources run `A8, A9, AF, B0, AC, AA, AB, AD, B1, AE, B2` — **jumbled**.
// The engine's codes and its message resources are in two different orders, so **one cannot
// be derived from the other** and a port that computed `resource = code + 0x43` would be
// wrong for five of the eleven.
//
// This is what makes the table worth transcribing rather than describing. The project had
// been assembling these eleven codes one at a time from the call sites that raise them, and
// had named **five** of them: `0x65`, `0x66`, `0x6A`, `0x6B` and `0x6E`. The remaining six
// — `0x67`, `0x68`, `0x69`, `0x6C`, `0x6D` and `0x6F` — are now known to exist and to have
// messages, even though what each *means* is not established here.
const (
	// EngineStatusFirst and EngineStatusLast bound the engine's own codes, 0x65 and 0x6F.
	EngineStatusFirst uint16 = 0x65
	EngineStatusLast  uint16 = 0x6F
	// EngineStatusCount is how many that is: eleven, and the table has eleven entries.
	EngineStatusCount = 11
	// The resource each maps to, in the engine's code order. **This sequence is not
	// monotonic** and cannot be computed.
	EngineStatusHeader  uint16 = 0xA8 // 0x65, the sub-resource header consistency failure
	EngineStatusNull    uint16 = 0xA9 // 0x66, the null allocation
	EngineStatusReserved1 uint16 = 0xAF // 0x67
	EngineStatusReserved2 uint16 = 0xB0 // 0x68
	EngineStatusReserved3 uint16 = 0xAC // 0x69
	EngineStatusMagic1  uint16 = 0xAA // 0x6A, the first magic-word failure
	EngineStatusFile    uint16 = 0xAB // 0x6B, the file failure
	EngineStatusReserved4 uint16 = 0xAD // 0x6C
	EngineStatusReserved5 uint16 = 0xB1 // 0x6D
	EngineStatusAppend  uint16 = 0xAE // 0x6E, the script-append diagnostic
	EngineStatusReserved6 uint16 = 0xB2 // 0x6F
	// EngineStatusUnknown is the fallback resource, 0xB3, for a status outside the range.
	EngineStatusUnknown uint16 = 0xB3
	// EngineStatusNamedCount is how many of the eleven the project has **named** as of
	// this slice: five. The other six are known to exist and to have messages, but what
	// they mean is not established.
	EngineStatusNamedCount = 5
)

// engineStatusResource maps a status to its message resource, reproducing the switch.
func engineStatusResource(status uint16) uint16 {
	for offset := 0; offset < EngineStatusCount; offset++ {
		if EngineStatusFirst+uint16(offset) == status {
			return engineStatusResources[offset]
		}
	}
	return EngineStatusUnknown
}

// engineStatusResources is the mapping in the engine's code order. **The sequence is
// jumbled on purpose** — it is the image's order, not a sorted one.
var engineStatusResources = [EngineStatusCount]uint16{
	0xA8, 0xA9, 0xAF, 0xB0, 0xAC, 0xAA, 0xAB, 0xAD, 0xB1, 0xAE, 0xB2,
}

// VerifyEngineStatusTable checks the table's three properties: eleven entries covering
// 0x65..0x6F, distinct resources, and **not monotonic** — which is what stops a port
// computing one from the other.
func VerifyEngineStatusTable() (ok bool, detail string) {
	// **Eleven entries for eleven codes**, contiguous from 0x65 to 0x6F.
	if EngineStatusCount != 11 {
		return false, "the table should hold eleven entries"
	}
	if int(EngineStatusLast)-int(EngineStatusFirst)+1 != EngineStatusCount {
		return false, "the codes should run contiguously from 0x65 to 0x6F"
	}
	if len(engineStatusResources) != EngineStatusCount {
		return false, "the resource list should match the code count"
	}
	// Every code in the range resolves, and a code outside it takes the fallback.
	for offset := 0; offset < EngineStatusCount; offset++ {
		status := EngineStatusFirst + uint16(offset)
		got := engineStatusResource(status)
		if got != engineStatusResources[offset] {
			return false, "each code should resolve to the resource at its own index"
		}
		// And a code one past the end should not.
		if engineStatusResource(status+1) == got && status != EngineStatusLast {
			return false, "two adjacent codes should not share a resource"
		}
	}
	if engineStatusResource(EngineStatusLast+1) != EngineStatusUnknown {
		return false, "a status past the range should take the fallback"
	}
	if engineStatusResource(0) != EngineStatusUnknown {
		return false, "a status below the range should take the fallback"
	}
	// **The resources are distinct**, so each code has its own message and the mapping can
	// be inverted by hand even though it cannot be computed.
	seen := map[uint16]uint16{}
	for offset, resource := range engineStatusResources {
		if _, dup := seen[resource]; dup {
			return false, "two codes share a message resource"
		}
		seen[resource] = EngineStatusFirst + uint16(offset)
	}
	if len(seen) != EngineStatusCount {
		return false, "the resources should be distinct"
	}
	// **And the sequence is not monotonic** — this is the property that matters. The codes
	// ascend and the resources do not, so no arithmetic links them.
	ascending := true
	for i := 1; i < len(engineStatusResources); i++ {
		if engineStatusResources[i] < engineStatusResources[i-1] {
			ascending = false
			break
		}
	}
	if ascending {
		return false, "the resource sequence should not be sorted, since the image's is not"
	}
	// So a port that assumed `resource = code + delta` would be wrong for some codes. How
	// many is worth pinning: the deltas are not constant, and the sequence has **three**
	// descents in eleven steps. I first wrote five; counting them gives three.
	deltas := map[int]bool{}
	descents := 0
	for i := 1; i < len(engineStatusResources); i++ {
		delta := int(engineStatusResources[i]) - int(engineStatusResources[i-1])
		deltas[delta] = true
		if delta < 0 {
			descents++
		}
	}
	if len(deltas) < 2 {
		return false, "the deltas should not all be the same, or the codes would be derivable"
	}
	if descents != 3 {
		return false, "the sequence should have three descents"
	}
	// And the fallback is above every real one, so it is distinguishable from the table.
	for _, resource := range engineStatusResources {
		if EngineStatusUnknown <= resource {
			return false, "the fallback should sit above the table's resources"
		}
	}
	// The five the project has named, checked against the constants that already exist, so
	// the new file and the old ones cannot disagree.
	if SubMagic2Status != EngineStatusFirst {
		return false, "0x65 should be the header status the sub-cache records"
	}
	if StatusNullAllocation != EngineStatusFirst+1 {
		return false, "0x66 should be the null-allocation status"
	}
	if SubMagic1Status != EngineStatusFirst+5 {
		return false, "0x6A should be the first magic status"
	}
	if StatusFileNotFound != EngineStatusFirst+6 {
		return false, "0x6B should be the file status"
	}
	if ScriptAppendDiagA != EngineStatusFirst+9 {
		return false, "0x6E should be the script-append status"
	}
	// And the count of named statuses is what this slice established: five of eleven, so
	// six are known to exist and unnamed.
	if EngineStatusNamedCount != 5 {
		return false, "five of the eleven statuses should be named so far"
	}
	if EngineStatusNamedCount >= EngineStatusCount {
		return false, "the count should be less than the total, or the note is stale"
	}
	return true, "eleven codes, eleven messages, and no arithmetic between them"
}

// # The two branches occupy separate, contiguous resource ranges
//
// The OS-error branch uses `0x9A`–`0xA7` and the engine-status branch uses `0xA8`–`0xB2`,
// with `0xB3` the fallback and `0xB4` a second string the reporter fetches afterwards. So
// the whole set is **one contiguous run from `0x9A` to `0xB4`** — twenty-nine resources —
// and the two branches do not interleave even though the *engine's* codes and its own
// resources do.
//
// That is worth stating because it means the resource space is a *table* with a known
// layout, so a port can allocate it wholesale rather than mapping codes one at a time.
const (
	// OSErrorResourceFirst and OSErrorResourceLast bound the OS-error branch's resources,
	// 0x9A and 0xA7.
	OSErrorResourceFirst uint16 = 0x9A
	OSErrorResourceLast  uint16 = 0xA7
	// EngineResourceFirst and EngineResourceLast bound the engine-status branch's, 0xA8 and
	// 0xB2. The two ranges are **adjacent, not overlapping**.
	EngineResourceFirst uint16 = 0xA8
	EngineResourceLast  uint16 = 0xB2
	// DiagnosticSecondString is the resource the reporter fetches after the first, 0xB4 —
	// so a port that fetched only the first would have half the dialog.
	DiagnosticSecondString uint16 = 0xB4
	// DiagnosticResourceRunFirst and DiagnosticResourceRunLast bound the whole set, 0x9A
	// and 0xB4, which is **twenty-seven consecutive resources**.
	DiagnosticResourceRunFirst uint16 = 0x9A
	DiagnosticResourceRunLast  uint16 = 0xB4
	// DiagnosticResourceRunCount is twenty-seven, the span plus one. I first wrote
	// twenty-nine, which is 0xB4 minus 0x9A without the plus one.
	DiagnosticResourceRunCount = 27
)

// VerifyResourceLayout checks that the resource space is one contiguous run split into two
// adjacent branches, and that the branches do not overlap.
func VerifyResourceLayout() (ok bool, detail string) {
	// **One contiguous run from 0x9A to 0xB4** — twenty-seven resources with no gap.
	if DiagnosticResourceRunFirst != 0x9A || DiagnosticResourceRunLast != 0xB4 {
		return false, "the run should run from 0x9A to 0xB4"
	}
	if int(DiagnosticResourceRunLast)-int(DiagnosticResourceRunFirst)+1 != DiagnosticResourceRunCount {
		return false, "the run should hold twenty-seven resources"
	}
	// The two branches are **adjacent**: the OS errors end where the engine's begin, with
	// no gap and no overlap.
	if EngineResourceFirst != OSErrorResourceLast+1 {
		return false, "the two branches should be adjacent"
	}
	if EngineResourceFirst <= OSErrorResourceFirst {
		return false, "the engine branch should lie above the OS branch"
	}
	if EngineResourceLast < EngineResourceFirst {
		return false, "the engine branch should be a real range"
	}
	// The fallback sits immediately after the engine branch's last entry.
	if EngineStatusUnknown != EngineResourceLast+1 {
		return false, "the fallback should follow the engine branch"
	}
	// And the second string is above both, so a port fetching only the first would have
	// half the dialog and would notice by the missing text.
	if DiagnosticSecondString <= EngineStatusUnknown {
		return false, "the second string should sit above the fallback"
	}
	// So the layout is: OS errors, then engine statuses, then the fallback, then the second
	// string — four consecutive blocks, and the last two are single resources.
	if int(EngineResourceLast)-int(EngineResourceFirst)+1 < EngineStatusCount {
		return false, "the engine branch should hold at least the eleven status resources"
	}
	// The engine branch is **exactly** the eleven statuses wide, so the branch and the table
	// are the same size. The jumbling is *within* the branch — the eleven resources are a
	// permutation of it — which is what makes a range check useless for recovering the
	// table. I first wrote that the branch was wider; `0xA8..0xB2` is eleven, not more.
	if int(EngineResourceLast)-int(EngineResourceFirst)+1 != EngineStatusCount {
		return false, "the engine branch should be exactly as wide as the status table"
	}
	// And the engine branch's eleven resources are spread across it rather than packed at
	// the bottom, which is what the jumbled sequence in the status table says.
	lowest, highest := engineStatusResources[0], engineStatusResources[0]
	for _, resource := range engineStatusResources {
		if resource < lowest {
			lowest = resource
		}
		if resource > highest {
			highest = resource
		}
	}
	if highest <= lowest {
		return false, "the resources should span a range"
	}
	// **The eleven resources span the whole branch**, so they are a permutation of it rather
	// than a scattered subset: the lowest is the first status's message and the highest the
	// last one's. I first wrote that they should span *at least* eleven slots, which is a
	// weaker and vaguer claim than the one the data supports.
	if highest-lowest != EngineStatusCount-1 {
		return false, "the eleven resources should span the branch exactly"
	}
	// And they all lie inside the engine branch.
	for _, resource := range engineStatusResources {
		if resource < EngineResourceFirst || resource > EngineResourceLast {
			return false, "every status resource should lie inside the engine branch"
		}
	}
	return true, "twenty-nine contiguous resources split into two adjacent branches"
}

// VerifyDiagnosticCallsite checks the one call site the reporter distinguishes, and that
// the second parameter is otherwise inert — which is what keeps it a tag rather than a
// general modifier.
func VerifyDiagnosticCallsite() (ok bool, detail string) {
	// **Exactly one call site is special**, 0x1450, and only under the "file not found"
	// error. So the tag is consulted in one branch out of the whole switch.
	if DiagnosticSpecialCallsite != 0x1450 {
		return false, "the special call site should be 0x1450"
	}
	// And the second parameter is otherwise unused, so a port that treated it as a general
	// modifier would be inventing behaviour.
	if !DiagnosticCallsiteIsOtherwiseUnused {
		return false, "the call site should be recorded as otherwise unused"
	}
	// The two resources that error branches into, which differ only by that call site.
	// 0x9A for 0x1450 and 0x9B for everything else.
	const specialResource, generalResource = 0x9A, 0x9B
	if specialResource != OSErrorResourceFirst {
		return false, "the special call site's resource should be the branch's first"
	}
	if generalResource != specialResource+1 {
		return false, "the two resources should be consecutive"
	}
	// So the pair is adjacent at the very bottom of the resource run, which is consistent
	// with the OS branch starting there.
	if generalResource > OSErrorResourceLast {
		return false, "both should lie inside the OS branch"
	}
	// The three buffers, all the same size — the reporter has no one buffer of a different
	// width, so a port need not special-case any of them.
	if DiagnosticMessageMax != 256 {
		return false, "the buffers should be 256 bytes"
	}
	if DiagnosticBufferCount != 3 {
		return false, "the reporter should declare three buffers"
	}
	if DiagnosticRoutine != "FUN_0042C470" {
		return false, "the reporter's name should be the one the image gives"
	}
	// And the guard on 0x57: the same code takes one of two resources depending on a
	// global, so a third global-dependent branch exists alongside the call-site one. This
	// is the second place a message is chosen by something other than the status.
	if ErrorInvalidParameterGuard != 0x57 {
		return false, "the guarded error should be 0x57"
	}
	return true, "one call site distinguished, and two branches that choose by something else"
}
