package ratelimit

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newTestLimiter(perMinute int) (*Limiter, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
	l := New(perMinute)
	l.now = clock.now

	return l, clock
}

func TestLimiterAllowsBurstUpToLimitThenRejects(t *testing.T) {
	t.Parallel()

	l, _ := newTestLimiter(60)

	for i := range 60 {
		if ok, _ := l.Allow("conn-a"); !ok {
			t.Fatalf("call %d rejected, want the first 60 calls allowed", i+1)
		}
	}

	ok, retryAfter := l.Allow("conn-a")
	if ok {
		t.Fatal("call 61 allowed, want rejected")
	}

	if retryAfter <= 0 || retryAfter > time.Second {
		t.Errorf("retryAfter = %v, want (0, 1s] for 60/min", retryAfter)
	}
}

func TestLimiterIsPerKey(t *testing.T) {
	t.Parallel()

	l, _ := newTestLimiter(2)

	l.Allow("conn-a")
	l.Allow("conn-a")

	if ok, _ := l.Allow("conn-a"); ok {
		t.Fatal("conn-a third call allowed, want rejected")
	}

	if ok, _ := l.Allow("conn-b"); !ok {
		t.Error("conn-b rejected because of conn-a's usage")
	}
}

func TestLimiterRefillsOverTime(t *testing.T) {
	t.Parallel()

	l, clock := newTestLimiter(60)

	for range 60 {
		l.Allow("conn-a")
	}

	if ok, _ := l.Allow("conn-a"); ok {
		t.Fatal("allowed before refill")
	}

	clock.advance(time.Second)

	if ok, _ := l.Allow("conn-a"); !ok {
		t.Error("rejected after one refill interval")
	}

	clock.advance(time.Minute)

	for i := range 60 {
		if ok, _ := l.Allow("conn-a"); !ok {
			t.Fatalf("call %d rejected after a full minute idle", i+1)
		}
	}
}

func TestLimiterRejectedCallsDoNotConsumeTokens(t *testing.T) {
	t.Parallel()

	l, clock := newTestLimiter(60)

	for range 60 {
		l.Allow("conn-a")
	}

	// A client hammering while limited must not push its own recovery further away.
	for range 1000 {
		l.Allow("conn-a")
	}

	clock.advance(time.Second)

	if ok, _ := l.Allow("conn-a"); !ok {
		t.Error("rejected after refill: rejected calls consumed tokens")
	}
}

func TestLimiterEvictsIdleKeys(t *testing.T) {
	t.Parallel()

	l, clock := newTestLimiter(60)

	for i := range 100 {
		l.Allow(string(rune('a'+i%26)) + time.Duration(i).String())
	}

	if got := l.size(); got != 100 {
		t.Fatalf("size = %d, want 100", got)
	}

	clock.advance(idleTTL + sweepInterval + time.Second)
	l.Allow("fresh")

	if got := l.size(); got != 1 {
		t.Errorf("size after idle sweep = %d, want 1", got)
	}
}
