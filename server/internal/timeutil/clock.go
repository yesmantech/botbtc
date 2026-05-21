package timeutil

import "time"

// NowMs returns current time in milliseconds since epoch.
func NowMs() int64 {
	return time.Now().UnixMilli()
}

// ElapsedMs returns milliseconds elapsed since the given ms timestamp.
func ElapsedMs(startMs int64) int64 {
	return NowMs() - startMs
}

// IsExpired checks if a given timestamp is older than maxAgeMs.
func IsExpired(timestampMs int64, maxAgeMs int64) bool {
	return ElapsedMs(timestampMs) > maxAgeMs
}
