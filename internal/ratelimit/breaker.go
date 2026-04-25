package ratelimit

import (
	"sync"
	"time"
)

// state represents the circuit breaker's current state.
type state int

const (
	stateClosed   state = iota // normal operation; failures are counted
	stateOpen                  // downstream is failing; calls are blocked
	stateHalfOpen              // recovery probe; limited calls are let through
)

// BreakerConfig controls the circuit breaker's behaviour.
// Zero values are replaced by safe defaults inside NewBreaker.
type BreakerConfig struct {
	// FailureThreshold is the number of consecutive failures required to trip
	// the breaker into Open. Must be >= 1; defaults to 5.
	FailureThreshold int

	// RecoveryTimeout is how long the breaker stays Open before transitioning
	// to HalfOpen to probe the downstream. Must be > 0; defaults to 10s.
	RecoveryTimeout time.Duration

	// ProbeLimit is the number of calls allowed in HalfOpen before a decision
	// is made. Must be >= 1; defaults to 1.
	ProbeLimit int
}

// Breaker is a three-state circuit breaker (Closed → Open → HalfOpen) that
// tracks consecutive failures and temporarily blocks calls to a failing
// downstream until it recovers.
type Breaker struct {
	mu        sync.Mutex
	cfg       BreakerConfig
	state     state
	failures  int
	successes int
	openedAt  time.Time
}

// NewBreaker creates a Breaker with the given configuration.
// Zero-value config fields are replaced by safe defaults.
func NewBreaker(cfg BreakerConfig) *Breaker {
	if cfg.FailureThreshold < 1 {
		cfg.FailureThreshold = 5
	}
	if cfg.RecoveryTimeout <= 0 {
		cfg.RecoveryTimeout = 10 * time.Second
	}
	if cfg.ProbeLimit < 1 {
		cfg.ProbeLimit = 1
	}
	return &Breaker{cfg: cfg}
}

// tryTransitionToHalfOpen checks whether the Open timeout has elapsed.
// If so it transitions to HalfOpen and resets the success counter.
// Must be called with b.mu held.
func (b *Breaker) tryTransitionToHalfOpen(now time.Time) bool {
	if now.Sub(b.openedAt) >= b.cfg.RecoveryTimeout {
		b.state = stateHalfOpen
		b.successes = 0
		return true
	}
	return false
}

// Allow reports whether the call should proceed given the current time.
//
//   - Closed: always returns true.
//   - Open: returns false until RecoveryTimeout elapses, then transitions to
//     HalfOpen and falls through to HalfOpen logic.
//   - HalfOpen: allows up to ProbeLimit calls (increments successes per call);
//     returns false once the limit is reached.
func (b *Breaker) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateClosed:
		return true

	case stateOpen:
		if !b.tryTransitionToHalfOpen(now) {
			return false
		}
		// Transitioned to HalfOpen — fall through.
		fallthrough

	case stateHalfOpen:
		if b.successes < b.cfg.ProbeLimit {
			b.successes++
			return true
		}
		return false
	}

	return false
}

// RecordSuccess records a successful call result.
//
//   - Closed: resets the consecutive-failure counter.
//   - HalfOpen: if the probe limit has been reached, transitions to Closed and
//     resets all counters.
//   - Open: no-op.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateClosed:
		b.failures = 0

	case stateHalfOpen:
		if b.successes >= b.cfg.ProbeLimit {
			b.state = stateClosed
			b.failures = 0
			b.successes = 0
		}

	case stateOpen:
		// no-op
	}
}

// RecordFailure records a failed call result at the given time.
//
//   - Closed: increments the failure counter; trips to Open when
//     FailureThreshold is reached.
//   - HalfOpen: immediately trips back to Open, resetting successes.
//   - Open: no-op.
func (b *Breaker) RecordFailure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case stateClosed:
		b.failures++
		if b.failures >= b.cfg.FailureThreshold {
			b.state = stateOpen
			b.openedAt = now
		}

	case stateHalfOpen:
		b.state = stateOpen
		b.openedAt = now
		b.successes = 0

	case stateOpen:
		// no-op
	}
}

// State returns a human-readable string for the current breaker state given
// the current time. If the breaker is Open but the RecoveryTimeout has elapsed,
// it transitions to HalfOpen before returning (same logic as Allow).
//
// Possible return values: "closed", "open", "half-open".
func (b *Breaker) State(now time.Time) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == stateOpen {
		b.tryTransitionToHalfOpen(now)
	}

	switch b.state {
	case stateClosed:
		return "closed"
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half-open"
	}
	return "closed"
}
