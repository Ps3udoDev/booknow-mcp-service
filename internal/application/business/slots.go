package business

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

const slotStep = 30 * time.Minute

// Exception types that block time (with hours: partially; without hours: the whole day).
var blockingExceptionTypes = map[string]bool{"vacation": true, "sick": true, "holiday": true}

const specialHoursType = "special_hours"

// dayHours are the branch operating hours for one weekday.
type dayHours struct {
	// restricted is false when the branch has no operating hours configured (no limit applies).
	restricted bool
	closed     bool
	open       time.Duration
	close      time.Duration
}

// interval is a half-open range of offsets from local midnight.
type interval struct {
	start, end time.Duration
}

type slotInput struct {
	loc                *time.Location
	day                time.Time // local midnight of the requested date
	now                time.Time
	durationMinutes    int
	bufferMinutes      int
	requiresSpecialist bool
	branchHours        dayHours
	specialists        []SpecialistSchedule
	exceptions         []ScheduleException
	busy               []BusyInterval
}

// computeSlots returns bookable start times on a 30-minute grid. A slot is offered when the service
// duration plus its buffer fits entirely inside working time that is not blocked by breaks, branch hours,
// absences or existing appointments, and it has not started yet.
func computeSlots(in slotInput) []Slot {
	need := time.Duration(in.durationMinutes+in.bufferMinutes) * time.Minute
	duration := time.Duration(in.durationMinutes) * time.Minute

	branch, branchBlocks, open := branchConstraints(in)
	if !open {
		return []Slot{}
	}

	bySlot := map[time.Duration][]Ref{}

	if !in.requiresSpecialist {
		windows := []interval{{0, 24 * time.Hour}}
		windows = applyBranch(windows, branch, branchBlocks)

		for _, start := range candidateStarts(in, windows, need) {
			bySlot[start] = []Ref{}
		}
	}

	for _, sp := range in.specialists {
		if !in.requiresSpecialist {
			break
		}

		windows, ok := specialistWindows(in, sp)
		if !ok {
			continue
		}

		windows = applyBranch(windows, branch, branchBlocks)

		for _, b := range in.busy {
			if b.SpecialistID == sp.ID {
				windows = subtract(windows, interval{b.Start.Sub(in.day), b.End.Sub(in.day)})
			}
		}

		for _, start := range candidateStarts(in, windows, need) {
			bySlot[start] = append(bySlot[start], Ref{ID: sp.ID, Name: sp.Name})
		}
	}

	starts := make([]time.Duration, 0, len(bySlot))
	for start := range bySlot {
		starts = append(starts, start)
	}

	slices.Sort(starts)

	slots := make([]Slot, 0, len(starts))
	for _, start := range starts {
		refs := bySlot[start]
		slices.SortFunc(refs, func(a, b Ref) int { return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID)) })

		begin := in.day.Add(start)
		slots = append(slots, Slot{Start: begin, End: begin.Add(duration), Specialists: refs})
	}

	return slots
}

// exceptionEffect summarizes a set of schedule exceptions.
type exceptionEffect struct {
	// special replaces the regular hours when set.
	special *interval
	dayOff  bool
	blocks  []interval
}

// effectOf applies exceptions in order: day off wins, special hours replace, timed absences block.
func effectOf(exceptions []ScheduleException, applies func(ScheduleException) bool) exceptionEffect {
	var effect exceptionEffect

	for _, ex := range exceptions {
		if !applies(ex) {
			continue
		}

		start, end, hasHours := exceptionHours(ex)

		switch {
		case ex.Type == specialHoursType && hasHours && !ex.DayOff:
			effect.special = &interval{start, end}
		case ex.DayOff || (blockingExceptionTypes[ex.Type] && !hasHours):
			effect.dayOff = true
		case blockingExceptionTypes[ex.Type]:
			effect.blocks = append(effect.blocks, interval{start, end})
		}
	}

	return effect
}

// branchConstraints resolves the branch hours (possibly replaced by branch-wide special hours) and
// branch-wide absences. open is false when the branch does not work that day.
func branchConstraints(in slotInput) (dayHours, []interval, bool) {
	effect := effectOf(in.exceptions, func(ex ScheduleException) bool { return ex.SpecialistID == nil })
	if effect.dayOff {
		return dayHours{}, nil, false
	}

	hours := in.branchHours
	if effect.special != nil {
		hours = dayHours{restricted: true, open: effect.special.start, close: effect.special.end}
	}

	if hours.restricted && hours.closed {
		return dayHours{}, nil, false
	}

	return hours, effect.blocks, true
}

// specialistWindows returns the specialist's working intervals after breaks and personal exceptions.
// ok is false when the specialist does not work that day.
func specialistWindows(in slotInput, sp SpecialistSchedule) ([]interval, bool) {
	effect := effectOf(in.exceptions, func(ex ScheduleException) bool {
		return ex.SpecialistID != nil && *ex.SpecialistID == sp.ID
	})
	if effect.dayOff {
		return nil, false
	}

	var windows []interval

	if effect.special != nil {
		windows = []interval{*effect.special}
	} else {
		for _, sh := range sp.Shifts {
			windows = append(windows, shiftWindows(sh)...)
		}
	}

	for _, b := range effect.blocks {
		windows = subtract(windows, b)
	}

	return windows, len(windows) > 0
}

// shiftWindows returns a shift minus its break; malformed shifts yield nothing.
func shiftWindows(sh Shift) []interval {
	start, okStart := parseClock(sh.Start)
	end, okEnd := parseClock(sh.End)

	if !okStart || !okEnd || end <= start {
		return nil
	}

	windows := []interval{{start, end}}

	if sh.BreakStart == nil || sh.BreakEnd == nil {
		return windows
	}

	breakStart, okBS := parseClock(*sh.BreakStart)
	breakEnd, okBE := parseClock(*sh.BreakEnd)

	if okBS && okBE && breakEnd > breakStart {
		windows = subtract(windows, interval{breakStart, breakEnd})
	}

	return windows
}

func applyBranch(windows []interval, hours dayHours, blocks []interval) []interval {
	if hours.restricted {
		windows = intersect(windows, interval{hours.open, hours.close})
	}

	for _, b := range blocks {
		windows = subtract(windows, b)
	}

	return windows
}

func candidateStarts(in slotInput, windows []interval, need time.Duration) []time.Duration {
	var starts []time.Duration

	for _, w := range windows {
		t := w.start.Truncate(slotStep)
		if t < w.start {
			t += slotStep
		}

		for ; t+need <= w.end; t += slotStep {
			if !in.day.Add(t).Before(in.now) {
				starts = append(starts, t)
			}
		}
	}

	return starts
}

func exceptionHours(ex ScheduleException) (time.Duration, time.Duration, bool) {
	if ex.Start == nil || ex.End == nil {
		return 0, 0, false
	}

	start, okStart := parseClock(*ex.Start)
	end, okEnd := parseClock(*ex.End)

	if !okStart || !okEnd || end <= start {
		return 0, 0, false
	}

	return start, end, true
}

// subtract removes cut from every interval.
func subtract(windows []interval, cut interval) []interval {
	out := make([]interval, 0, len(windows))

	for _, w := range windows {
		if cut.end <= w.start || cut.start >= w.end {
			out = append(out, w)

			continue
		}

		if cut.start > w.start {
			out = append(out, interval{w.start, cut.start})
		}

		if cut.end < w.end {
			out = append(out, interval{cut.end, w.end})
		}
	}

	return out
}

// intersect clips every interval to limit.
func intersect(windows []interval, limit interval) []interval {
	out := make([]interval, 0, len(windows))

	for _, w := range windows {
		start, end := max(w.start, limit.start), min(w.end, limit.end)
		if end > start {
			out = append(out, interval{start, end})
		}
	}

	return out
}

// parseClock parses "15:04" or "15:04:05" (as returned by Postgres time::text) into an offset from midnight.
func parseClock(s string) (time.Duration, bool) {
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second, true
		}
	}

	if s == "24:00" || s == "24:00:00" {
		return 24 * time.Hour, true
	}

	return 0, false
}

// branchDayHours reads branches.operating_hours ({"monday": {"open": "08:00", "close": "18:00"}, "sunday": null}).
// No configuration means no restriction; a missing, null or malformed day means closed.
func branchDayHours(raw []byte, weekday string) dayHours {
	if len(raw) == 0 {
		return dayHours{}
	}

	var days map[string]*struct {
		Open  string `json:"open"`
		Close string `json:"close"`
	}

	if err := json.Unmarshal(raw, &days); err != nil {
		return dayHours{restricted: true, closed: true}
	}

	if len(days) == 0 {
		return dayHours{}
	}

	day, ok := days[weekday]
	if !ok || day == nil {
		return dayHours{restricted: true, closed: true}
	}

	open, okOpen := parseClock(day.Open)
	closeAt, okClose := parseClock(day.Close)

	if !okOpen || !okClose || closeAt <= open {
		return dayHours{restricted: true, closed: true}
	}

	return dayHours{restricted: true, open: open, close: closeAt}
}
