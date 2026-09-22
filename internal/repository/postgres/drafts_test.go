package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/drafts"
)

type draftFixture struct {
	businessFixture

	admin, connection  string
	variantA, variantB string
}

func (s *seeder) variant(tenantID, serviceID, name string, durationModifier int, priceModifier float64, active bool) string {
	s.t.Helper()

	return s.scanID(`insert into public.service_variants (tenant_id, service_id, name, description, duration_modifier, price_modifier, is_active)
		values ($1, $2, $3, 'descripcion interna', $4, $5, $6) returning id::text`,
		tenantID, serviceID, name, durationModifier, priceModifier, active)
}

func seedDrafts(s *seeder) draftFixture {
	s.t.Helper()

	f := draftFixture{businessFixture: seedBusiness(s)}
	f.admin = s.user()
	s.member(f.tenantA, f.admin, "admin", new(true))
	f.connection = s.connection(f.tenantA, f.admin, testClientID, "active", time.Now())
	f.variantA = s.variant(f.tenantA, f.serviceA, "Largo", 15, 5, true)
	f.variantB = s.variant(f.tenantB, f.serviceB, "Gel", 0, 3, true)

	return f
}

// newDraft is a pending draft for Luis at a local time of 2030-01-09 (no seeded appointments that day).
func (f draftFixture) newDraft(key, hhmm string) drafts.NewDraft {
	at := local("2030-01-09", hhmm)
	price, usd, notes := 25.0, "USD", "Prefiere tijera"

	return drafts.NewDraft{
		TenantID: f.tenantA, ConnectionID: f.connection, ActorUserID: f.admin,
		CustomerID: f.customerAna, ServiceID: f.serviceA, VariantID: &f.variantA, BranchID: f.branchA, SpecialistID: &f.luis,
		ScheduledAt: at, EndsAt: at.Add(75 * time.Minute), DurationMinutes: 75,
		EstimatedPrice: &price, CurrencyCode: &usd, CustomerNotes: &notes,
		IdempotencyKey: key, ExpiresAt: time.Now().Add(10 * time.Minute),
	}
}

func insertDraft(t *testing.T, store *DraftStore, d drafts.NewDraft) string {
	t.Helper()

	id, created, err := store.InsertDraft(t.Context(), d)
	if err != nil || !created || id == "" {
		t.Fatalf("InsertDraft() = %q, %v, %v; want a new draft", id, created, err)
	}

	return id
}

func TestDraftStoreLookups(t *testing.T) {
	t.Parallel()

	s := newSeeder(t, integrationPool(t))
	f := seedDrafts(s)
	inactive := s.variant(f.tenantA, f.serviceA, "Retirada", 0, 0, false)
	s.asServiceRole()

	store := NewDraftStore(s.tx)

	customer, err := store.Customer(t.Context(), f.tenantA, f.customerAna)
	if err != nil || customer.FirstName != "Ana" || customer.LastName != "Pérez" || !customer.Active {
		t.Errorf("Customer() = %+v, %v", customer, err)
	}

	variant, err := store.Variant(t.Context(), f.tenantA, f.serviceA, f.variantA)
	if err != nil || variant.Name != "Largo" || variant.DurationModifier != 15 || variant.PriceModifier != 5 || !variant.Active || variant.ServiceID != f.serviceA {
		t.Errorf("Variant() = %+v, %v", variant, err)
	}

	if v, err := store.Variant(t.Context(), f.tenantA, f.serviceA, inactive); err != nil || v.Active {
		t.Errorf("Variant(inactive) = %+v, %v; want Active false", v, err)
	}

	notFound := map[string]error{
		"customer of another tenant": func() error { _, err := store.Customer(t.Context(), f.tenantB, f.customerAna); return err }(),
		"variant of another tenant":  func() error { _, err := store.Variant(t.Context(), f.tenantA, f.serviceB, f.variantB); return err }(),
		"variant of another service": func() error {
			_, err := store.Variant(t.Context(), f.tenantA, f.inactiveServiceA, f.variantA)
			return err
		}(),
		"variant queried from other tenant": func() error { _, err := store.Variant(t.Context(), f.tenantB, f.serviceA, f.variantA); return err }(),
	}

	for name, err := range notFound {
		if !errors.Is(err, drafts.ErrNotFound) {
			t.Errorf("%s: error = %v, want ErrNotFound", name, err)
		}
	}
}

func TestDraftStoreInsertAndRead(t *testing.T) {
	t.Parallel()

	s := newSeeder(t, integrationPool(t))
	f := seedDrafts(s)
	s.asServiceRole()

	store := NewDraftStore(s.tx)
	want := f.newDraft("reserva-ana-0001", "10:00")
	id := insertDraft(t, store, want)

	if again, created, err := store.InsertDraft(t.Context(), want); err != nil || created || again != "" {
		t.Errorf("InsertDraft(same key) = %q, %v, %v; want no new row", again, created, err)
	}

	byKey, err := store.DraftByKey(t.Context(), f.tenantA, f.connection, want.IdempotencyKey)
	if err != nil {
		t.Fatalf("DraftByKey() error = %v", err)
	}

	byID, err := store.Draft(t.Context(), f.tenantA, id)
	if err != nil {
		t.Fatalf("Draft() error = %v", err)
	}

	for name, got := range map[string]drafts.Draft{"by key": byKey, "by id": byID} {
		if got.ID != id || got.TenantID != f.tenantA || got.ConnectionID != f.connection || got.CustomerID != f.customerAna ||
			got.ServiceID != f.serviceA || deref(got.VariantID) != f.variantA || got.BranchID != f.branchA || deref(got.SpecialistID) != f.luis ||
			!got.ScheduledAt.Equal(want.ScheduledAt) || !got.EndsAt.Equal(want.EndsAt) || got.DurationMinutes != 75 ||
			got.EstimatedPrice == nil || *got.EstimatedPrice != 25 || deref(got.CurrencyCode) != "USD" || deref(got.CustomerNotes) != "Prefiere tijera" ||
			got.Status != drafts.StatusDraft || got.IdempotencyKey != want.IdempotencyKey || got.ConfirmedAppointmentID != nil ||
			got.ExpiresAt.Sub(want.ExpiresAt).Abs() > time.Millisecond {
			t.Errorf("draft %s = %+v", name, got)
		}

		if got.CustomerFirstName != "Ana" || got.CustomerLastName != "Pérez" || got.ServiceName != "Corte" || deref(got.VariantName) != "Largo" ||
			got.BranchName != "Centro" || got.BranchTimezone != "America/Guayaquil" || deref(got.SpecialistName) != "Luis" {
			t.Errorf("draft %s names = %+v", name, got)
		}
	}

	if _, err := store.DraftByKey(t.Context(), f.tenantA, "c0ffee00-0000-4000-8000-000000000009", want.IdempotencyKey); !errors.Is(err, drafts.ErrNotFound) {
		t.Errorf("DraftByKey(other connection) error = %v, want ErrNotFound", err)
	}

	if _, err := store.Draft(t.Context(), f.tenantB, id); !errors.Is(err, drafts.ErrNotFound) {
		t.Errorf("Draft(other tenant) error = %v, want ErrNotFound", err)
	}

	// Foreign keys do not enforce the tenant: names of another tenant's rows must not leak.
	foreign := f.newDraft("reserva-ajena-0001", "12:00")
	foreign.SpecialistID, foreign.VariantID, foreign.CustomerID = &f.zoe, &f.variantB, f.customerB

	leaked, err := store.Draft(t.Context(), f.tenantA, insertDraft(t, store, foreign))
	if err != nil || leaked.SpecialistName != nil || leaked.VariantName != nil || leaked.CustomerFirstName != "" {
		t.Errorf("draft pointing to another tenant = %+v, %v; want no names from tenant B", leaked, err)
	}
}

func TestDraftStoreConfirm(t *testing.T) {
	t.Parallel()

	s := newSeeder(t, integrationPool(t))
	f := seedDrafts(s)
	s.asServiceRole()

	store := NewDraftStore(s.tx)
	d := f.newDraft("reserva-ana-0001", "10:00")
	id := insertDraft(t, store, d)

	first, err := store.Confirm(t.Context(), id, f.admin)
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}

	a := first.Appointment
	if first.Idempotent || a.ID == "" || a.Status != "pending" || a.Source != "mcp" || !a.ScheduledAt.Equal(d.ScheduledAt) ||
		!a.EndsAt.Equal(d.EndsAt) || a.DurationMinutes != 75 || a.EstimatedPrice == nil || *a.EstimatedPrice != 25 || deref(a.CurrencyCode) != "USD" {
		t.Errorf("Confirm() = %+v", first)
	}

	replay, err := store.Confirm(t.Context(), id, f.admin)
	if err != nil || !replay.Idempotent || replay.Appointment.ID != a.ID {
		t.Errorf("Confirm(replay) = %+v, %v; want idempotent with the same appointment", replay, err)
	}

	got, err := store.Draft(t.Context(), f.tenantA, id)
	if err != nil || got.Status != drafts.StatusConfirmed || deref(got.ConfirmedAppointmentID) != a.ID {
		t.Errorf("Draft() after confirm = %+v, %v", got, err)
	}
}

func TestDraftStoreConfirmErrors(t *testing.T) {
	t.Parallel()

	pool := integrationPool(t)

	tests := []struct {
		name string
		// setup returns the draft and actor to confirm.
		setup func(s *seeder, f draftFixture, store *DraftStore) (draftID, actor string)
		want  error
	}{
		{
			name: "unknown draft",
			setup: func(_ *seeder, f draftFixture, _ *DraftStore) (string, string) {
				return "00000000-0000-4000-8000-000000000000", f.admin
			},
			want: drafts.ErrNotFound,
		},
		{
			name: "expired draft",
			setup: func(_ *seeder, f draftFixture, store *DraftStore) (string, string) {
				d := f.newDraft("reserva-expirada", "10:00")
				d.ExpiresAt = time.Now().Add(-time.Minute)

				return insertDraft(t, store, d), f.admin
			},
			want: drafts.ErrDraftExpired,
		},
		{
			name: "specialist already booked",
			setup: func(_ *seeder, f draftFixture, store *DraftStore) (string, string) {
				// Ana has a confirmed appointment on 2026-09-16 09:00-10:00.
				d := f.newDraft("reserva-solapada", "10:00")
				d.SpecialistID = &f.ana
				d.ScheduledAt = local("2026-09-16", "09:30")
				d.EndsAt = d.ScheduledAt.Add(time.Hour)

				return insertDraft(t, store, d), f.admin
			},
			want: drafts.ErrSpecialistUnavailable,
		},
		{
			name: "actor without a staff role in the tenant",
			setup: func(s *seeder, f draftFixture, store *DraftStore) (string, string) {
				employee := s.user()
				s.member(f.tenantA, employee, "employee", new(true))

				return insertDraft(t, store, f.newDraft("reserva-empleado", "10:00")), employee
			},
			want: drafts.ErrUnauthorized,
		},
		{
			name: "cancelled draft",
			setup: func(s *seeder, f draftFixture, store *DraftStore) (string, string) {
				id := insertDraft(t, store, f.newDraft("reserva-cancelada", "10:00"))
				s.exec(`update public.mcp_appointment_drafts set status = 'cancelled' where id = $1`, id)

				return id, f.admin
			},
			want: drafts.ErrInvalidStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The function raises, which aborts the transaction: one transaction per case.
			s := newSeeder(t, pool)
			f := seedDrafts(s)
			store := NewDraftStore(s.tx)
			draftID, actor := tt.setup(s, f, store)
			s.asServiceRole()

			_, err := store.Confirm(t.Context(), draftID, actor)
			if !errors.Is(err, tt.want) {
				t.Errorf("Confirm() error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestDraftStoreConcurrentConfirmations runs confirmations from separate connections on committed data:
// overlapping drafts for the same specialist must produce one appointment, and a draft confirmed twice
// at the same time must produce one appointment and one idempotent reply.
func TestDraftStoreConcurrentConfirmations(t *testing.T) {
	t.Parallel()

	pool := integrationPool(t)

	s := newSeeder(t, pool)
	f := seedDrafts(s)
	seedStore := NewDraftStore(s.tx)
	overlapA := insertDraft(t, seedStore, f.newDraft("concurrente-a", "10:00"))
	overlapB := insertDraft(t, seedStore, f.newDraft("concurrente-b", "10:30"))
	same := insertDraft(t, seedStore, f.newDraft("concurrente-c", "15:00"))

	if err := s.tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit fixtures: %v", err)
	}

	t.Cleanup(func() { cleanupTenants(t, pool, f.tenantA, f.tenantB) })

	overlap := confirmConcurrently(t, pool, f.admin, overlapA, overlapB)

	succeeded, unavailable := 0, 0

	for _, r := range overlap {
		switch {
		case r.err == nil:
			succeeded++
		case errors.Is(r.err, drafts.ErrSpecialistUnavailable):
			unavailable++
		default:
			t.Errorf("Confirm() unexpected error = %v", r.err)
		}
	}

	if succeeded != 1 || unavailable != 1 {
		t.Errorf("overlapping confirmations: %d succeeded, %d unavailable; want 1 and 1", succeeded, unavailable)
	}

	twice := confirmConcurrently(t, pool, f.admin, same, same)
	if twice[0].err != nil || twice[1].err != nil || twice[0].res.Appointment.ID != twice[1].res.Appointment.ID ||
		twice[0].res.Idempotent == twice[1].res.Idempotent {
		t.Errorf("same draft twice = %+v; want one new and one idempotent reply for the same appointment", twice)
	}

	var appointments int
	if err := pool.QueryRow(t.Context(), `select count(*) from public.appointments where tenant_id = $1 and source = 'mcp'`, f.tenantA).
		Scan(&appointments); err != nil || appointments != 2 {
		t.Errorf("mcp appointments = %d (%v), want 2", appointments, err)
	}
}

type confirmOutcome struct {
	res drafts.Confirmation
	err error
}

// confirmConcurrently confirms each draft in its own transaction as booknow_mcp_service, releasing all at once.
func confirmConcurrently(t *testing.T, pool *pgxpool.Pool, actor string, draftIDs ...string) []confirmOutcome {
	t.Helper()

	out := make([]confirmOutcome, len(draftIDs))
	start := make(chan struct{})

	var wg sync.WaitGroup

	for i, id := range draftIDs {
		wg.Go(func() {
			ctx := context.WithoutCancel(t.Context())

			tx, err := pool.Begin(ctx)
			if err != nil {
				out[i].err = err

				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			if _, err := tx.Exec(ctx, `set local role booknow_mcp_service`); err != nil {
				out[i].err = err

				return
			}

			<-start

			out[i].res, out[i].err = NewDraftStore(tx).Confirm(ctx, id, actor)
			if out[i].err == nil {
				out[i].err = tx.Commit(ctx)
			}
		})
	}

	close(start)
	wg.Wait()

	return out
}

// cleanupTenants removes committed fixtures. Most tables cascade from tenants; the rest are deleted first.
func cleanupTenants(t *testing.T, pool *pgxpool.Pool, tenantIDs ...string) {
	t.Helper()

	ctx := context.WithoutCancel(t.Context())

	for _, sql := range []string{
		`delete from public.appointment_services where appointment_id in (select id from public.appointments where tenant_id = any($1::uuid[]))`,
		`delete from public.mcp_appointment_drafts where tenant_id = any($1::uuid[])`,
		`delete from public.appointments where tenant_id = any($1::uuid[])`,
		`delete from public.schedule_exceptions where specialist_id in (select id from public.profiles where tenant_id = any($1::uuid[]))
		   or branch_id in (select id from public.branches where tenant_id = any($1::uuid[]))`,
		`delete from public.service_variants where tenant_id = any($1::uuid[])`,
		`delete from public.specialist_schedules where tenant_id = any($1::uuid[])`,
		`delete from public.tenants where id = any($1::uuid[])`,
	} {
		if _, err := pool.Exec(ctx, sql, tenantIDs); err != nil {
			t.Errorf("cleanup %q: %v", sql, err)
		}
	}
}
