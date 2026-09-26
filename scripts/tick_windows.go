//go:build windows

package scripts

import "syscall"

var nativeTickProc = syscall.NewLazyDLL("winmm.dll").NewProc("timeGetTime")

func NativeTickMilliseconds() uint32 {
	tick, _, _ := nativeTickProc.Call()
	return uint32(tick)
}
