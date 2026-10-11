//go:build !windows

package native

import (
	"time"
)

var nativeTickStart = time.Now()

func NativeTickMilliseconds() uint32 {
	if ms, ok := virtualTickMilliseconds(); ok {
		return ms
	}
	return uint32(time.Since(nativeTickStart) / time.Millisecond)
}
