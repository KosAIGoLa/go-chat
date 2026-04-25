package ratelimit

import (
	"sync"
	"time"
)

// Decision describes the result of a rate-limit check.
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAt    time.Time
	RetryAfter time.Duration
}

// Limiter is an in-memory fixed-window limiter suitable for local tests and
// single-process deployments. Production deployments can keep the same call
// semantics while backing the counters with Redis keys such as
// ratelimit:user:{user_id}:{window} or ratelimit:ip:{ip}:{window}.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]bucket
}

type bucket struct {
	count   int
	resetAt time.Time
}

func New(limit int, window time.Duration) *Limiter {
	return &Limiter{limit: limit, window: window, buckets: make(map[string]bucket)}
}

func (l *Limiter) Allow(key string, now time.Time) bool {
	return l.AllowN(key, 1, now).Allowed
}

// AllowN consumes n events from key's current window. Non-positive limits or
// windows fail closed, which is safer for abusive traffic than accidentally
// permitting unlimited sends due to bad config.
func (l *Limiter) AllowN(key string, n int, now time.Time) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.limit <= 0 || l.window <= 0 || n <= 0 {
		return Decision{Allowed: false, Limit: l.limit}
	}

	b := l.buckets[key]
	if b.resetAt.IsZero() || !now.Before(b.resetAt) {
		b = bucket{resetAt: now.Add(l.window)}
	}

	decision := Decision{Limit: l.limit, ResetAt: b.resetAt}
	if b.count+n > l.limit {
		decision.Allowed = false
		decision.Remaining = max(l.limit-b.count, 0)
		decision.RetryAfter = maxDuration(b.resetAt.Sub(now), 0)
		l.buckets[key] = b
		return decision
	}

	b.count += n
	decision.Allowed = true
	decision.Remaining = l.limit - b.count
	l.buckets[key] = b
	return decision
}

// Cleanup removes expired buckets and returns the number of removed keys. It is
// optional, but long-running gateway/message-api processes should call it
// periodically to bound memory when limiting by user, device, tenant and IP.
func (l *Limiter) Cleanup(now time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()

	removed := 0
	for key, b := range l.buckets {
		if !b.resetAt.IsZero() && !now.Before(b.resetAt) {
			delete(l.buckets, key)
			removed++
		}
	}
	return removed
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
