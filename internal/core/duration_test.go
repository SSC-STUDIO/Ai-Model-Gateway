package core

import (
	"testing"
	"time"
)

func TestMillisecondsToDuration_UsesFallbackForNonPositiveValues(t *testing.T) {
	fallback := 30 * time.Second

	for _, ms := range []int{0, -1} {
		if got := MillisecondsToDuration(ms, fallback); got != fallback {
			t.Fatalf("expected fallback %v for %d, got %v", fallback, ms, got)
		}
	}
}

func TestMillisecondsToDuration_ClampsOverflowingValues(t *testing.T) {
	got := MillisecondsToDuration(int(180000000000000), 30*time.Second)
	if got != maxSafeDuration {
		t.Fatalf("expected clamped duration %v, got %v", maxSafeDuration, got)
	}
}
