// Package ratelimit provides an in-memory, per-key token bucket limiter.
// Limits are per process: with N instances the effective ceiling is N × limit, which is acceptable
// for stopping runaway agents and bounded by Cloud Run max-instances.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// idleTTL removes buckets of keys that have not been seen recently, bounding memory.
	idleTTL = 10 * time.Minute
	// sweepInterval is how often idle buckets are looked for, lazily on Allow.
	sweepInterval = time.Minute
)

// Limiter allows up to perMinute calls per key, refilling continuously.
type Limiter struct {
	perMinute int
	now       func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// New returns a limiter allowing perMinute calls per key (bursts up to perMinute).
func New(perMinute int) *Limiter {
	return &Limiter{
		perMinute: perMinute,
		now:       time.Now,
		buckets:   make(map[string]*bucket),
	}
}

// Allow reports whether a call for key may proceed; when it may not, it returns how long to wait.
// Rejected calls do not consume tokens.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.perMinute)), l.perMinute)}
		l.buckets[key] = b
	}

	b.lastSeen = now

	reservation := b.limiter.ReserveN(now, 1)
	if delay := reservation.DelayFrom(now); delay > 0 {
		reservation.CancelAt(now)

		return false, delay
	}

	return true, 0
}

func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}

	l.lastSweep = now

	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) > idleTTL {
			delete(l.buckets, key)
		}
	}
}

func (l *Limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.buckets)
}
