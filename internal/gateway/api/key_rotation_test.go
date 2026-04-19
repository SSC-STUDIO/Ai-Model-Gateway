package api

import (
	"testing"
	"time"

	"ai-model-gateway/internal/gateway/snapshot"
)

func TestNewKeyRotator(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
			{Name: "key-b", Value: "value-b"},
		},
	}

	kr := NewKeyRotator(p)
	if kr == nil {
		t.Fatal("expected non-nil KeyRotator")
	}
	if !kr.IsEnabled() {
		t.Fatal("expected IsEnabled to be true")
	}
}

func TestNewKeyRotatorNilProvider(t *testing.T) {
	kr := NewKeyRotator(nil)
	if kr == nil {
		t.Fatal("expected non-nil KeyRotator")
	}
	if kr.IsEnabled() {
		t.Fatal("expected IsEnabled to be false for nil provider")
	}
}

func TestKeyRotatorNext(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
			{Name: "key-b", Value: "value-b"},
			{Name: "key-c", Value: "value-c"},
		},
	}

	kr := NewKeyRotator(p)

	// Should cycle through keys in round-robin order.
	keys := make(map[string]int)
	for i := 0; i < 6; i++ {
		k := kr.Next()
		if k == "" {
			t.Fatalf("expected a key on iteration %d, got empty", i)
		}
		keys[k]++
	}

	if keys["value-a"] != 2 || keys["value-b"] != 2 || keys["value-c"] != 2 {
		t.Fatalf("expected equal distribution, got %v", keys)
	}
}

func TestKeyRotatorNextSkipsDisabled(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a", Disabled: true},
			{Name: "key-b", Value: "value-b"},
		},
	}

	kr := NewKeyRotator(p)

	for i := 0; i < 4; i++ {
		k := kr.Next()
		if k != "value-b" {
			t.Fatalf("expected only value-b, got %q on iteration %d", k, i)
		}
	}
}

func TestKeyRotatorNextSkipsFailedKeys(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a", FailCount: 3},
			{Name: "key-b", Value: "value-b"},
		},
	}

	kr := NewKeyRotator(p)

	for i := 0; i < 4; i++ {
		k := kr.Next()
		if k != "value-b" {
			t.Fatalf("expected only value-b, got %q on iteration %d", k, i)
		}
	}
}

func TestKeyRotatorNextReturnsEmptyWhenAllFailed(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a", FailCount: 3},
			{Name: "key-b", Value: "value-b", FailCount: 5},
		},
	}

	kr := NewKeyRotator(p)

	k := kr.Next()
	if k != "" {
		t.Fatalf("expected empty string when all keys failed, got %q", k)
	}
}

func TestKeyRotatorNextReturnsEmptyWhenAllDisabled(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a", Disabled: true},
			{Name: "key-b", Value: "value-b", Disabled: true},
		},
	}

	kr := NewKeyRotator(p)

	k := kr.Next()
	if k != "" {
		t.Fatalf("expected empty string when all keys disabled, got %q", k)
	}
}

func TestKeyRotatorNextReturnsEmptyNoKeys(t *testing.T) {
	kr := NewKeyRotator(&snapshot.ProviderSnapshot{ProviderID: "empty"})

	k := kr.Next()
	if k != "" {
		t.Fatalf("expected empty string when no keys, got %q", k)
	}
}

func TestKeyRotatorReportFailure(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
			{Name: "key-b", Value: "value-b"},
		},
	}

	kr := NewKeyRotator(p)

	// Fail key-a twice.
	kr.ReportFailure("value-a")
	kr.ReportFailure("value-a")

	// Should still return key-a since failCount < maxFailCount.
	k := kr.Next()
	if k == "" {
		t.Fatal("expected a key, got empty")
	}

	// Third failure should mark key-a as failed.
	kr.ReportFailure("value-a")

	// Now only key-b should be returned.
	for i := 0; i < 4; i++ {
		k = kr.Next()
		if k != "value-b" {
			t.Fatalf("expected only value-b after key-a failed 3 times, got %q on iteration %d", k, i)
		}
	}
}

func TestKeyRotatorReportSuccess(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a", FailCount: 2},
			{Name: "key-b", Value: "value-b"},
		},
	}

	kr := NewKeyRotator(p)

	// Report success for key-a should reset its fail count.
	kr.ReportSuccess("value-a")

	// key-a should now be selectable again.
	seen := make(map[string]bool)
	for i := 0; i < 4; i++ {
		k := kr.Next()
		seen[k] = true
	}

	if !seen["value-a"] {
		t.Fatal("expected value-a to be selectable after ReportSuccess")
	}
}

func TestKeyRotatorReportSuccessUnknownKey(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
		},
	}

	kr := NewKeyRotator(p)

	// Should not panic on unknown key.
	kr.ReportSuccess("unknown-key")
}

func TestKeyRotatorReportFailureUnknownKey(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
		},
	}

	kr := NewKeyRotator(p)

	// Should not panic on unknown key.
	kr.ReportFailure("unknown-key")
}

func TestKeyRotatorNilMethods(t *testing.T) {
	var kr *KeyRotator

	if kr.Next() != "" {
		t.Fatal("expected empty string from nil KeyRotator.Next")
	}
	if kr.IsEnabled() {
		t.Fatal("expected IsEnabled false from nil KeyRotator")
	}

	// Should not panic.
	kr.ReportSuccess("key")
	kr.ReportFailure("key")
	kr.TryRecover(time.Minute)
}

func TestKeyRotatorTryRecover(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
			{Name: "key-b", Value: "value-b"},
		},
	}

	kr := NewKeyRotator(p)

	// Fail key-a 3 times to mark it as failed.
	for i := 0; i < 3; i++ {
		kr.ReportFailure("value-a")
	}

	// key-a should now be skipped.
	for i := 0; i < 4; i++ {
		k := kr.Next()
		if k != "value-b" {
			t.Fatalf("expected only value-b before recovery, got %q", k)
		}
	}

	// TryRecover with 0 cooldown should not recover since lastFail is now.
	kr.TryRecover(0)
	k := kr.Next()
	if k != "value-b" {
		t.Fatalf("expected only value-b after TryRecover with 0 cooldown, got %q", k)
	}

	// Manipulate internal state to simulate passage of time.
	kr.mu.Lock()
	for i := range kr.keys {
		if kr.keys[i].value == "value-a" {
			kr.keys[i].lastFail = time.Now().Add(-2 * time.Minute)
		}
	}
	kr.mu.Unlock()

	// Now TryRecover with 1 minute cooldown should recover key-a.
	kr.TryRecover(time.Minute)

	seen := make(map[string]bool)
	for i := 0; i < 4; i++ {
		k = kr.Next()
		seen[k] = true
	}
	if !seen["value-a"] {
		t.Fatal("expected value-a to be recovered")
	}
}

func TestKeyRotatorTryRecoverNoEffectOnNonFailedKeys(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
			{Name: "key-b", Value: "value-b", FailCount: 2},
		},
	}

	kr := NewKeyRotator(p)

	// TryRecover should not affect key-b since failCount < maxFailCount.
	kr.TryRecover(time.Minute)

	seen := make(map[string]bool)
	for i := 0; i < 4; i++ {
		k := kr.Next()
		seen[k] = true
	}
	if !seen["value-b"] {
		t.Fatal("expected value-b to still be selectable")
	}
}

func TestKeyRotatorSingleKey(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
		},
	}

	kr := NewKeyRotator(p)

	for i := 0; i < 3; i++ {
		k := kr.Next()
		if k != "value-a" {
			t.Fatalf("expected value-a, got %q on iteration %d", k, i)
		}
	}
}

func TestKeyRotatorRoundRobinWithFailures(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a"},
			{Name: "key-b", Value: "value-b"},
			{Name: "key-c", Value: "value-c"},
		},
	}

	kr := NewKeyRotator(p)

	// First call should return value-a (index 0).
	if k := kr.Next(); k != "value-a" {
		t.Fatalf("expected value-a first, got %q", k)
	}

	// Fail value-b so it gets skipped.
	for i := 0; i < 3; i++ {
		kr.ReportFailure("value-b")
	}

	// Next calls should alternate between value-a and value-c.
	results := make([]string, 4)
	for i := 0; i < 4; i++ {
		results[i] = kr.Next()
	}

	// After value-a (index 0), next index is 1 (value-b, failed), skip to 2 (value-c).
	// Then index 0 (value-a), then skip 1 to 2 (value-c).
	expected := []string{"value-c", "value-c", "value-a", "value-c"}
	for i, exp := range expected {
		if results[i] != exp {
			t.Fatalf("iteration %d: expected %q, got %q", i, exp, results[i])
		}
	}
}

func TestKeyRotatorPreservesInitialFailCount(t *testing.T) {
	p := &snapshot.ProviderSnapshot{
		ProviderID: "test-provider",
		APIKeys: []snapshot.APIKey{
			{Name: "key-a", Value: "value-a", FailCount: 2},
			{Name: "key-b", Value: "value-b"},
		},
	}

	kr := NewKeyRotator(p)

	// key-a has FailCount=2, so it should still be selectable.
	seen := make(map[string]bool)
	for i := 0; i < 4; i++ {
		k := kr.Next()
		seen[k] = true
	}
	if !seen["value-a"] {
		t.Fatal("expected value-a to be selectable with initial failCount=2")
	}

	// One more failure should disable key-a.
	kr.ReportFailure("value-a")
	for i := 0; i < 4; i++ {
		k := kr.Next()
		if k != "value-b" {
			t.Fatalf("expected only value-b after key-a failed, got %q", k)
		}
	}
}
