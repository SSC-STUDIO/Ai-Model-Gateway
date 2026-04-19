package core

import "time"

const maxMillisecondsDuration = int64((1<<63 - 1) / int64(time.Millisecond))

var maxSafeDuration = time.Duration(maxMillisecondsDuration) * time.Millisecond

// MillisecondsToDuration converts config millisecond values to time.Duration
// without overflowing time.Duration's int64 range.
func MillisecondsToDuration(ms int, fallback time.Duration) time.Duration {
	if ms <= 0 {
		return fallback
	}
	if int64(ms) > maxMillisecondsDuration {
		return maxSafeDuration
	}
	return time.Duration(ms) * time.Millisecond
}
