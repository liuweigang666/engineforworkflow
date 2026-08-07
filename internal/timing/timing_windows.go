//go:build windows

// Package timing provides a fine-grained monotonic clock for the paper's
// benchmark programs. On Windows it is backed by QueryPerformanceCounter
// (typically 100 ns ticks on modern hardware), which gives sub-microsecond
// resolution for per-stage latency measurements.
package timing

import (
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	procQPC  = kernel32.NewProc("QueryPerformanceCounter")
	procQPF  = kernel32.NewProc("QueryPerformanceFrequency")

	freqOnce    sync.Once
	qpcHz       int64
	qpcFallback = false
)

func qpcFrequency() int64 {
	freqOnce.Do(func() {
		var f int64
		r1, _, _ := procQPF.Call(uintptr(unsafe.Pointer(&f)))
		if r1 == 0 || f <= 0 {
			// Fallback: assume 1 ns ticks if the frequency query fails.
			f = int64(time.Second)
			qpcFallback = true
		}
		qpcHz = f
	})
	return qpcHz
}

// Now returns a monotonic timestamp in nanoseconds since an arbitrary epoch.
// It is safe for measuring durations; do not interpret the absolute value.
func Now() time.Duration {
	var c int64
	r1, _, _ := procQPC.Call(uintptr(unsafe.Pointer(&c)))
	if r1 == 0 {
		return time.Duration(time.Now().UnixNano())
	}
	return time.Duration(c * int64(time.Second) / qpcFrequency())
}

// Since returns the elapsed time between t and Now.
func Since(t time.Duration) time.Duration {
	return Now() - t
}
