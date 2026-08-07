//go:build !windows

package timing

import "time"

// Now returns a monotonic timestamp in nanoseconds since an arbitrary epoch.
// On non-Windows platforms it falls back to the wall clock (UnixNano).
func Now() time.Duration {
	return time.Duration(time.Now().UnixNano())
}

// Since returns the elapsed time between t and Now.
func Since(t time.Duration) time.Duration {
	return Now() - t
}
