//go:build windows

package native

import (
	"syscall"
)

var nativeTickProc = syscall.NewLazyDLL("winmm.dll").NewProc("timeGetTime")

func NativeTickMilliseconds() uint32 {
	if ms, ok := virtualTickMilliseconds(); ok {
		return ms
	}
	tick, _, _ := nativeTickProc.Call()
	return uint32(tick)
}
