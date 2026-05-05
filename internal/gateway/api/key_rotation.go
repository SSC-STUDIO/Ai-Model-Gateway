// Package api provides HTTP handlers for gatewayd.
package api

import (
	"sync"
	"sync/atomic"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
)

// KeyRotator manages API key rotation for a provider.
type KeyRotator struct {
	providerID string
	keys       []rotatableKey
	index      atomic.Int64
	mu         sync.RWMutex
}

type rotatableKey struct {
	name      string
	value     string
	disabled  bool
	failCount int
	lastFail  time.Time
}

const maxFailCount = 3

// NewKeyRotator creates a key rotator from provider snapshot.
func NewKeyRotator(p *snapshot.ProviderSnapshot) *KeyRotator {
	if p == nil {
		return &KeyRotator{}
	}

	kr := &KeyRotator{
		providerID: p.ProviderID,
	}

	for _, ak := range p.APIKeys {
		kr.keys = append(kr.keys, rotatableKey{
			name:      ak.Name,
			value:     ak.Value,
			disabled:  ak.Disabled,
			failCount: ak.FailCount,
		})
	}

	return kr
}

// Next returns the next available API key value.
// Returns empty string if no keys available.
func (kr *KeyRotator) Next() string {
	if kr == nil || len(kr.keys) == 0 {
		return ""
	}

	kr.mu.RLock()
	defer kr.mu.RUnlock()

	n := len(kr.keys)
	if n == 0 {
		return ""
	}

	// Try each key starting from the current index.
	start := int(kr.index.Add(1)-1) % n
	if start < 0 {
		start = 0
	}

	for i := 0; i < n; i++ {
		idx := (start + i) % n
		k := &kr.keys[idx]
		if k.disabled {
			continue
		}
		if k.failCount >= maxFailCount {
			continue
		}
		return k.value
	}

	return ""
}

// ReportSuccess resets the failure count for the current key.
func (kr *KeyRotator) ReportSuccess(keyValue string) {
	if kr == nil || keyValue == "" {
		return
	}

	kr.mu.Lock()
	defer kr.mu.Unlock()

	for i := range kr.keys {
		if kr.keys[i].value == keyValue {
			kr.keys[i].failCount = 0
			kr.keys[i].lastFail = time.Time{}
			return
		}
	}
}

// ReportFailure increments the failure count for the given key.
func (kr *KeyRotator) ReportFailure(keyValue string) {
	if kr == nil || keyValue == "" {
		return
	}

	kr.mu.Lock()
	defer kr.mu.Unlock()

	for i := range kr.keys {
		if kr.keys[i].value == keyValue {
			kr.keys[i].failCount++
			kr.keys[i].lastFail = time.Now()
			return
		}
	}
}

// IsEnabled returns true if key rotation is active for this provider.
func (kr *KeyRotator) IsEnabled() bool {
	if kr == nil {
		return false
	}
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return len(kr.keys) > 0
}

// TryRecover attempts to recover keys that have been marked as failed.
// It resets the fail count for keys whose last failure is older than the given cooldown.
// Returns true if at least one key was reset.
func (kr *KeyRotator) TryRecover(cooldown time.Duration) bool {
	if kr == nil {
		return false
	}
	if len(kr.keys) == 0 {
		return false
	}

	kr.mu.Lock()
	defer kr.mu.Unlock()

	var recovered bool
	now := time.Now()
	for i := range kr.keys {
		if kr.keys[i].failCount >= maxFailCount && !kr.keys[i].lastFail.IsZero() {
			if cooldown > 0 && now.Sub(kr.keys[i].lastFail) >= cooldown {
				kr.keys[i].failCount = 0
				recovered = true
			}
		}
	}
	return recovered
}
