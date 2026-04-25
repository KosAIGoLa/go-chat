package ratelimit

import (
	"testing"
	"time"
)

var (
	t0       = time.Unix(1_000, 0)
	recovery = 10 * time.Second
)

func newTestBreaker(threshold, probeLimit int) *Breaker {
	return NewBreaker(BreakerConfig{
		FailureThreshold: threshold,
		RecoveryTimeout:  recovery,
		ProbeLimit:       probeLimit,
	})
}

// TestBreakerStartsClosed verifies a new breaker is in closed state and allows calls.
func TestBreakerStartsClosed(t *testing.T) {
	b := newTestBreaker(3, 1)
	if got := b.State(t0); got != "closed" {
		t.Fatalf("expected closed, got %q", got)
	}
	if !b.Allow(t0) {
		t.Fatalf("closed breaker should allow calls")
	}
}

// TestBreakerOpenAfterThreshold trips the breaker exactly at the threshold.
func TestBreakerOpenAfterThreshold(t *testing.T) {
	b := newTestBreaker(3, 1)

	b.RecordFailure(t0)
	b.RecordFailure(t0)
	if got := b.State(t0); got != "closed" {
		t.Fatalf("expected closed after 2 failures (threshold=3), got %q", got)
	}

	b.RecordFailure(t0) // hits threshold
	if got := b.State(t0); got != "open" {
		t.Fatalf("expected open after 3 failures, got %q", got)
	}
	if b.Allow(t0) {
		t.Fatalf("open breaker should block calls")
	}
}

// TestBreakerHalfOpenAfterTimeout verifies the Open→HalfOpen transition.
func TestBreakerHalfOpenAfterTimeout(t *testing.T) {
	b := newTestBreaker(1, 1)
	b.RecordFailure(t0) // trips open

	afterTimeout := t0.Add(recovery)
	if got := b.State(afterTimeout); got != "half-open" {
		t.Fatalf("expected half-open after recovery timeout, got %q", got)
	}
	if !b.Allow(afterTimeout) {
		t.Fatalf("first probe in half-open should be allowed")
	}
}

// TestBreakerClosesAfterSuccessfulProbe verifies HalfOpen→Closed transition.
func TestBreakerClosesAfterSuccessfulProbe(t *testing.T) {
	b := newTestBreaker(1, 1)
	b.RecordFailure(t0) // trips open

	afterTimeout := t0.Add(recovery)
	b.Allow(afterTimeout) // enters half-open, uses probe slot
	b.RecordSuccess()     // probe succeeded → should close

	if got := b.State(afterTimeout); got != "closed" {
		t.Fatalf("expected closed after successful probe, got %q", got)
	}
}

// TestBreakerReopensOnFailureInHalfOpen verifies HalfOpen→Open on failure.
func TestBreakerReopensOnFailureInHalfOpen(t *testing.T) {
	b := newTestBreaker(1, 1)
	b.RecordFailure(t0) // trips open

	afterTimeout := t0.Add(recovery)
	b.Allow(afterTimeout)         // enters half-open
	b.RecordFailure(afterTimeout) // probe failed → back to open

	// Still within original window from afterTimeout: should be open.
	if got := b.State(afterTimeout); got != "open" {
		t.Fatalf("expected open after failure in half-open, got %q", got)
	}
}

// TestBreakerRecordSuccessResetsClosed verifies that RecordSuccess resets the
// failure counter in Closed state, so the count restarts from zero.
func TestBreakerRecordSuccessResetsClosed(t *testing.T) {
	b := newTestBreaker(3, 1)

	b.RecordFailure(t0)
	b.RecordFailure(t0) // 2 failures
	b.RecordSuccess()   // reset to 0

	// Two more failures: still below threshold (0+2 < 3).
	b.RecordFailure(t0)
	b.RecordFailure(t0)
	if got := b.State(t0); got != "closed" {
		t.Fatalf("expected closed (failures reset), got %q", got)
	}
}

// TestBreakerAllowBlocksExcessProbes verifies probeLimit enforcement in HalfOpen.
func TestBreakerAllowBlocksExcessProbes(t *testing.T) {
	b := newTestBreaker(1, 1)
	b.RecordFailure(t0) // trips open

	afterTimeout := t0.Add(recovery)
	first := b.Allow(afterTimeout)  // enters half-open, uses the one probe slot
	second := b.Allow(afterTimeout) // should be blocked

	if !first {
		t.Fatalf("first probe in half-open should be allowed")
	}
	if second {
		t.Fatalf("second probe in half-open (probeLimit=1) should be blocked")
	}
}

// TestBreakerDefaultConfig verifies zero-value config applies safe defaults:
// FailureThreshold=5, RecoveryTimeout=10s, ProbeLimit=1.
func TestBreakerDefaultConfig(t *testing.T) {
	b := NewBreaker(BreakerConfig{}) // all zeros → defaults

	base := time.Unix(0, 0)
	for i := 0; i < 5; i++ {
		b.RecordFailure(base)
	}
	if got := b.State(base); got != "open" {
		t.Fatalf("expected open after 5 failures (default threshold=5), got %q", got)
	}

	afterDefault := base.Add(10 * time.Second)
	if got := b.State(afterDefault); got != "half-open" {
		t.Fatalf("expected half-open after 10s (default recovery), got %q", got)
	}
}
