package business

import (
	"errors"
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}

	return loc
}

func TestParseRange(t *testing.T) {
	t.Parallel()

	gye := mustLoc(t, "America/Guayaquil") // UTC-5, no DST

	tests := []struct {
		name       string
		start, end string
		wantFrom   time.Time
		wantTo     time.Time
		wantErr    bool
	}{
		{
			name: "dates are whole local days, end inclusive", start: "2026-09-01", end: "2026-09-01",
			wantFrom: time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC), wantTo: time.Date(2026, 9, 2, 5, 0, 0, 0, time.UTC),
		},
		{
			name: "31 days is the maximum", start: "2026-09-01", end: "2026-10-01",
			wantFrom: time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC), wantTo: time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC),
		},
		{
			name: "datetimes are exact instants, end inclusive", start: "2026-09-01T10:00:00-05:00", end: "2026-09-01T18:00:00Z",
			wantFrom: time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC), wantTo: time.Date(2026, 9, 1, 18, 0, 0, 1000, time.UTC),
		},
		{name: "32 days rejected", start: "2026-09-01", end: "2026-10-02", wantErr: true},
		{name: "end before start", start: "2026-09-10", end: "2026-09-01", wantErr: true},
		{name: "invalid start", start: "01/09/2026", end: "2026-09-02", wantErr: true},
		{name: "impossible date", start: "2026-02-30", end: "2026-03-01", wantErr: true},
		{name: "empty end", start: "2026-09-01", end: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			from, to, err := parseRange(tt.start, tt.end, gye)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidArgument) {
					t.Fatalf("parseRange() error = %v, want ErrInvalidArgument", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseRange() error = %v", err)
			}

			if !from.Equal(tt.wantFrom) || !to.Equal(tt.wantTo) {
				t.Errorf("parseRange() = [%v, %v), want [%v, %v)", from.UTC(), to.UTC(), tt.wantFrom, tt.wantTo)
			}
		})
	}
}

func TestLocationFallback(t *testing.T) {
	t.Parallel()

	if got := location("America/Caracas").String(); got != "America/Caracas" {
		t.Errorf("location(America/Caracas) = %s", got)
	}

	for _, bad := range []string{"", "Mars/Olympus"} {
		if got := location(bad).String(); got != defaultTimezone {
			t.Errorf("location(%q) = %s, want fallback %s", bad, got, defaultTimezone)
		}
	}
}
