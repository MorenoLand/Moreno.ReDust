//go:build !windows

package native

import (
	. "redust/scripts"
	"time"
)

var nativeTickStart = time.Now()

func NativeTickMilliseconds() uint32 {
	return uint32(time.Since(nativeTickStart) / time.Millisecond)
}
