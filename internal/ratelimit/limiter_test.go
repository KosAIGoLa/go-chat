package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := New(2, time.Second)
	now := time.Unix(1, 0)
	if !l.Allow("u1", now) || !l.Allow("u1", now) {
		t.Fatalf("first two should pass")
	}
	if l.Allow("u1", now) {
		t.Fatalf("third should be limited")
	}
	if !l.Allow("u1", now.Add(time.Second)) {
		t.Fatalf("new window should pass")
	}
}

func TestLimiterDecisionMetadata(t *testing.T) {
	l := New(3, time.Second)
	now := time.Unix(10, 0)

	first := l.AllowN("tenant:1:user:2", 2, now)
	if !first.Allowed || first.Limit != 3 || first.Remaining != 1 || !first.ResetAt.Equal(now.Add(time.Second)) {
		t.Fatalf("unexpected first decision: %+v", first)
	}

	limited := l.AllowN("tenant:1:user:2", 2, now.Add(100*time.Millisecond))
	if limited.Allowed || limited.Remaining != 1 || limited.RetryAfter != 900*time.Millisecond {
		t.Fatalf("unexpected limited decision: %+v", limited)
	}
}

func TestLimiterCleanup(t *testing.T) {
	l := New(1, time.Second)
	now := time.Unix(20, 0)
	if !l.Allow("ip:127.0.0.1", now) {
		t.Fatalf("initial event should pass")
	}
	if removed := l.Cleanup(now.Add(time.Second)); removed != 1 {
		t.Fatalf("expected one expired bucket to be removed, got %d", removed)
	}
	if !l.Allow("ip:127.0.0.1", now.Add(time.Second)) {
		t.Fatalf("event should pass after cleanup")
	}
}

func TestLimiterRejectsInvalidConfig(t *testing.T) {
	if New(0, time.Second).Allow("u1", time.Unix(1, 0)) {
		t.Fatalf("zero limit should fail closed")
	}
	if New(1, 0).Allow("u1", time.Unix(1, 0)) {
		t.Fatalf("zero window should fail closed")
	}
}
