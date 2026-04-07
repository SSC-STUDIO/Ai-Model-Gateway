package adminapi

import (
	"encoding/json"
	"math"
	"net/http"
	"sync"
	"time"
)

// RateLimitMiddleware returns a middleware that enforces a token-bucket rate
// limit. When the bucket is exhausted the middleware responds with 429 Too
// Many Requests and a JSON error body.
//
// Parameters:
//   - rps:   refill rate in requests per second
//   - burst: maximum tokens the bucket can hold (also the initial count)
func RateLimitMiddleware(rps float64, burst int) func(http.Handler) http.Handler {
	if rps <= 0 {
		rps = 10
	}
	if burst <= 0 {
		burst = 20
	}

	b := &tokenBucket{
		tokens:    float64(burst),
		maxTokens: float64(burst),
		rps:       rps,
		lastTime:  time.Now(),
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !b.allow() {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "rate limit exceeded",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// tokenBucket implements a simple token-bucket algorithm protected by a mutex.
type tokenBucket struct {
	mu        sync.Mutex
	tokens    float64
	maxTokens float64
	rps       float64
	lastTime  time.Time
}

// allow consumes one token and returns true, or returns false when the bucket
// is empty.
func (tb *tokenBucket) allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.tokens = math.Min(tb.maxTokens, tb.tokens+elapsed*tb.rps)
	tb.lastTime = now

	if tb.tokens < 1 {
		return false
	}
	tb.tokens--
	return true
}
