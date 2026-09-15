package business

import (
	"slices"
	"testing"
	"time"
)

// slotFixture builds inputs for 2026-09-16 (a Wednesday) in Guayaquil (UTC-5).
type slotFixture struct {
	t  *testing.T
	in slotInput
}

func newSlotFixture(t *testing.T) *slotFixture {
	t.Helper()

	loc := mustLoc(t, "America/Guayaquil")

	return &slotFixture{t: t, in: slotInput{
		loc:                loc,
		day:                time.Date(2026, 9, 16, 0, 0, 0, 0, loc),
		now:                time.Date(2026, 9, 15, 12, 0, 0, 0, loc),
		durationMinutes:    60,
		requiresSpecialist: true,
		branchHours:        dayHours{restricted: false},
	}}
}

func (f *slotFixture) at(hhmm string) time.Time {
	f.t.Helper()

	c, ok := parseClock(hhmm)
	if !ok {
		f.t.Fatalf("bad clock %q", hhmm)
	}

	return f.in.day.Add(c)
}

func (f *slotFixture) specialist(id, name string, shifts ...Shift) {
	f.in.specialists = append(f.in.specialists, SpecialistSchedule{ID: id, Name: name, Shifts: shifts})
}

func startsOf(slots []Slot) []string {
	out := make([]string, 0, len(slots))
	for _, s := range slots {
		out = append(out, s.Start.Format("15:04"))
	}

	return out
}

func TestComputeSlots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(f *slotFixture)
		want  []string
	}{
		{
			name:  "fits whole service inside the shift on a 30 minute grid",
			setup: func(f *slotFixture) { f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "12:00:00"}) },
			want:  []string{"09:00", "09:30", "10:00", "10:30", "11:00"},
		},
		{
			name: "break removes overlapping slots",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "12:00:00", BreakStart: new("10:00:00"), BreakEnd: new("10:30:00")})
			},
			want: []string{"09:00", "10:30", "11:00"},
		},
		{
			name: "branch operating hours restrict the shift",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "08:00:00", End: "18:00:00"})
				f.in.branchHours = dayHours{restricted: true, open: mustClock(t, "10:00"), close: mustClock(t, "12:00")}
			},
			want: []string{"10:00", "10:30", "11:00"},
		},
		{
			name: "branch closed that day",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "12:00:00"})
				f.in.branchHours = dayHours{restricted: true, closed: true}
			},
			want: []string{},
		},
		{
			name: "busy appointment blocks overlapping slots",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "12:00:00"})
				f.in.busy = []BusyInterval{{SpecialistID: "s1", Start: f.at("09:30"), End: f.at("10:30")}}
			},
			want: []string{"10:30", "11:00"},
		},
		{
			name: "another specialist's appointment does not block",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "10:00:00"})
				f.in.busy = []BusyInterval{{SpecialistID: "s2", Start: f.at("09:00"), End: f.at("10:00")}}
			},
			want: []string{"09:00"},
		},
		{
			name: "buffer must also fit before the end of the shift",
			setup: func(f *slotFixture) {
				f.in.durationMinutes, f.in.bufferMinutes = 30, 15
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "10:00:00"})
			},
			want: []string{"09:00"},
		},
		{
			name: "misaligned shift start snaps to the next grid point",
			setup: func(f *slotFixture) {
				f.in.durationMinutes = 30
				f.specialist("s1", "Ana", Shift{Start: "09:15:00", End: "10:30:00"})
			},
			want: []string{"09:30", "10:00"},
		},
		{
			name: "slots already started today are hidden",
			setup: func(f *slotFixture) {
				f.in.now = f.at("10:10")
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "12:00:00"})
			},
			want: []string{"10:30", "11:00"},
		},
		{
			name: "specialist day off",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "10:00:00"})
				f.in.exceptions = []ScheduleException{{SpecialistID: new("s1"), Type: "sick", DayOff: true}}
			},
			want: []string{},
		},
		{
			name: "vacation without hours blocks the whole day",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "10:00:00"})
				f.in.exceptions = []ScheduleException{{SpecialistID: new("s1"), Type: "vacation"}}
			},
			want: []string{},
		},
		{
			name: "partial absence only blocks its hours",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "12:00:00"})
				f.in.exceptions = []ScheduleException{{SpecialistID: new("s1"), Type: "vacation", Start: new("09:00:00"), End: new("10:00:00")}}
			},
			want: []string{"10:00", "10:30", "11:00"},
		},
		{
			name: "branch-wide holiday applies to every specialist",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "10:00:00"})
				f.specialist("s2", "Luis", Shift{Start: "09:00:00", End: "10:00:00"})
				f.in.exceptions = []ScheduleException{{Type: "holiday"}}
			},
			want: []string{},
		},
		{
			name: "specialist special hours replace the regular shift",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "12:00:00"})
				f.in.exceptions = []ScheduleException{{SpecialistID: new("s1"), Type: "special_hours", Start: new("14:00:00"), End: new("15:00:00")}}
			},
			want: []string{"14:00"},
		},
		{
			name: "branch special hours replace operating hours",
			setup: func(f *slotFixture) {
				f.specialist("s1", "Ana", Shift{Start: "08:00:00", End: "18:00:00"})
				f.in.branchHours = dayHours{restricted: true, closed: true}
				f.in.exceptions = []ScheduleException{{Type: "special_hours", Start: new("09:00:00"), End: new("10:00:00")}}
			},
			want: []string{"09:00"},
		},
		{
			name: "service without specialist uses branch hours only",
			setup: func(f *slotFixture) {
				f.in.requiresSpecialist = false
				f.in.branchHours = dayHours{restricted: true, open: mustClock(t, "09:00"), close: mustClock(t, "10:30")}
			},
			want: []string{"09:00", "09:30"},
		},
		{
			name:  "no specialists scheduled",
			setup: func(*slotFixture) {},
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newSlotFixture(t)
			tt.setup(f)

			got := computeSlots(f.in)
			if !slices.Equal(startsOf(got), tt.want) {
				t.Errorf("slots = %v, want %v", startsOf(got), tt.want)
			}

			for _, s := range got {
				if s.End.Sub(s.Start) != time.Duration(f.in.durationMinutes)*time.Minute {
					t.Errorf("slot %v ends at %v, want start + service duration", s.Start, s.End)
				}

				if s.Start.Location().String() != f.in.loc.String() {
					t.Errorf("slot start location = %s, want %s", s.Start.Location(), f.in.loc)
				}
			}
		})
	}
}

func TestComputeSlotsMergesSpecialistsPerStart(t *testing.T) {
	t.Parallel()

	f := newSlotFixture(t)
	f.specialist("s2", "Luis", Shift{Start: "09:00:00", End: "10:00:00"})
	f.specialist("s1", "Ana", Shift{Start: "09:00:00", End: "11:00:00"})

	got := computeSlots(f.in)
	if want := []string{"09:00", "09:30", "10:00"}; !slices.Equal(startsOf(got), want) {
		t.Fatalf("slots = %v, want %v", startsOf(got), want)
	}

	names := func(s Slot) []string {
		out := []string{}
		for _, r := range s.Specialists {
			out = append(out, r.Name)
		}

		return out
	}

	if n := names(got[0]); !slices.Equal(n, []string{"Ana", "Luis"}) {
		t.Errorf("09:00 specialists = %v, want [Ana Luis] sorted by name", n)
	}

	if n := names(got[2]); !slices.Equal(n, []string{"Ana"}) {
		t.Errorf("10:00 specialists = %v, want [Ana]", n)
	}
}

func TestBranchDayHours(t *testing.T) {
	t.Parallel()

	hours := []byte(`{"monday": {"open": "08:00", "close": "18:00"}, "sunday": null, "saturday": {"open": "09:00", "close": "14:00"}}`)

	tests := []struct {
		name    string
		raw     []byte
		weekday string
		want    dayHours
	}{
		{name: "open day", raw: hours, weekday: "monday", want: dayHours{restricted: true, open: mustClock(t, "08:00"), close: mustClock(t, "18:00")}},
		{name: "null day is closed", raw: hours, weekday: "sunday", want: dayHours{restricted: true, closed: true}},
		{name: "missing day is closed", raw: hours, weekday: "tuesday", want: dayHours{restricted: true, closed: true}},
		{name: "no hours configured", raw: nil, weekday: "monday", want: dayHours{}},
		{name: "empty object", raw: []byte(`{}`), weekday: "monday", want: dayHours{}},
		{name: "malformed hours are closed", raw: []byte(`{"monday": {"open": "late", "close": "18:00"}}`), weekday: "monday", want: dayHours{restricted: true, closed: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := branchDayHours(tt.raw, tt.weekday); got != tt.want {
				t.Errorf("branchDayHours() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func mustClock(t *testing.T, s string) time.Duration {
	t.Helper()

	c, ok := parseClock(s)
	if !ok {
		t.Fatalf("bad clock %q", s)
	}

	return c
}
