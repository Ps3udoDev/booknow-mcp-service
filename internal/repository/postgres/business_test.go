package postgres

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
)

// businessFixture holds IDs of two tenants seeded for store tests.
type businessFixture struct {
	tenantA, tenantB                     string
	branchA, inactiveBranchA, branchB    string
	serviceA, inactiveServiceA, serviceB string
	ana, luis, pedro, eva, zoe           string
	customerAna                          string
}

var gye = func() *time.Location {
	loc, err := time.LoadLocation("America/Guayaquil")
	if err != nil {
		panic(err)
	}

	return loc
}()

// local builds a Guayaquil wall-clock time.
func local(day, hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, gye)
	if err != nil {
		panic(err)
	}

	return t
}

func (s *seeder) staff(tenantID, name string, specialist, active bool) string {
	s.t.Helper()

	id := s.user()
	s.exec(`insert into public.profiles (id, tenant_id, full_name, email, is_specialist, is_active) values ($1, $2, $3, $4, $5, $6)`,
		id, tenantID, name, strings.ToLower(name)+"-"+id[:8]+"@example.test", specialist, active)

	return id
}

func (s *seeder) businessTenant(timezone string) string {
	s.t.Helper()

	id, _ := s.tenant("active")
	s.exec(`update public.tenants set timezone = $2, name = 'Negocio ' || slug where id = $1`, id, timezone)

	return id
}

func (s *seeder) branch(tenantID, name string, active bool) string {
	s.t.Helper()

	return s.scanID(`insert into public.branches (tenant_id, name, timezone, is_active) values ($1, $2, 'America/Guayaquil', $3) returning id::text`,
		tenantID, name, active)
}

func (s *seeder) service(tenantID, name string, active bool) string {
	s.t.Helper()

	return s.scanID(`insert into public.services (tenant_id, name, slug, duration_minutes, buffer_minutes, requires_specialist, is_active)
		values ($1, $2, $3, 60, 10, true, $4) returning id::text`,
		tenantID, name, strings.ToLower(name)+"-"+strings.ToLower(s.randomSuffix()), active)
}

func (s *seeder) randomSuffix() string {
	return strings.ToLower(s.user()[:8])
}

func (s *seeder) customer(tenantID, first, last, phone string) string {
	s.t.Helper()

	return s.scanID(`insert into public.customers (tenant_id, first_name, last_name, full_name, phone) values ($1, $2::text, $3::text, $2::text || ' ' || $3::text, nullif($4::text, '')) returning id::text`,
		tenantID, first, last, phone)
}

func (s *seeder) appointment(tenantID, branchID, customerID, serviceID, specialistID string, at time.Time, status string) string {
	s.t.Helper()

	return s.scanID(`insert into public.appointments (tenant_id, branch_id, customer_id, service_id, specialist_id, scheduled_at, duration_minutes, status, estimated_price)
		values ($1, $2, $3, $4, nullif($5, '')::uuid, $6, 60, $7::public.appointment_status, 15.50) returning id::text`,
		tenantID, branchID, customerID, serviceID, specialistID, at, status)
}

func (s *seeder) schedule(tenantID, specialistID, branchID, weekday, start, end, breakStart, breakEnd string, active bool) {
	s.t.Helper()

	s.exec(`insert into public.specialist_schedules (tenant_id, specialist_id, branch_id, day_of_week, start_time, end_time, break_start, break_end, is_active)
		values ($1, $2, $3, $4::public.day_of_week, $5::time, $6::time, nullif($7, '')::time, nullif($8, '')::time, $9)`,
		tenantID, specialistID, branchID, weekday, start, end, breakStart, breakEnd, active)
}

func seedBusiness(s *seeder) businessFixture {
	s.t.Helper()

	var f businessFixture

	f.tenantA = s.businessTenant("America/Guayaquil")
	f.tenantB = s.businessTenant("America/Caracas")

	f.branchA = s.branch(f.tenantA, "Centro", true)
	f.inactiveBranchA = s.branch(f.tenantA, "Cerrada", false)
	f.branchB = s.branch(f.tenantB, "Sucursal B", true)

	f.serviceA = s.service(f.tenantA, "Corte", true)
	f.inactiveServiceA = s.service(f.tenantA, "Tinte", false)
	f.serviceB = s.service(f.tenantB, "Manicure", true)

	f.ana = s.staff(f.tenantA, "Ana", true, true)
	f.luis = s.staff(f.tenantA, "Luis", true, true)
	f.pedro = s.staff(f.tenantA, "Pedro", true, false)
	f.eva = s.staff(f.tenantA, "Eva", false, true)
	f.zoe = s.staff(f.tenantB, "Zoe", true, true)

	f.customerAna = s.customer(f.tenantA, "Ana", "Pérez", "+593991234567")
	juan := s.customer(f.tenantA, "Juan", "100%real", "")
	s.customer(f.tenantA, "Juan", "1000x", "0987654321")
	anaB := s.customer(f.tenantB, "Ana", "Beta", "0991112223")

	day := "2026-09-16" // Wednesday
	s.appointment(f.tenantA, f.branchA, f.customerAna, f.serviceA, f.ana, local(day, "09:00"), "confirmed")
	s.appointment(f.tenantA, f.branchA, juan, f.serviceA, f.luis, local(day, "10:00"), "cancelled")
	s.appointment(f.tenantA, f.branchA, juan, f.serviceA, "", local(day, "11:00"), "pending")
	// 23:30 local on the 17th is already the 18th in UTC.
	s.appointment(f.tenantA, f.branchA, juan, f.serviceA, f.luis, local("2026-09-17", "23:30"), "pending")
	s.appointment(f.tenantB, f.branchB, anaB, f.serviceB, f.zoe, local(day, "09:00"), "confirmed")

	s.schedule(f.tenantA, f.ana, f.branchA, "wednesday", "09:00", "13:00", "11:00", "11:30", true)
	s.schedule(f.tenantA, f.ana, f.branchA, "thursday", "09:00", "13:00", "", "", true)
	s.schedule(f.tenantA, f.luis, f.branchA, "wednesday", "09:00", "12:00", "", "", true)
	s.schedule(f.tenantA, f.pedro, f.branchA, "wednesday", "09:00", "12:00", "", "", true)
	s.schedule(f.tenantB, f.zoe, f.branchB, "wednesday", "09:00", "12:00", "", "", true)

	s.exec(`insert into public.schedule_exceptions (specialist_id, branch_id, exception_date, exception_type, start_time, end_time, is_day_off, reason)
		values ($1, $2, '2026-09-16', 'vacation', '10:00', '11:00', false, 'motivo privado')`, f.luis, f.branchA)
	s.exec(`insert into public.schedule_exceptions (specialist_id, branch_id, exception_date, exception_type, is_day_off)
		values (null, $1, '2026-09-16', 'holiday', true)`, f.branchB)
	// No specialist and no branch: not attributable to any tenant, so it must never apply.
	s.exec(`insert into public.schedule_exceptions (specialist_id, branch_id, exception_date, exception_type, is_day_off)
		values (null, null, '2026-09-16', 'holiday', true)`)

	return f
}

func newBusinessStoreTest(t *testing.T) (*BusinessStore, businessFixture) {
	t.Helper()

	s := newSeeder(t, integrationPool(t))
	f := seedBusiness(s)
	s.asServiceRole()

	return NewBusinessStore(s.tx), f
}

func TestBusinessStoreTenantProfile(t *testing.T) {
	t.Parallel()

	store, f := newBusinessStoreTest(t)

	got, err := store.TenantProfile(t.Context(), f.tenantA)
	if err != nil {
		t.Fatalf("TenantProfile() error = %v", err)
	}

	if got.Timezone != "America/Guayaquil" || !strings.HasPrefix(got.Name, "Negocio ") || got.Slug == "" {
		t.Errorf("TenantProfile() = %+v", got)
	}

	if _, err := store.TenantProfile(t.Context(), "00000000-0000-4000-8000-000000000000"); !errors.Is(err, business.ErrNotFound) {
		t.Errorf("unknown tenant error = %v, want ErrNotFound", err)
	}
}

func TestBusinessStoreSnapshotCounts(t *testing.T) {
	t.Parallel()

	store, f := newBusinessStoreTest(t)
	dayStart := local("2026-09-16", "00:00")

	got, err := store.SnapshotCounts(t.Context(), business.SnapshotQuery{
		TenantID: f.tenantA, Since: local("2026-09-01", "00:00"), DayStart: dayStart, DayEnd: dayStart.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("SnapshotCounts() error = %v", err)
	}

	if got.ActiveBranches != 1 || got.ActiveSpecialists != 2 || got.ActiveServices != 1 || got.TodayAppointments != 3 {
		t.Errorf("counts = %+v, want 1 branch, 2 specialists, 1 service, 3 appointments today", got)
	}

	if want := map[string]int{"confirmed": 1, "cancelled": 1, "pending": 2}; !mapsEqual(got.ByStatus, want) {
		t.Errorf("ByStatus = %v, want %v", got.ByStatus, want)
	}

	if len(got.TopServices) != 1 || got.TopServices[0] != (business.ServiceCount{Name: "Corte", AppointmentsCount: 4}) {
		t.Errorf("TopServices = %+v, want [Corte 4]", got.TopServices)
	}
}

func TestBusinessStoreScheduleSummary(t *testing.T) {
	t.Parallel()

	store, f := newBusinessStoreTest(t)
	base := business.ScheduleQuery{
		TenantID: f.tenantA, Timezone: "America/Guayaquil",
		From: local("2026-09-16", "00:00"), To: local("2026-09-18", "00:00"),
	}

	got, err := store.ScheduleSummary(t.Context(), base)
	if err != nil {
		t.Fatalf("ScheduleSummary() error = %v", err)
	}

	wantDays := []business.DayCount{{Date: "2026-09-16", Count: 2}, {Date: "2026-09-17", Count: 1}}
	if !slices.Equal(got.Days, wantDays) {
		t.Errorf("Days = %+v, want %+v (cancelled excluded, bucketed by local date)", got.Days, wantDays)
	}

	names := map[string]int{}
	for _, sc := range got.Specialists {
		names[sc.Name] = sc.Count
	}

	if want := map[string]int{"Ana": 1, "Luis": 1, "Sin asignar": 1}; !mapsEqual(names, want) {
		t.Errorf("Specialists = %v, want %v", names, want)
	}

	onlyAna := base
	onlyAna.SpecialistID = f.ana

	got, err = store.ScheduleSummary(t.Context(), onlyAna)
	if err != nil {
		t.Fatalf("ScheduleSummary(specialist) error = %v", err)
	}

	if !slices.Equal(got.Days, []business.DayCount{{Date: "2026-09-16", Count: 1}}) {
		t.Errorf("Days for Ana = %+v", got.Days)
	}

	otherBranch := base
	otherBranch.BranchID = f.inactiveBranchA

	got, err = store.ScheduleSummary(t.Context(), otherBranch)
	if err != nil || len(got.Days) != 0 {
		t.Errorf("ScheduleSummary(other branch) = %+v, %v; want no days", got, err)
	}
}

func TestBusinessStoreListAppointments(t *testing.T) {
	t.Parallel()

	store, f := newBusinessStoreTest(t)
	query := business.AppointmentQuery{
		TenantID: f.tenantA, From: local("2026-09-16", "00:00"), To: local("2026-09-17", "00:00"), Limit: 2,
	}

	rows, total, err := store.ListAppointments(t.Context(), query)
	if err != nil {
		t.Fatalf("ListAppointments() error = %v", err)
	}

	if total != 3 || len(rows) != 2 {
		t.Fatalf("ListAppointments() = %d rows of %d, want 2 of 3 (tenant B excluded)", len(rows), total)
	}

	first := rows[0]
	if !first.ScheduledAt.Equal(local("2026-09-16", "09:00")) || first.Status != "confirmed" ||
		deref(first.CustomerID) != f.customerAna || deref(first.CustomerPhone) != "+593991234567" ||
		deref(first.SpecialistName) != "Ana" || deref(first.ServiceName) != "Corte" || deref(first.BranchName) != "Centro" ||
		first.EndsAt == nil || first.EstimatedPrice == nil || *first.EstimatedPrice != 15.5 {
		t.Errorf("first row = %+v", first)
	}

	query.Status, query.Offset = "cancelled", 0

	rows, total, err = store.ListAppointments(t.Context(), query)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].Status != "cancelled" {
		t.Errorf("ListAppointments(cancelled) = %d rows, total %d, err %v", len(rows), total, err)
	}

	query.Status, query.Offset = "", 10

	rows, total, err = store.ListAppointments(t.Context(), query)
	if err != nil || total != 3 || len(rows) != 0 {
		t.Errorf("ListAppointments(page beyond end) = %d rows, total %d, err %v; want 0 rows and total 3", len(rows), total, err)
	}
}

func TestBusinessStoreSearchCustomers(t *testing.T) {
	t.Parallel()

	store, f := newBusinessStoreTest(t)

	tests := []struct {
		name    string
		pattern string
		digits  string
		want    []string
	}{
		{name: "name does not cross tenants", pattern: "%ana%", want: []string{"Ana Pérez"}},
		{name: "full name", pattern: "%ana pér%", want: []string{"Ana Pérez"}},
		{name: "escaped wildcard is literal", pattern: `%100\%%`, want: []string{"Juan 100%real"}},
		{name: "phone digits", pattern: "%99123%", digits: "99123", want: []string{"Ana Pérez"}},
		{name: "formatted phone query", pattern: "%0987-654%", digits: "0987654", want: []string{"Juan 1000x"}},
		{name: "phone of another tenant", pattern: "%99111%", digits: "99111", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := store.SearchCustomers(t.Context(), business.CustomerQuery{
				TenantID: f.tenantA, Pattern: tt.pattern, Digits: tt.digits, Limit: 10,
			})
			if err != nil {
				t.Fatalf("SearchCustomers() error = %v", err)
			}

			got := []string{}
			for _, r := range rows {
				got = append(got, r.FirstName+" "+r.LastName)
			}

			slices.Sort(got)

			if !slices.Equal(got, tt.want) {
				t.Errorf("SearchCustomers(%q, %q) = %v, want %v", tt.pattern, tt.digits, got, tt.want)
			}
		})
	}
}

func TestBusinessStoreSlotCatalog(t *testing.T) {
	t.Parallel()

	store, f := newBusinessStoreTest(t)

	svc, err := store.SlotService(t.Context(), f.tenantA, f.serviceA)
	if err != nil || svc.Name != "Corte" || svc.DurationMinutes != 60 || svc.BufferMinutes != 10 || !svc.RequiresSpecialist || !svc.Active {
		t.Errorf("SlotService() = %+v, %v", svc, err)
	}

	if svc, err := store.SlotService(t.Context(), f.tenantA, f.inactiveServiceA); err != nil || svc.Active {
		t.Errorf("SlotService(inactive) = %+v, %v; want Active false", svc, err)
	}

	if _, err := store.SlotService(t.Context(), f.tenantA, f.serviceB); !errors.Is(err, business.ErrNotFound) {
		t.Errorf("SlotService(other tenant) error = %v, want ErrNotFound", err)
	}

	branch, err := store.SlotBranch(t.Context(), f.tenantA, f.branchA)
	if err != nil || branch.Name != "Centro" || branch.Timezone != "America/Guayaquil" || !branch.Active || len(branch.OperatingHours) == 0 {
		t.Errorf("SlotBranch() = %+v, %v", branch, err)
	}

	if _, err := store.SlotBranch(t.Context(), f.tenantA, f.branchB); !errors.Is(err, business.ErrNotFound) {
		t.Errorf("SlotBranch(other tenant) error = %v, want ErrNotFound", err)
	}
}

func TestBusinessStoreAvailability(t *testing.T) {
	t.Parallel()

	store, f := newBusinessStoreTest(t)
	dayStart := local("2026-09-16", "00:00")
	query := business.AvailabilityQuery{
		TenantID: f.tenantA, BranchID: f.branchA, Weekday: "wednesday", Date: "2026-09-16",
		DayStart: dayStart, DayEnd: dayStart.AddDate(0, 0, 1),
	}

	got, err := store.Availability(t.Context(), query)
	if err != nil {
		t.Fatalf("Availability() error = %v", err)
	}

	specialists := map[string]business.SpecialistSchedule{}
	for _, sp := range got.Specialists {
		specialists[sp.Name] = sp
	}

	if len(specialists) != 2 || specialists["Ana"].ID != f.ana || specialists["Luis"].ID != f.luis {
		t.Fatalf("Specialists = %+v, want Ana and Luis only (no inactive, non-specialist or other tenant)", got.Specialists)
	}

	anaShift := specialists["Ana"].Shifts
	if len(anaShift) != 1 || anaShift[0].Start != "09:00:00" || anaShift[0].End != "13:00:00" || deref(anaShift[0].BreakStart) != "11:00:00" {
		t.Errorf("Ana shifts = %+v, want Wednesday 09-13 with break at 11", anaShift)
	}

	if len(got.Exceptions) != 1 || deref(got.Exceptions[0].SpecialistID) != f.luis || got.Exceptions[0].Type != "vacation" ||
		deref(got.Exceptions[0].Start) != "10:00:00" {
		t.Errorf("Exceptions = %+v, want only Luis's vacation (other tenant's and tenant-less holidays excluded)", got.Exceptions)
	}

	if len(got.Busy) != 1 || got.Busy[0].SpecialistID != f.ana || !got.Busy[0].Start.Equal(local("2026-09-16", "09:00")) ||
		!got.Busy[0].End.Equal(local("2026-09-16", "10:00")) {
		t.Errorf("Busy = %+v, want only Ana 09-10 (cancelled excluded)", got.Busy)
	}

	for name, tc := range map[string]struct {
		specialistID string
		wantFound    bool
		wantCount    int
	}{
		"requested specialist":          {specialistID: f.ana, wantFound: true, wantCount: 1},
		"specialist of another tenant":  {specialistID: f.zoe, wantFound: false, wantCount: 0},
		"inactive specialist":           {specialistID: f.pedro, wantFound: false, wantCount: 0},
		"staff who is not a specialist": {specialistID: f.eva, wantFound: false, wantCount: 0},
	} {
		q := query
		q.SpecialistID = tc.specialistID

		got, err := store.Availability(t.Context(), q)
		if err != nil {
			t.Fatalf("Availability(%s) error = %v", name, err)
		}

		if got.SpecialistFound != tc.wantFound || len(got.Specialists) != tc.wantCount {
			t.Errorf("Availability(%s) found = %v specialists = %d, want %v and %d", name, got.SpecialistFound, len(got.Specialists), tc.wantFound, tc.wantCount)
		}
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if b[k] != v {
			return false
		}
	}

	return true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
