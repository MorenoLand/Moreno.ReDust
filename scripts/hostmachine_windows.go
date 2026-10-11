//go:build windows

package scripts

import (
	"syscall"
	"unsafe"
)

var (
	windowsKernel32          = syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx = windowsKernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemInfo        = windowsKernel32.NewProc("GetSystemInfo")
)

// memoryStatusEx is MEMORYSTATUSEX. Its fields are 64-bit. freemem reads three
// of them, the same three the native MEMORYSTATUS holds as DWORDs.
type memoryStatusEx struct {
	length               uint32
	memoryLoad           uint32
	totalPhys            uint64
	availPhys            uint64
	totalPageFile        uint64
	availPageFile        uint64
	totalVirtual         uint64
	availVirtual         uint64
	availExtendedVirtual uint64
}

// systemInfo is SYSTEM_INFO. The pointer-sized fields are uintptr, so the
// layout follows the width of the running process, as the Windows structure
// does.
type systemInfo struct {
	processorArchitecture     uint16
	reserved                  uint16
	pageSize                  uint32
	minimumApplicationAddress uintptr
	maximumApplicationAddress uintptr
	activeProcessorMask       uintptr
	numberOfProcessors        uint32
	processorType             uint32
	allocationGranularity     uint32
	processorLevel            uint16
	processorRevision         uint16
}

// HostMemoryStatus is freemem's live reading: the available page file, the
// available virtual memory and the total physical memory of this process, from
// GlobalMemoryStatusEx. The native game made the same reading through
// GlobalMemoryStatus (FUN_0042C420), which fills DWORD fields. This port makes
// the 64-bit call instead. On a host with more than 4 GiB of RAM, the DWORD call
// made from a 64-bit process returned values that disagree with the Ex call
// (measured), so the Ex call is the reading the port's 64-bit build uses. Each
// field is then clamped to the DWORD width the native reads. The values depend
// on the process width (a 32-bit process has a smaller virtual address space)
// and on the machine and the moment, so they are host-dependent by design. The
// call has no failure path the native handler checks. It cannot fail on a
// supported Windows, and if it did, the zeroed struct would read as no memory.
func HostMemoryStatus() (pageFile, virtual, totalPhysical uint32) {
	var status memoryStatusEx
	status.length = uint32(unsafe.Sizeof(status))
	procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	return dwordClamp(status.availPageFile), dwordClamp(status.availVirtual), dwordClamp(status.totalPhys)
}

// HostProcessorType is machinespeed's live reading: the dwProcessorType of
// GetSystemInfo, as FUN_004279D0 returns it. The export is resolved by name
// first, as the native does, and the native's 0x182 is returned when it is
// missing. The value belongs to the running process: a 32-bit process on the
// measured 64-bit host reads a different value from a 64-bit one.
func HostProcessorType() uint32 {
	if err := procGetSystemInfo.Find(); err != nil {
		return processorIntel386
	}
	var info systemInfo
	procGetSystemInfo.Call(uintptr(unsafe.Pointer(&info)))
	return info.processorType
}
