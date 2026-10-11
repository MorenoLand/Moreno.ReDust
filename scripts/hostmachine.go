package scripts

import "runtime/metrics"

// processorIntel386 is PROCESSOR_INTEL_386 (386, 0x182). FUN_004279D0 returns
// it when the GetSystemInfo export is missing, so it is the native's own
// value for a host that reports no processor type.
const processorIntel386 = 0x182

// processorIntelPentium is PROCESSOR_INTEL_PENTIUM (586). A 32-bit x86 process
// on the Windows host measured for this port (WOW64) reads this value from
// GetSystemInfo, so the 386 policy value below matches what a 32-bit Windows
// process sees.
const processorIntelPentium = 586

// processorAMDX8664 is PROCESSOR_AMD_X8664 (8664), the value a 64-bit x64
// process reads from GetSystemInfo on the measured host.
const processorAMDX8664 = 8664

// fallbackProcessorType is machinespeed's value on a host that is not Windows.
// The native binary defines no value there, so this is a port policy, chosen
// per GOARCH: amd64 reports PROCESSOR_AMD_X8664 and 386 reports
// PROCESSOR_INTEL_PENTIUM. Every other GOARCH gets the native fallback, 0x182,
// so an unknown host reads as an unknown native host does.
func fallbackProcessorType(goarch string) uint32 {
	switch goarch {
	case "amd64":
		return processorAMDX8664
	case "386":
		return processorIntelPentium
	}
	return processorIntel386
}

// runtimeMemoryStatus is freemem's reading on a host that is not Windows. No
// GlobalMemoryStatus exists there, so it reports the Go runtime's own view. The
// available fields are the heap bytes the runtime has mapped that hold no
// objects: completely free spans, spans released to the system (still mapped)
// and unused slots in spans in use. The physical total is every byte the runtime
// has mapped. This is a port policy. It is the runtime's memory, not the
// machine's free memory.
func runtimeMemoryStatus() (pageFile, virtual, totalPhysical uint32) {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/free:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
		{Name: "/memory/classes/heap/unused:bytes"},
		{Name: "/memory/classes/total:bytes"},
	}
	metrics.Read(samples)
	available := sampleUint64(samples[0]) + sampleUint64(samples[1]) + sampleUint64(samples[2])
	total := sampleUint64(samples[3])
	return dwordClamp(available), dwordClamp(available), dwordClamp(total)
}

// sampleUint64 reads a runtime metric that is a byte count. Any other kind
// reads as zero.
func sampleUint64(sample metrics.Sample) uint64 {
	if sample.Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return sample.Value.Uint64()
}

// dwordClamp narrows a byte count to the DWORD width the native MEMORYSTATUS
// fields hold. Larger values saturate at 0xFFFFFFFF.
func dwordClamp(value uint64) uint32 {
	if value > 0xFFFFFFFF {
		return 0xFFFFFFFF
	}
	return uint32(value)
}
