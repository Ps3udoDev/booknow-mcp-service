package business

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/platform/pii"
)

const (
	snapshotWindowDays   = 30
	defaultPageLimit     = 20
	maxPageLimit         = 100
	maxPage              = 10000
	minSearchRunes       = 3
	maxSearchRunes       = 100
	minPhoneSearchDigits = 3
	maxSlotDaysAhead     = 90
)

var appointmentStatuses = map[string]bool{
	"pending": true, "confirmed": true, "in_progress": true, "completed": true, "cancelled": true, "no_show": true,
}

var weekdays = [...]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// Service implements the read use cases on top of a Store.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService returns a Service reading from store.
func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

// Snapshot returns PII-free KPIs of the tenant. "Today" is the current date in the tenant timezone.
func (s *Service) Snapshot(ctx context.Context, tenantID string) (Snapshot, error) {
	profile, loc, err := s.profile(ctx, tenantID)
	if err != nil {
		return Snapshot{}, err
	}

	now := s.now()
	dayStart := startOfDay(now, loc)

	counts, err := s.store.SnapshotCounts(ctx, SnapshotQuery{
		TenantID: tenantID,
		Since:    now.AddDate(0, 0, -snapshotWindowDays),
		DayStart: dayStart,
		DayEnd:   dayStart.AddDate(0, 0, 1),
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot counts: %w", err)
	}

	byStatus := counts.ByStatus
	if byStatus == nil {
		byStatus = map[string]int{}
	}

	topServices := counts.TopServices
	if topServices == nil {
		topServices = []ServiceCount{}
	}

	return Snapshot{
		TenantName: profile.Name,
		TenantSlug: profile.Slug,
		Timezone:   loc.String(),
		Metrics: SnapshotMetrics{
			ActiveBranchesCount:    counts.ActiveBranches,
			ActiveSpecialistsCount: counts.ActiveSpecialists,
			ActiveServicesCount:    counts.ActiveServices,
			TodayAppointmentsCount: counts.TodayAppointments,
			AppointmentsByStatus:   byStatus,
		},
		TopServices: topServices,
		GeneratedAt: now.In(loc),
	}, nil
}

// ScheduleSummary counts non-cancelled appointments per local day and per specialist.
func (s *Service) ScheduleSummary(ctx context.Context, tenantID string, in ScheduleSummaryInput) (ScheduleSummary, error) {
	if err := optionalUUID("branchId", in.BranchID); err != nil {
		return ScheduleSummary{}, err
	}

	if err := optionalUUID("specialistId", in.SpecialistID); err != nil {
		return ScheduleSummary{}, err
	}

	_, loc, err := s.profile(ctx, tenantID)
	if err != nil {
		return ScheduleSummary{}, err
	}

	from, to, err := parseRange(in.StartDate, in.EndDate, loc)
	if err != nil {
		return ScheduleSummary{}, err
	}

	counts, err := s.store.ScheduleSummary(ctx, ScheduleQuery{
		TenantID:     tenantID,
		Timezone:     loc.String(),
		From:         from,
		To:           to,
		BranchID:     strings.TrimSpace(in.BranchID),
		SpecialistID: strings.TrimSpace(in.SpecialistID),
	})
	if err != nil {
		return ScheduleSummary{}, fmt.Errorf("schedule summary: %w", err)
	}

	total := 0
	for _, d := range counts.Days {
		total += d.Count
	}

	return ScheduleSummary{
		Period:                 Period{StartDate: in.StartDate, EndDate: in.EndDate, Timezone: loc.String()},
		TotalAppointments:      total,
		DailyAppointments:      nonNil(counts.Days),
		SpecialistAppointments: nonNil(counts.Specialists),
	}, nil
}

// ListAppointments returns a page of appointments with masked customer phones.
func (s *Service) ListAppointments(ctx context.Context, tenantID string, in ListAppointmentsInput) (AppointmentList, error) {
	status := strings.TrimSpace(in.Status)
	if status != "" && !appointmentStatuses[status] {
		return AppointmentList{}, invalidArgument("status inválido: usa pending, confirmed, in_progress, completed, cancelled o no_show.")
	}

	page, limit, err := pageAndLimit(in.Page, in.Limit)
	if err != nil {
		return AppointmentList{}, err
	}

	_, loc, err := s.profile(ctx, tenantID)
	if err != nil {
		return AppointmentList{}, err
	}

	from, to, err := parseRange(in.StartDate, in.EndDate, loc)
	if err != nil {
		return AppointmentList{}, err
	}

	rows, total, err := s.store.ListAppointments(ctx, AppointmentQuery{
		TenantID: tenantID, From: from, To: to, Status: status, Limit: limit, Offset: (page - 1) * limit,
	})
	if err != nil {
		return AppointmentList{}, fmt.Errorf("list appointments: %w", err)
	}

	items := make([]Appointment, 0, len(rows))
	for _, r := range rows {
		items = append(items, toAppointment(r, loc))
	}

	return AppointmentList{
		Timezone: loc.String(),
		Items:    items,
		Pagination: Pagination{
			Page: page, Limit: limit, Total: total,
			TotalPages: int(math.Ceil(float64(total) / float64(limit))),
		},
	}, nil
}

// SearchCustomers finds customers by name or phone and returns them with masked phones.
func (s *Service) SearchCustomers(ctx context.Context, tenantID string, in SearchCustomersInput) ([]Customer, error) {
	query := strings.TrimSpace(in.Query)

	if n := utf8.RuneCountInString(query); n < minSearchRunes || n > maxSearchRunes {
		return nil, invalidArgument("query debe tener entre %d y %d caracteres.", minSearchRunes, maxSearchRunes)
	}

	_, limit, err := pageAndLimit(1, in.Limit)
	if err != nil {
		return nil, err
	}

	digits := asciiDigits(query)
	if len(digits) < minPhoneSearchDigits {
		digits = ""
	}

	rows, err := s.store.SearchCustomers(ctx, CustomerQuery{
		TenantID: tenantID,
		Pattern:  "%" + escapeLike(query) + "%",
		Digits:   digits,
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("search customers: %w", err)
	}

	customers := make([]Customer, 0, len(rows))

	for _, r := range rows {
		phone := ""
		if r.Phone != nil {
			phone = *r.Phone
		}

		customers = append(customers, Customer{
			ID: r.ID, FirstName: r.FirstName, LastName: r.LastName,
			PhoneMasked: pii.MaskPhone(phone), CreatedAt: r.CreatedAt,
		})
	}

	return customers, nil
}

// AvailableSlots computes bookable times for a service at a branch on a local date.
func (s *Service) AvailableSlots(ctx context.Context, tenantID string, in AvailableSlotsInput) (AvailableSlots, error) {
	c, err := s.catalog(ctx, tenantID, in.ServiceID, in.BranchID)
	if err != nil {
		return AvailableSlots{}, err
	}

	day, err := s.slotDay(in.Date, c.loc)
	if err != nil {
		return AvailableSlots{}, err
	}

	slots, err := s.daySlots(ctx, tenantID, c, day, in.SpecialistID, 0)
	if err != nil {
		return AvailableSlots{}, err
	}

	var specialistID *string
	if in.SpecialistID != "" {
		specialistID = &in.SpecialistID
	}

	return AvailableSlots{
		Date:                day.Format(dateLayout),
		Timezone:            c.loc.String(),
		Service:             SlotServiceRef{ID: c.service.ID, Name: c.service.Name, DurationMinutes: c.service.DurationMinutes},
		Branch:              Ref{ID: c.branch.ID, Name: c.branch.Name},
		SpecialistID:        specialistID,
		SlotIntervalMinutes: int(slotStep / time.Minute),
		CapacityChecked:     c.service.RequiresSpecialist,
		Slots:               slots,
	}, nil
}

// CheckSlot reports whether a booking can start exactly at in.Start, with the same rules as AvailableSlots
// and the service duration extended by in.ExtraMinutes.
func (s *Service) CheckSlot(ctx context.Context, tenantID string, in SlotCheckInput) (SlotCheck, error) {
	c, err := s.catalog(ctx, tenantID, in.ServiceID, in.BranchID)
	if err != nil {
		return SlotCheck{}, err
	}

	duration := c.service.DurationMinutes + in.ExtraMinutes
	if duration <= 0 {
		return SlotCheck{}, invalidArgument("La duración del servicio con la variante debe ser positiva.")
	}

	if in.Start.Before(s.now()) {
		return SlotCheck{}, invalidArgument("La fecha y hora de la cita no puede estar en el pasado.")
	}

	start := in.Start.In(c.loc)

	day, err := s.slotDay(start.Format(dateLayout), c.loc)
	if err != nil {
		return SlotCheck{}, err
	}

	slots, err := s.daySlots(ctx, tenantID, c, day, in.SpecialistID, in.ExtraMinutes)
	if err != nil {
		return SlotCheck{}, err
	}

	check := SlotCheck{Service: c.service, Branch: c.branch, Timezone: c.loc.String(), DurationMinutes: duration}

	for _, slot := range slots {
		if slot.Start.Equal(start) {
			check.Available = true
			check.Specialists = slot.Specialists
		}
	}

	return check, nil
}

// slotCatalog is a validated, active service and branch of the tenant.
type slotCatalog struct {
	service SlotService
	branch  SlotBranch
	loc     *time.Location
}

func (s *Service) catalog(ctx context.Context, tenantID, serviceID, branchID string) (slotCatalog, error) {
	if err := requiredUUID("serviceId", serviceID); err != nil {
		return slotCatalog{}, err
	}

	if err := requiredUUID("branchId", branchID); err != nil {
		return slotCatalog{}, err
	}

	service, err := s.store.SlotService(ctx, tenantID, serviceID)
	if err := entityErr(err, service.Active, "El servicio no existe o no está activo en este negocio."); err != nil {
		return slotCatalog{}, err
	}

	branch, err := s.store.SlotBranch(ctx, tenantID, branchID)
	if err := entityErr(err, branch.Active, "La sucursal no existe o no está activa en este negocio."); err != nil {
		return slotCatalog{}, err
	}

	return slotCatalog{service: service, branch: branch, loc: location(branch.Timezone)}, nil
}

// daySlots computes the slots of a local day; extraMinutes extends the service duration.
func (s *Service) daySlots(ctx context.Context, tenantID string, c slotCatalog, day time.Time, specialistID string, extraMinutes int) ([]Slot, error) {
	if err := optionalUUID("specialistId", specialistID); err != nil {
		return nil, err
	}

	weekday := weekdays[day.Weekday()]

	availability, err := s.store.Availability(ctx, AvailabilityQuery{
		TenantID: tenantID, BranchID: c.branch.ID, SpecialistID: specialistID,
		Weekday: weekday, Date: day.Format(dateLayout), DayStart: day, DayEnd: day.AddDate(0, 0, 1),
	})
	if err != nil {
		return nil, fmt.Errorf("availability: %w", err)
	}

	if specialistID != "" && !availability.SpecialistFound {
		return nil, notFound("El especialista no existe o no está activo en este negocio.")
	}

	return computeSlots(slotInput{
		loc: c.loc, day: day, now: s.now(),
		durationMinutes: c.service.DurationMinutes + extraMinutes, bufferMinutes: c.service.BufferMinutes,
		requiresSpecialist: c.service.RequiresSpecialist,
		branchHours:        branchDayHours(c.branch.OperatingHours, weekday),
		specialists:        availability.Specialists,
		exceptions:         availability.Exceptions,
		busy:               availability.Busy,
	}), nil
}

func (s *Service) slotDay(date string, loc *time.Location) (time.Time, error) {
	day, err := time.ParseInLocation(dateLayout, strings.TrimSpace(date), loc)
	if err != nil {
		return time.Time{}, invalidArgument("date inválida: usa YYYY-MM-DD.")
	}

	today := startOfDay(s.now(), loc)

	if day.Before(today) {
		return time.Time{}, invalidArgument("date no puede ser anterior a hoy.")
	}

	if day.After(today.AddDate(0, 0, maxSlotDaysAhead)) {
		return time.Time{}, invalidArgument("date no puede estar a más de %d días.", maxSlotDaysAhead)
	}

	return day, nil
}

func (s *Service) profile(ctx context.Context, tenantID string) (TenantProfile, *time.Location, error) {
	profile, err := s.store.TenantProfile(ctx, tenantID)
	if err != nil {
		return TenantProfile{}, nil, fmt.Errorf("tenant profile: %w", err)
	}

	return profile, location(profile.Timezone), nil
}

func toAppointment(r AppointmentRow, loc *time.Location) Appointment {
	a := Appointment{
		ID: r.ID, ScheduledAt: r.ScheduledAt.In(loc), DurationMinutes: r.DurationMinutes, Status: r.Status,
		EstimatedPrice: r.EstimatedPrice, CurrencyCode: r.CurrencyCode, CustomerNotes: r.CustomerNotes, Source: r.Source,
	}

	if r.EndsAt != nil {
		a.EndsAt = new(r.EndsAt.In(loc))
	}

	if r.CustomerID != nil {
		phone := ""
		if r.CustomerPhone != nil {
			phone = *r.CustomerPhone
		}

		a.Customer = &AppointmentCustomer{
			ID: *r.CustomerID, FirstName: deref(r.CustomerFirstName), LastName: deref(r.CustomerLastName),
			PhoneMasked: pii.MaskPhone(phone),
		}
	}

	a.Specialist = ref(r.SpecialistID, r.SpecialistName)
	a.Service = ref(r.ServiceID, r.ServiceName)
	a.Branch = ref(r.BranchID, r.BranchName)

	return a
}

func pageAndLimit(page, limit int) (int, int, error) {
	if page < 0 || page > maxPage {
		return 0, 0, invalidArgument("page debe estar entre 1 y %d.", maxPage)
	}

	if limit < 0 || limit > maxPageLimit {
		return 0, 0, invalidArgument("limit debe estar entre 1 y %d.", maxPageLimit)
	}

	if page == 0 {
		page = 1
	}

	if limit == 0 {
		limit = defaultPageLimit
	}

	return page, limit, nil
}

func entityErr(err error, active bool, message string) error {
	if errors.Is(err, ErrNotFound) || (err == nil && !active) {
		return notFound(message)
	}

	return err
}

func requiredUUID(field, value string) error {
	if !IsUUID(strings.TrimSpace(value)) {
		return invalidArgument("%s debe ser un UUID válido.", field)
	}

	return nil
}

func optionalUUID(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return requiredUUID(field, value)
}

// IsUUID reports whether s is a canonical hyphenated UUID.
func IsUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	for i := range len(s) {
		c := s[i]

		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHex(c) {
				return false
			}
		}
	}

	return true
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// escapeLike escapes ILIKE wildcards so user input matches literally (escape character: backslash).
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func asciiDigits(s string) string {
	var b strings.Builder

	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}

	return b.String()
}

func ref(id, name *string) *Ref {
	if id == nil {
		return nil
	}

	return &Ref{ID: *id, Name: deref(name)}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}

	return s
}
