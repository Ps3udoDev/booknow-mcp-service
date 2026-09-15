package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
)

// BusinessStore implements business.Store. Every query filters by tenant_id, and joins repeat the
// tenant condition so a foreign key pointing to another tenant can never leak data.
// Queries only reference columns granted to booknow_mcp_service.
type BusinessStore struct {
	db DBTX
}

var _ business.Store = (*BusinessStore)(nil)

// NewBusinessStore returns a business store that queries db.
func NewBusinessStore(db DBTX) *BusinessStore {
	return &BusinessStore{db: db}
}

// TenantProfile returns the tenant name, slug and timezone.
func (s *BusinessStore) TenantProfile(ctx context.Context, tenantID string) (business.TenantProfile, error) {
	var p business.TenantProfile

	err := s.db.QueryRow(ctx, `select name, slug, coalesce(timezone, '') from public.tenants where id = $1`, tenantID).
		Scan(&p.Name, &p.Slug, &p.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return business.TenantProfile{}, business.ErrNotFound
	}

	if err != nil {
		return business.TenantProfile{}, fmt.Errorf("query tenant profile: %w", err)
	}

	return p, nil
}

const snapshotCountsSQL = `
select
	(select count(*) from public.branches b where b.tenant_id = $1 and b.is_active is true),
	(select count(*) from public.profiles p where p.tenant_id = $1 and p.is_specialist is true and p.is_active is true),
	(select count(*) from public.services s where s.tenant_id = $1 and s.is_active is true),
	(select count(*) from public.appointments a where a.tenant_id = $1 and a.scheduled_at >= $2 and a.scheduled_at < $3)`

const snapshotStatusSQL = `
select coalesce(a.status::text, 'unknown'), count(*)
from public.appointments a
where a.tenant_id = $1 and a.scheduled_at >= $2
group by 1`

const snapshotTopServicesSQL = `
select s.name, count(*)
from public.appointments a
join public.services s on s.id = a.service_id and s.tenant_id = a.tenant_id
where a.tenant_id = $1 and a.scheduled_at >= $2
group by s.name
order by count(*) desc, s.name
limit 5`

// SnapshotCounts returns the aggregates for get_business_snapshot.
func (s *BusinessStore) SnapshotCounts(ctx context.Context, q business.SnapshotQuery) (business.SnapshotCounts, error) {
	var c business.SnapshotCounts

	if err := s.db.QueryRow(ctx, snapshotCountsSQL, q.TenantID, q.DayStart, q.DayEnd).
		Scan(&c.ActiveBranches, &c.ActiveSpecialists, &c.ActiveServices, &c.TodayAppointments); err != nil {
		return business.SnapshotCounts{}, fmt.Errorf("query snapshot counts: %w", err)
	}

	c.ByStatus = map[string]int{}

	statuses, err := collect(ctx, s.db, snapshotStatusSQL, []any{q.TenantID, q.Since}, func(r pgx.Rows) (struct {
		status string
		count  int
	}, error,
	) {
		var v struct {
			status string
			count  int
		}

		err := r.Scan(&v.status, &v.count)

		return v, err
	})
	if err != nil {
		return business.SnapshotCounts{}, fmt.Errorf("query snapshot statuses: %w", err)
	}

	for _, st := range statuses {
		c.ByStatus[st.status] = st.count
	}

	c.TopServices, err = collect(ctx, s.db, snapshotTopServicesSQL, []any{q.TenantID, q.Since}, func(r pgx.Rows) (business.ServiceCount, error) {
		var v business.ServiceCount
		err := r.Scan(&v.Name, &v.AppointmentsCount)

		return v, err
	})
	if err != nil {
		return business.SnapshotCounts{}, fmt.Errorf("query snapshot top services: %w", err)
	}

	return c, nil
}

// scheduleFilter selects non-cancelled appointments in range, optionally for a branch or specialist.
// $1 tenant, $2 timezone, $3 from, $4 to, $5 branch (empty for any), $6 specialist (empty for any).
const scheduleFilter = `
from public.appointments a
where a.tenant_id = $1
  and a.scheduled_at >= $3 and a.scheduled_at < $4
  and a.status is distinct from 'cancelled'
  and ($5 = '' or a.branch_id = nullif($5, '')::uuid)
  and ($6 = '' or a.specialist_id = nullif($6, '')::uuid)`

const scheduleDaysSQL = `
select to_char(a.scheduled_at at time zone $2, 'YYYY-MM-DD') as day, count(*)` + scheduleFilter + `
group by day
order by day`

// scheduleSpecialistsSQL uses the same filter without the timezone: $1 tenant, $2 from, $3 to,
// $4 branch (empty for any), $5 specialist (empty for any).
const scheduleSpecialistsSQL = `
select a.specialist_id::text, coalesce(p.full_name, 'Sin asignar'), count(*)
from public.appointments a
left join public.profiles p on p.id = a.specialist_id and p.tenant_id = a.tenant_id
where a.tenant_id = $1
  and a.scheduled_at >= $2 and a.scheduled_at < $3
  and a.status is distinct from 'cancelled'
  and ($4 = '' or a.branch_id = nullif($4, '')::uuid)
  and ($5 = '' or a.specialist_id = nullif($5, '')::uuid)
group by 1, 2
order by 3 desc, 2`

// ScheduleSummary returns appointment counts per local day and per specialist.
func (s *BusinessStore) ScheduleSummary(ctx context.Context, q business.ScheduleQuery) (business.ScheduleCounts, error) {
	args := []any{q.TenantID, q.Timezone, q.From, q.To, q.BranchID, q.SpecialistID}

	days, err := collect(ctx, s.db, scheduleDaysSQL, args, func(r pgx.Rows) (business.DayCount, error) {
		var v business.DayCount
		err := r.Scan(&v.Date, &v.Count)

		return v, err
	})
	if err != nil {
		return business.ScheduleCounts{}, fmt.Errorf("query schedule days: %w", err)
	}

	specialistArgs := []any{q.TenantID, q.From, q.To, q.BranchID, q.SpecialistID}

	specialists, err := collect(ctx, s.db, scheduleSpecialistsSQL, specialistArgs, func(r pgx.Rows) (business.SpecialistCount, error) {
		var v business.SpecialistCount
		err := r.Scan(&v.ID, &v.Name, &v.Count)

		return v, err
	})
	if err != nil {
		return business.ScheduleCounts{}, fmt.Errorf("query schedule specialists: %w", err)
	}

	return business.ScheduleCounts{Days: days, Specialists: specialists}, nil
}

const appointmentsFilter = `
from public.appointments a
where a.tenant_id = $1
  and a.scheduled_at >= $2 and a.scheduled_at < $3
  and ($4 = '' or a.status::text = $4)`

const appointmentsCountSQL = `select count(*)` + appointmentsFilter

const appointmentsPageSQL = `
select a.id::text, a.scheduled_at, a.ends_at, a.duration_minutes, coalesce(a.status::text, 'pending'),
       a.estimated_price::float8, a.currency_code, a.customer_notes, a.source,
       c.id::text, c.first_name, c.last_name, c.phone,
       p.id::text, p.full_name,
       sv.id::text, sv.name,
       b.id::text, b.name
from public.appointments a
left join public.customers c on c.id = a.customer_id and c.tenant_id = a.tenant_id
left join public.profiles p on p.id = a.specialist_id and p.tenant_id = a.tenant_id
left join public.services sv on sv.id = a.service_id and sv.tenant_id = a.tenant_id
left join public.branches b on b.id = a.branch_id and b.tenant_id = a.tenant_id
where a.tenant_id = $1
  and a.scheduled_at >= $2 and a.scheduled_at < $3
  and ($4 = '' or a.status::text = $4)
order by a.scheduled_at, a.id
limit $5 offset $6`

// ListAppointments returns a page of appointments and the total matching count.
func (s *BusinessStore) ListAppointments(ctx context.Context, q business.AppointmentQuery) ([]business.AppointmentRow, int, error) {
	var total int
	if err := s.db.QueryRow(ctx, appointmentsCountSQL, q.TenantID, q.From, q.To, q.Status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count appointments: %w", err)
	}

	rows, err := collect(ctx, s.db, appointmentsPageSQL, []any{q.TenantID, q.From, q.To, q.Status, q.Limit, q.Offset},
		func(r pgx.Rows) (business.AppointmentRow, error) {
			var v business.AppointmentRow
			err := r.Scan(&v.ID, &v.ScheduledAt, &v.EndsAt, &v.DurationMinutes, &v.Status,
				&v.EstimatedPrice, &v.CurrencyCode, &v.CustomerNotes, &v.Source,
				&v.CustomerID, &v.CustomerFirstName, &v.CustomerLastName, &v.CustomerPhone,
				&v.SpecialistID, &v.SpecialistName, &v.ServiceID, &v.ServiceName, &v.BranchID, &v.BranchName)

			return v, err
		})
	if err != nil {
		return nil, 0, fmt.Errorf("query appointments: %w", err)
	}

	return rows, total, nil
}

// searchCustomersSQL matches an escaped ILIKE pattern ($2) on names and, when $3 is not empty,
// phone digits containing $3 (digits only, so it cannot inject LIKE wildcards).
const searchCustomersSQL = `
select c.id::text, c.first_name, c.last_name, c.phone, c.created_at
from public.customers c
where c.tenant_id = $1
  and (
    c.first_name ilike $2 escape '\'
    or c.last_name ilike $2 escape '\'
    or c.full_name ilike $2 escape '\'
    or (c.first_name || ' ' || c.last_name) ilike $2 escape '\'
    or ($3 <> '' and regexp_replace(coalesce(c.phone, ''), '[^0-9]', '', 'g') like '%' || $3 || '%')
  )
order by c.created_at desc nulls last, c.id
limit $4`

// SearchCustomers returns customers of the tenant matching the query.
func (s *BusinessStore) SearchCustomers(ctx context.Context, q business.CustomerQuery) ([]business.CustomerRow, error) {
	rows, err := collect(ctx, s.db, searchCustomersSQL, []any{q.TenantID, q.Pattern, q.Digits, q.Limit},
		func(r pgx.Rows) (business.CustomerRow, error) {
			var v business.CustomerRow
			err := r.Scan(&v.ID, &v.FirstName, &v.LastName, &v.Phone, &v.CreatedAt)

			return v, err
		})
	if err != nil {
		return nil, fmt.Errorf("search customers: %w", err)
	}

	return rows, nil
}

// SlotService returns a service of the tenant.
func (s *BusinessStore) SlotService(ctx context.Context, tenantID, serviceID string) (business.SlotService, error) {
	var v business.SlotService

	err := s.db.QueryRow(ctx, `
		select id::text, name, duration_minutes, coalesce(buffer_minutes, 0), base_price::float8, currency_code,
		       coalesce(requires_specialist, true), coalesce(is_active, false)
		from public.services where id = $1 and tenant_id = $2`, serviceID, tenantID).
		Scan(&v.ID, &v.Name, &v.DurationMinutes, &v.BufferMinutes, &v.BasePrice, &v.CurrencyCode, &v.RequiresSpecialist, &v.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return business.SlotService{}, business.ErrNotFound
	}

	if err != nil {
		return business.SlotService{}, fmt.Errorf("query service: %w", err)
	}

	return v, nil
}

// SlotBranch returns a branch of the tenant.
func (s *BusinessStore) SlotBranch(ctx context.Context, tenantID, branchID string) (business.SlotBranch, error) {
	var v business.SlotBranch

	err := s.db.QueryRow(ctx, `
		select id::text, name, coalesce(timezone, ''), operating_hours, coalesce(is_active, false)
		from public.branches where id = $1 and tenant_id = $2`, branchID, tenantID).
		Scan(&v.ID, &v.Name, &v.Timezone, &v.OperatingHours, &v.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return business.SlotBranch{}, business.ErrNotFound
	}

	if err != nil {
		return business.SlotBranch{}, fmt.Errorf("query branch: %w", err)
	}

	return v, nil
}

const specialistFoundSQL = `
select exists (
  select 1 from public.profiles p
  where p.id = $1 and p.tenant_id = $2 and p.is_specialist is true and p.is_active is true
)`

// schedulesSQL returns active shifts at the branch for the weekday of active specialists of the tenant.
const schedulesSQL = `
select p.id::text, p.full_name, ss.start_time::text, ss.end_time::text, ss.break_start::text, ss.break_end::text
from public.specialist_schedules ss
join public.profiles p on p.id = ss.specialist_id and p.tenant_id = ss.tenant_id
where ss.tenant_id = $1
  and ss.branch_id = $2
  and ss.day_of_week::text = $3
  and ss.is_active is not false
  and p.is_specialist is true and p.is_active is true
  and ($4 = '' or p.id = nullif($4, '')::uuid)
order by p.full_name, p.id, ss.start_time`

// exceptionsSQL returns exceptions for the date: those of the listed specialists (for this branch or any)
// and branch-wide ones of this branch. schedule_exceptions has no tenant_id, so branch-wide rows are only
// trusted through a branch already verified to belong to the tenant.
const exceptionsSQL = `
select se.specialist_id::text, se.exception_type, se.start_time::text, se.end_time::text, coalesce(se.is_day_off, false)
from public.schedule_exceptions se
where se.exception_date = $1::date
  and (se.branch_id is null or se.branch_id = $2)
  and (
    (se.specialist_id is null and se.branch_id = $2)
    or se.specialist_id = any($3::uuid[])
  )`

const busySQL = `
select a.specialist_id::text, a.scheduled_at,
       coalesce(a.ends_at, a.scheduled_at + make_interval(mins => a.duration_minutes))
from public.appointments a
where a.tenant_id = $1
  and a.specialist_id = any($2::uuid[])
  and a.status::text in ('pending', 'confirmed', 'in_progress')
  and a.scheduled_at < $4
  and coalesce(a.ends_at, a.scheduled_at + make_interval(mins => a.duration_minutes)) > $3`

// Availability returns schedules, exceptions and busy intervals for one day at a branch.
// The branch must already be verified to belong to the tenant.
func (s *BusinessStore) Availability(ctx context.Context, q business.AvailabilityQuery) (business.Availability, error) {
	var a business.Availability

	if q.SpecialistID != "" {
		if err := s.db.QueryRow(ctx, specialistFoundSQL, q.SpecialistID, q.TenantID).Scan(&a.SpecialistFound); err != nil {
			return business.Availability{}, fmt.Errorf("query specialist: %w", err)
		}

		if !a.SpecialistFound {
			return a, nil
		}
	}

	specialists, err := s.schedules(ctx, q)
	if err != nil {
		return business.Availability{}, err
	}

	a.Specialists = specialists

	ids := make([]string, 0, len(specialists))
	for _, sp := range specialists {
		ids = append(ids, sp.ID)
	}

	a.Exceptions, err = collect(ctx, s.db, exceptionsSQL, []any{q.Date, q.BranchID, ids}, func(r pgx.Rows) (business.ScheduleException, error) {
		var v business.ScheduleException
		err := r.Scan(&v.SpecialistID, &v.Type, &v.Start, &v.End, &v.DayOff)

		return v, err
	})
	if err != nil {
		return business.Availability{}, fmt.Errorf("query schedule exceptions: %w", err)
	}

	a.Busy, err = collect(ctx, s.db, busySQL, []any{q.TenantID, ids, q.DayStart, q.DayEnd}, func(r pgx.Rows) (business.BusyInterval, error) {
		var v business.BusyInterval
		err := r.Scan(&v.SpecialistID, &v.Start, &v.End)

		return v, err
	})
	if err != nil {
		return business.Availability{}, fmt.Errorf("query busy intervals: %w", err)
	}

	return a, nil
}

func (s *BusinessStore) schedules(ctx context.Context, q business.AvailabilityQuery) ([]business.SpecialistSchedule, error) {
	type shiftRow struct {
		id, name string
		shift    business.Shift
	}

	rows, err := collect(ctx, s.db, schedulesSQL, []any{q.TenantID, q.BranchID, q.Weekday, q.SpecialistID}, func(r pgx.Rows) (shiftRow, error) {
		var v shiftRow
		err := r.Scan(&v.id, &v.name, &v.shift.Start, &v.shift.End, &v.shift.BreakStart, &v.shift.BreakEnd)

		return v, err
	})
	if err != nil {
		return nil, fmt.Errorf("query specialist schedules: %w", err)
	}

	var specialists []business.SpecialistSchedule

	for _, row := range rows {
		if n := len(specialists); n > 0 && specialists[n-1].ID == row.id {
			specialists[n-1].Shifts = append(specialists[n-1].Shifts, row.shift)

			continue
		}

		specialists = append(specialists, business.SpecialistSchedule{ID: row.id, Name: row.name, Shifts: []business.Shift{row.shift}})
	}

	return specialists, nil
}

// collect runs a query and scans every row with scan.
func collect[T any](ctx context.Context, db DBTX, sql string, args []any, scan func(pgx.Rows) (T, error)) ([]T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []T

	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, v)
	}

	return out, rows.Err()
}
