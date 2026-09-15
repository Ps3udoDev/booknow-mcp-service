package business

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	tenantA   = "85a89283-7a3c-4d4c-81cc-fc3d95c03786"
	serviceID = "11111111-1111-4111-8111-111111111111"
	branchID  = "22222222-2222-4222-8222-222222222222"
	specID    = "33333333-3333-4333-8333-333333333333"
)

// fakeStore records the queries it receives and returns canned data.
type fakeStore struct {
	profile TenantProfile

	snapshotQ SnapshotQuery
	snapshot  SnapshotCounts

	scheduleQ ScheduleQuery
	schedule  ScheduleCounts

	appointmentsQ AppointmentQuery
	appointments  []AppointmentRow
	total         int

	customersQ CustomerQuery
	customers  []CustomerRow

	service      SlotService
	serviceErr   error
	branch       SlotBranch
	branchErr    error
	availQ       AvailabilityQuery
	availability Availability

	tenantsSeen []string
}

func (f *fakeStore) seen(tenantID string) { f.tenantsSeen = append(f.tenantsSeen, tenantID) }

func (f *fakeStore) TenantProfile(_ context.Context, tenantID string) (TenantProfile, error) {
	f.seen(tenantID)

	return f.profile, nil
}

func (f *fakeStore) SnapshotCounts(_ context.Context, q SnapshotQuery) (SnapshotCounts, error) {
	f.seen(q.TenantID)
	f.snapshotQ = q

	return f.snapshot, nil
}

func (f *fakeStore) ScheduleSummary(_ context.Context, q ScheduleQuery) (ScheduleCounts, error) {
	f.seen(q.TenantID)
	f.scheduleQ = q

	return f.schedule, nil
}

func (f *fakeStore) ListAppointments(_ context.Context, q AppointmentQuery) ([]AppointmentRow, int, error) {
	f.seen(q.TenantID)
	f.appointmentsQ = q

	return f.appointments, f.total, nil
}

func (f *fakeStore) SearchCustomers(_ context.Context, q CustomerQuery) ([]CustomerRow, error) {
	f.seen(q.TenantID)
	f.customersQ = q

	return f.customers, nil
}

func (f *fakeStore) SlotService(_ context.Context, tenantID, _ string) (SlotService, error) {
	f.seen(tenantID)

	return f.service, f.serviceErr
}

func (f *fakeStore) SlotBranch(_ context.Context, tenantID, _ string) (SlotBranch, error) {
	f.seen(tenantID)

	return f.branch, f.branchErr
}

func (f *fakeStore) Availability(_ context.Context, q AvailabilityQuery) (Availability, error) {
	f.seen(q.TenantID)
	f.availQ = q

	return f.availability, nil
}

// fixedNow is Tuesday 2026-09-15 12:00 in Guayaquil.
var fixedNow = time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)

func newTestService(store *fakeStore) *Service {
	if store.profile == (TenantProfile{}) {
		store.profile = TenantProfile{Name: "Elvis Studio", Slug: "elvis-studio", Timezone: "America/Guayaquil"}
	}

	svc := NewService(store)
	svc.now = func() time.Time { return fixedNow }

	return svc
}

func requireKind(t *testing.T, err, kind error) {
	t.Helper()

	if !errors.Is(err, kind) {
		t.Fatalf("error = %v, want %v", err, kind)
	}

	var be *Error
	if !errors.As(err, &be) || be.Message == "" {
		t.Fatalf("error %v has no user-facing message", err)
	}
}

func TestSnapshotUsesTenantTimezone(t *testing.T) {
	t.Parallel()

	store := &fakeStore{snapshot: SnapshotCounts{
		ActiveBranches: 2, TodayAppointments: 3,
		ByStatus:    map[string]int{"confirmed": 4},
		TopServices: []ServiceCount{{Name: "Corte", AppointmentsCount: 4}},
	}}

	got, err := newTestService(store).Snapshot(t.Context(), tenantA)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	wantDayStart := time.Date(2026, 9, 15, 5, 0, 0, 0, time.UTC)
	if !store.snapshotQ.DayStart.Equal(wantDayStart) || !store.snapshotQ.DayEnd.Equal(wantDayStart.Add(24*time.Hour)) {
		t.Errorf("today = [%v, %v), want local day starting %v", store.snapshotQ.DayStart, store.snapshotQ.DayEnd, wantDayStart)
	}

	if !store.snapshotQ.Since.Equal(fixedNow.AddDate(0, 0, -30)) {
		t.Errorf("since = %v, want 30 days before now", store.snapshotQ.Since)
	}

	if got.TenantSlug != "elvis-studio" || got.Timezone != "America/Guayaquil" || got.Metrics.TodayAppointmentsCount != 3 ||
		got.Metrics.ActiveBranchesCount != 2 || len(got.TopServices) != 1 {
		t.Errorf("Snapshot() = %+v", got)
	}

	if !slices.Equal(store.tenantsSeen, []string{tenantA, tenantA}) {
		t.Errorf("tenants queried = %v, want only %s", store.tenantsSeen, tenantA)
	}
}

func TestScheduleSummary(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		store := &fakeStore{schedule: ScheduleCounts{
			Days:        []DayCount{{Date: "2026-09-01", Count: 2}, {Date: "2026-09-02", Count: 3}},
			Specialists: []SpecialistCount{{Name: "Sin asignar", Count: 5}},
		}}

		got, err := newTestService(store).ScheduleSummary(t.Context(), tenantA, ScheduleSummaryInput{
			StartDate: "2026-09-01", EndDate: "2026-09-02", BranchID: branchID,
		})
		if err != nil {
			t.Fatalf("ScheduleSummary() error = %v", err)
		}

		q := store.scheduleQ
		if q.TenantID != tenantA || q.Timezone != "America/Guayaquil" || q.BranchID != branchID || q.SpecialistID != "" ||
			!q.From.Equal(time.Date(2026, 9, 1, 5, 0, 0, 0, time.UTC)) || !q.To.Equal(time.Date(2026, 9, 3, 5, 0, 0, 0, time.UTC)) {
			t.Errorf("query = %+v", q)
		}

		if got.TotalAppointments != 5 || got.Period.Timezone != "America/Guayaquil" {
			t.Errorf("ScheduleSummary() = %+v", got)
		}
	})

	for name, in := range map[string]ScheduleSummaryInput{
		"invalid branch id":     {StartDate: "2026-09-01", EndDate: "2026-09-02", BranchID: "branch-1"},
		"invalid specialist id": {StartDate: "2026-09-01", EndDate: "2026-09-02", SpecialistID: "x' or '1'='1"},
		"range too long":        {StartDate: "2026-09-01", EndDate: "2026-12-01"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := newTestService(&fakeStore{}).ScheduleSummary(t.Context(), tenantA, in)
			requireKind(t, err, ErrInvalidArgument)
		})
	}
}

func TestListAppointments(t *testing.T) {
	t.Parallel()

	phone := "+593991234567"
	scheduled := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)

	store := &fakeStore{
		total: 45,
		appointments: []AppointmentRow{{
			ID: "a1", ScheduledAt: scheduled, DurationMinutes: 60, Status: "confirmed",
			CustomerID: new("c1"), CustomerFirstName: new("Ana"), CustomerLastName: new("Pérez"), CustomerPhone: &phone,
			SpecialistID: new("s1"), SpecialistName: new("Luis"),
		}},
	}

	got, err := newTestService(store).ListAppointments(t.Context(), tenantA, ListAppointmentsInput{
		StartDate: "2026-09-01", EndDate: "2026-09-30", Status: "confirmed", Page: 3,
	})
	if err != nil {
		t.Fatalf("ListAppointments() error = %v", err)
	}

	q := store.appointmentsQ
	if q.TenantID != tenantA || q.Status != "confirmed" || q.Limit != 20 || q.Offset != 40 {
		t.Errorf("query = %+v, want default limit 20 and offset 40 for page 3", q)
	}

	if got.Pagination != (Pagination{Page: 3, Limit: 20, Total: 45, TotalPages: 3}) {
		t.Errorf("pagination = %+v", got.Pagination)
	}

	item := got.Items[0]
	if item.Customer == nil || item.Customer.PhoneMasked == nil || *item.Customer.PhoneMasked != "+593******567" {
		t.Errorf("customer = %+v, want masked phone", item.Customer)
	}

	if item.ScheduledAt.Location().String() != "America/Guayaquil" || item.ScheduledAt.Hour() != 10 {
		t.Errorf("scheduled_at = %v, want local tenant time 10:00", item.ScheduledAt)
	}

	for name, in := range map[string]ListAppointmentsInput{
		"unknown status": {StartDate: "2026-09-01", EndDate: "2026-09-02", Status: "done; drop table"},
		"limit over 100": {StartDate: "2026-09-01", EndDate: "2026-09-02", Limit: 101},
		"negative page":  {StartDate: "2026-09-01", EndDate: "2026-09-02", Page: -1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := newTestService(&fakeStore{}).ListAppointments(t.Context(), tenantA, in)
			requireKind(t, err, ErrInvalidArgument)
		})
	}
}

func TestSearchCustomers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		wantPattern string
		wantDigits  string
	}{
		{name: "name", query: "  ana ", wantPattern: "%ana%", wantDigits: ""},
		{name: "like wildcards are escaped", query: `50%_off\`, wantPattern: `%50\%\_off\\%`, wantDigits: ""},
		{name: "phone digits", query: "+593 99-12", wantPattern: "%+593 99-12%", wantDigits: "5939912"},
		{name: "two digits do not trigger phone search", query: "ana 12", wantPattern: "%ana 12%", wantDigits: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			phone := "0991234567"
			store := &fakeStore{customers: []CustomerRow{{ID: "c1", FirstName: "Ana", LastName: "Pérez", Phone: &phone}}}

			got, err := newTestService(store).SearchCustomers(t.Context(), tenantA, SearchCustomersInput{Query: tt.query})
			if err != nil {
				t.Fatalf("SearchCustomers() error = %v", err)
			}

			q := store.customersQ
			if q.TenantID != tenantA || q.Pattern != tt.wantPattern || q.Digits != tt.wantDigits || q.Limit != 20 {
				t.Errorf("query = %+v, want pattern %q digits %q limit 20", q, tt.wantPattern, tt.wantDigits)
			}

			if got[0].PhoneMasked == nil || strings.Contains(*got[0].PhoneMasked, "0991") {
				t.Errorf("phone not masked: %v", got[0].PhoneMasked)
			}
		})
	}

	for name, in := range map[string]SearchCustomersInput{
		"too short":     {Query: " ab "},
		"too long":      {Query: strings.Repeat("a", 101)},
		"limit too big": {Query: "ana", Limit: 500},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := newTestService(&fakeStore{}).SearchCustomers(t.Context(), tenantA, in)
			requireKind(t, err, ErrInvalidArgument)
		})
	}
}

func slotStore() *fakeStore {
	return &fakeStore{
		service: SlotService{ID: serviceID, Name: "Corte", DurationMinutes: 60, RequiresSpecialist: true, Active: true},
		branch:  SlotBranch{ID: branchID, Name: "Centro", Timezone: "America/Guayaquil", Active: true},
		availability: Availability{
			SpecialistFound: true,
			Specialists:     []SpecialistSchedule{{ID: specID, Name: "Ana", Shifts: []Shift{{Start: "09:00:00", End: "11:00:00"}}}},
		},
	}
}

func TestAvailableSlots(t *testing.T) {
	t.Parallel()

	store := slotStore()

	got, err := newTestService(store).AvailableSlots(t.Context(), tenantA, AvailableSlotsInput{
		ServiceID: serviceID, BranchID: branchID, Date: "2026-09-16", SpecialistID: specID,
	})
	if err != nil {
		t.Fatalf("AvailableSlots() error = %v", err)
	}

	q := store.availQ
	if q.TenantID != tenantA || q.BranchID != branchID || q.SpecialistID != specID || q.Weekday != "wednesday" || q.Date != "2026-09-16" ||
		!q.DayStart.Equal(time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)) || !q.DayEnd.Equal(time.Date(2026, 9, 17, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("availability query = %+v", q)
	}

	if len(got.Slots) != 3 || got.Timezone != "America/Guayaquil" || !got.CapacityChecked || got.Service.DurationMinutes != 60 {
		t.Errorf("AvailableSlots() = %+v", got)
	}
}

func TestAvailableSlotsErrors(t *testing.T) {
	t.Parallel()

	valid := AvailableSlotsInput{ServiceID: serviceID, BranchID: branchID, Date: "2026-09-16"}

	tests := []struct {
		name   string
		mutate func(*AvailableSlotsInput, *fakeStore)
		want   error
	}{
		{name: "invalid service id", mutate: func(in *AvailableSlotsInput, _ *fakeStore) { in.ServiceID = "corte" }, want: ErrInvalidArgument},
		{name: "invalid date", mutate: func(in *AvailableSlotsInput, _ *fakeStore) { in.Date = "mañana" }, want: ErrInvalidArgument},
		{name: "date in the past", mutate: func(in *AvailableSlotsInput, _ *fakeStore) { in.Date = "2026-09-14" }, want: ErrInvalidArgument},
		{name: "date too far ahead", mutate: func(in *AvailableSlotsInput, _ *fakeStore) { in.Date = "2027-01-01" }, want: ErrInvalidArgument},
		{name: "service of another tenant", mutate: func(_ *AvailableSlotsInput, s *fakeStore) { s.serviceErr = ErrNotFound }, want: ErrNotFound},
		{name: "inactive service", mutate: func(_ *AvailableSlotsInput, s *fakeStore) { s.service.Active = false }, want: ErrNotFound},
		{name: "branch of another tenant", mutate: func(_ *AvailableSlotsInput, s *fakeStore) { s.branchErr = ErrNotFound }, want: ErrNotFound},
		{name: "inactive branch", mutate: func(_ *AvailableSlotsInput, s *fakeStore) { s.branch.Active = false }, want: ErrNotFound},
		{
			name: "unknown specialist",
			mutate: func(in *AvailableSlotsInput, s *fakeStore) {
				in.SpecialistID = specID
				s.availability.SpecialistFound = false
			},
			want: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in, store := valid, slotStore()
			tt.mutate(&in, store)

			_, err := newTestService(store).AvailableSlots(t.Context(), tenantA, in)
			requireKind(t, err, tt.want)
		})
	}
}
