// Package ratelimit provides a token-bucket rate limiter keyed by API key.
package ratelimit

import (
	"strings"
	"sync"
	"time"
)

const (
	// staleBucketTTL is how long an inactive bucket is kept before cleanup.
	staleBucketTTL = 5 * time.Minute
	// defaultMaxBuckets limits memory usage for random-key attacks.
	defaultMaxBuckets = 65_536
	// cleanupInterval triggers stale-bucket sweep every N Allow() calls.
	cleanupInterval = 256
)

// Limiter implements a per-API-key token-bucket rate limiter.
type Limiter struct {
	buckets    map[string]*bucket
	mu         sync.RWMutex
	rps        float64
	burst      int
	maxBuckets int
	callCount  uint64
}

// bucket represents a single token bucket for one API key.
type bucket struct {
	tokens    float64
	lastCheck time.Time
}

// NewLimiter creates a new rate limiter with the given refill rate (rps)
// and maximum burst capacity.
func NewLimiter(rps float64, burst int) *Limiter {
	return &Limiter{
		buckets:    make(map[string]*bucket),
		rps:        rps,
		burst:      burst,
		maxBuckets: defaultMaxBuckets,
	}
}

// Allow checks whether a single request from the given key should be allowed.
// It returns true if the request is within the rate limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Run stale-bucket cleanup periodically instead of every call.
	l.callCount++
	if l.callCount%cleanupInterval == 0 {
		l.cleanupStaleBucketsLocked()
	}

	b, ok := l.buckets[key]
	if !ok {
		// Enforce max bucket count to prevent memory exhaustion from random keys.
		if len(l.buckets) >= l.maxBuckets {
			l.cleanupStaleBucketsLocked()
			if len(l.buckets) >= l.maxBuckets {
				// Still at capacity after cleanup — reject new keys.
				return false
			}
		}
		b = &bucket{tokens: float64(l.burst)}
		l.buckets[key] = b
	}

	now := time.Now()
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.lastCheck = now

	// Refill tokens based on elapsed time.
	b.tokens += elapsed * l.rps
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}

	if b.tokens < 1 {
		return false
	}

	b.tokens--
	return true
}

// cleanupStaleBucketsLocked removes buckets that have been inactive for longer
// than staleBucketTTL. Must be called with l.mu held.
func (l *Limiter) cleanupStaleBucketsLocked() {
	cutoff := time.Now().Add(-staleBucketTTL)
	for key, b := range l.buckets {
		if b.lastCheck.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// extractAPIKey extracts the API key from an Authorization header value.
// Expected format: "Bearer <token>".
func extractAPIKey(authHeader string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(authHeader, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
	}
	return ""
}
