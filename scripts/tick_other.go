//go:build !windows

package scripts

import "time"

var nativeTickStart = time.Now()

func NativeTickMilliseconds() uint32 {
	return uint32(time.Since(nativeTickStart) / time.Millisecond)
}
