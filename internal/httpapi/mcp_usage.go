package httpapi

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

const (
	// usageRecordInterval bounds last_used_at writes to one per connection per instance per interval.
	usageRecordInterval = time.Minute
	// usageRecordTimeout keeps a slow database from delaying MCP responses noticeably.
	usageRecordTimeout = time.Second
)

// RateLimiter decides whether a connection may make another request.
type RateLimiter interface {
	Allow(key string) (allowed bool, retryAfter time.Duration)
}

// UsageRecorder marks an MCP connection as used (mcp_connections.last_used_at).
type UsageRecorder interface {
	RecordConnectionUse(ctx context.Context, connectionID string) error
}

// usageThrottle remembers when each connection was last recorded to avoid a write per request.
type usageThrottle struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func newUsageThrottle() *usageThrottle {
	return &usageThrottle{last: make(map[string]time.Time), now: time.Now}
}

func (t *usageThrottle) due(connectionID string) bool {
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	if last, ok := t.last[connectionID]; ok && now.Sub(last) < usageRecordInterval {
		return false
	}

	// Drop stale entries so memory stays bounded by recently active connections.
	for id, seen := range t.last {
		if now.Sub(seen) >= usageRecordInterval {
			delete(t.last, id)
		}
	}

	t.last[connectionID] = now

	return true
}

// applyUsagePolicy enforces the per-connection rate limit and records connection use.
// It returns false when it already wrote a response.
func applyUsagePolicy(w http.ResponseWriter, r *http.Request, log *slog.Logger, cfg MCPConfig, throttle *usageThrottle, access tenant.Access) bool {
	ctx := r.Context()

	if cfg.RateLimit != nil {
		if allowed, retryAfter := cfg.RateLimit.Allow(access.ConnectionID); !allowed {
			seconds := max(1, int(math.Ceil(retryAfter.Seconds())))
			log.WarnContext(ctx, "mcp rate limited", slog.Int("retry_after_seconds", seconds))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			writeRPCError(w, http.StatusTooManyRequests, rpcCodeForbidden,
				"Demasiadas solicitudes para esta conexión MCP. Espera unos segundos antes de reintentar.", "RATE_LIMITED")

			return false
		}
	}

	if cfg.Usage != nil && throttle.due(access.ConnectionID) {
		usageCtx, cancel := context.WithTimeout(ctx, usageRecordTimeout)
		defer cancel()

		// Best effort: usage tracking must never block access.
		if err := cfg.Usage.RecordConnectionUse(usageCtx, access.ConnectionID); err != nil {
			log.WarnContext(ctx, "mcp connection usage not recorded", slog.Any("error", err))
		}
	}

	return true
}
