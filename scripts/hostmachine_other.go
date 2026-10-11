//go:build !windows

package scripts

import "runtime"

// HostMemoryStatus is freemem's reading off Windows. There is no
// GlobalMemoryStatus here, so it reports the Go runtime's view (see
// runtimeMemoryStatus). The value is a port policy.
func HostMemoryStatus() (pageFile, virtual, totalPhysical uint32) {
	return runtimeMemoryStatus()
}

// HostProcessorType is machinespeed's value off Windows. There is no
// GetSystemInfo here, so it is the per-GOARCH policy value (see
// fallbackProcessorType).
func HostProcessorType() uint32 {
	return fallbackProcessorType(runtime.GOARCH)
}
