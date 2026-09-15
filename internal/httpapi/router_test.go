package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePinger struct {
	err error
	// sawDeadline reports whether Ping received a context with a deadline.
	sawDeadline bool
}

func (p *fakePinger) Ping(ctx context.Context) error {
	_, p.sawDeadline = ctx.Deadline()

	return p.err
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	router := NewRouter(slog.New(slog.DiscardHandler), Deps{DB: &fakePinger{}})

	tests := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{name: "get ok", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "post not allowed", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/healthz", nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestReadyz(t *testing.T) {
	t.Parallel()

	const dbErrText = "dial tcp 10.0.0.5:5432: connection refused"

	tests := []struct {
		name       string
		method     string
		pingErr    error
		wantStatus int
	}{
		{name: "database reachable", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "database unreachable", method: http.MethodGet, pingErr: errors.New(dbErrText), wantStatus: http.StatusServiceUnavailable},
		{name: "post not allowed", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pinger := &fakePinger{err: tt.pingErr}
			router := NewRouter(slog.New(slog.DiscardHandler), Deps{DB: pinger})

			req := httptest.NewRequestWithContext(t.Context(), tt.method, "/readyz", nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if strings.Contains(rec.Body.String(), dbErrText) {
				t.Errorf("body leaks dependency error: %q", rec.Body.String())
			}

			if tt.method == http.MethodGet && !pinger.sawDeadline {
				t.Error("Ping called without a deadline; a hung database would hang the probe")
			}
		})
	}
}
