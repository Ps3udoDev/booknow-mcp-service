package business

import (
	"strings"
	"time"
)

const (
	// defaultTimezone matches the tenants.timezone column default.
	defaultTimezone = "America/Guayaquil"
	maxRangeDays    = 31
	dateLayout      = "2006-01-02"
)

// location resolves an IANA timezone, falling back to the tenant default for empty or unknown names.
func location(name string) *time.Location {
	if name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}

	loc, err := time.LoadLocation(defaultTimezone)
	if err != nil {
		return time.UTC
	}

	return loc
}

// parseRange parses an inclusive [start, end] range into [from, to).
// Dates (YYYY-MM-DD) are whole days in loc; RFC 3339 datetimes are exact instants.
func parseRange(start, end string, loc *time.Location) (time.Time, time.Time, error) {
	from, fromIsDate, err := parseInstant(start, loc)
	if err != nil {
		return time.Time{}, time.Time{}, invalidArgument("startDate inválida: usa YYYY-MM-DD o fecha y hora ISO 8601.")
	}

	to, toIsDate, err := parseInstant(end, loc)
	if err != nil {
		return time.Time{}, time.Time{}, invalidArgument("endDate inválida: usa YYYY-MM-DD o fecha y hora ISO 8601.")
	}

	if to.Before(from) {
		return time.Time{}, time.Time{}, invalidArgument("La fecha inicial debe ser anterior o igual a la final.")
	}

	// Make the end exclusive: the next local day for dates, the next representable instant for datetimes.
	if toIsDate {
		to = to.AddDate(0, 0, 1)
	} else {
		to = to.Add(time.Microsecond)
	}

	limit := from.AddDate(0, 0, maxRangeDays)
	if !fromIsDate {
		limit = limit.Add(time.Microsecond)
	}

	if to.After(limit) {
		return time.Time{}, time.Time{}, invalidArgument("El rango no puede superar %d días por consulta.", maxRangeDays)
	}

	return from, to, nil
}

func parseInstant(s string, loc *time.Location) (time.Time, bool, error) {
	s = strings.TrimSpace(s)

	if t, err := time.ParseInLocation(dateLayout, s, loc); err == nil {
		return t, true, nil
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false, err
	}

	return t, false, nil
}

// startOfDay returns local midnight of t in loc.
func startOfDay(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()

	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
