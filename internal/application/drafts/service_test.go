package drafts

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
)

const (
	tenantID     = "85a89283-7a3c-4d4c-81cc-fc3d95c03786"
	connectionID = "c0ffee00-0000-4000-8000-000000000001"
	userID       = "7170ce3e-5b1d-4747-9d6b-90ebfba7eada"
	customerID   = "44444444-4444-4444-8444-444444444444"
	serviceID    = "11111111-1111-4111-8111-111111111111"
	variantID    = "55555555-5555-4555-8555-555555555555"
	branchID     = "22222222-2222-4222-8222-222222222222"
	specialistID = "33333333-3333-4333-8333-333333333333"
	draftID      = "66666666-6666-4666-8666-666666666666"
	apptID       = "77777777-7777-4777-8777-777777777777"
	key          = "reserva-ana-0001"
)

var actor = Actor{TenantID: tenantID, ConnectionID: connectionID, UserID: userID}

// fixedNow is Tuesday 2026-09-15 12:00 in Guayaquil.
var fixedNow = time.Date(2026, 9, 15, 17, 0, 0, 0, time.UTC)

// fakeStore keeps drafts in memory, scoped by tenant and connection like the real store.
type fakeStore struct {
	customer    Customer
	customerErr error
	variant     Variant
	variantErr  error

	drafts   map[string]Draft
	inserted []NewDraft
	// raceWith simulates a concurrent insert with the same key: the insert does not create a row.
	raceWith *Draft

	confirmation Confirmation
	confirmErr   error
	confirmCalls []string

	tenantsSeen []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		customer: Customer{ID: customerID, FirstName: "Ana", LastName: "Pérez", Active: true},
		variant:  Variant{ID: variantID, ServiceID: serviceID, Name: "Largo", DurationModifier: 15, PriceModifier: 5, Active: true},
		drafts:   map[string]Draft{},
	}
}

func (f *fakeStore) Customer(_ context.Context, tenant, _ string) (Customer, error) {
	f.tenantsSeen = append(f.tenantsSeen, tenant)

	return f.customer, f.customerErr
}

func (f *fakeStore) Variant(_ context.Context, tenant, _, _ string) (Variant, error) {
	f.tenantsSeen = append(f.tenantsSeen, tenant)

	return f.variant, f.variantErr
}

func (f *fakeStore) DraftByKey(_ context.Context, tenant, connection, k string) (Draft, error) {
	f.tenantsSeen = append(f.tenantsSeen, tenant)

	for _, d := range f.drafts {
		if d.TenantID == tenant && d.ConnectionID == connection && d.IdempotencyKey == k {
			return d, nil
		}
	}

	return Draft{}, ErrNotFound
}

func (f *fakeStore) Draft(_ context.Context, tenant, id string) (Draft, error) {
	f.tenantsSeen = append(f.tenantsSeen, tenant)

	d, ok := f.drafts[id]
	if !ok || d.TenantID != tenant {
		return Draft{}, ErrNotFound
	}

	return d, nil
}

func (f *fakeStore) InsertDraft(_ context.Context, n NewDraft) (string, bool, error) {
	f.tenantsSeen = append(f.tenantsSeen, n.TenantID)

	if f.raceWith != nil {
		f.drafts[f.raceWith.ID] = *f.raceWith

		return "", false, nil
	}

	f.inserted = append(f.inserted, n)
	f.drafts[draftID] = Draft{
		ID: draftID, TenantID: n.TenantID, ConnectionID: n.ConnectionID, CustomerID: n.CustomerID, ServiceID: n.ServiceID,
		VariantID: n.VariantID, BranchID: n.BranchID, SpecialistID: n.SpecialistID, ScheduledAt: n.ScheduledAt, EndsAt: n.EndsAt,
		DurationMinutes: n.DurationMinutes, EstimatedPrice: n.EstimatedPrice, CurrencyCode: n.CurrencyCode, CustomerNotes: n.CustomerNotes,
		Status: StatusDraft, IdempotencyKey: n.IdempotencyKey, ExpiresAt: n.ExpiresAt,
		CustomerFirstName: "Ana", CustomerLastName: "Pérez", ServiceName: "Corte", VariantName: namePtr(n.VariantID, "Largo"),
		BranchName: "Centro", BranchTimezone: "America/Guayaquil", SpecialistName: namePtr(n.SpecialistID, "Luis"),
	}

	return draftID, true, nil
}

func (f *fakeStore) Confirm(_ context.Context, id, actorUserID string) (Confirmation, error) {
	f.confirmCalls = append(f.confirmCalls, id+"|"+actorUserID)

	return f.confirmation, f.confirmErr
}

func namePtr(id *string, name string) *string {
	if id == nil {
		return nil
	}

	return &name
}

// fakeSlots answers CheckSlot with a canned result and records the request.
type fakeSlots struct {
	check business.SlotCheck
	err   error
	in    business.SlotCheckInput
	calls int
}

func (f *fakeSlots) CheckSlot(_ context.Context, _ string, in business.SlotCheckInput) (business.SlotCheck, error) {
	f.in = in
	f.calls++

	return f.check, f.err
}

func availableSlot() *fakeSlots {
	usd := "USD"

	return &fakeSlots{check: business.SlotCheck{
		Service:         business.SlotService{ID: serviceID, Name: "Corte", DurationMinutes: 60, BasePrice: 20, CurrencyCode: &usd, RequiresSpecialist: true, Active: true},
		Branch:          business.SlotBranch{ID: branchID, Name: "Centro", Timezone: "America/Guayaquil", Active: true},
		Timezone:        "America/Guayaquil",
		DurationMinutes: 75,
		Available:       true,
		Specialists:     []business.Ref{{ID: specialistID, Name: "Luis"}},
	}}
}

func newTestService(store *fakeStore, slots *fakeSlots) *Service {
	svc := NewService(store, slots, 10*time.Minute)
	svc.now = func() time.Time { return fixedNow }

	return svc
}

func validCreate() CreateInput {
	return CreateInput{
		CustomerID: customerID, ServiceID: serviceID, ServiceVariantID: variantID, BranchID: branchID, SpecialistID: specialistID,
		ScheduledAt: "2026-09-16T10:00:00-05:00", CustomerNotes: "  Prefiere tijera  ", IdempotencyKey: key,
	}
}

func requireKind(t *testing.T, err, kind error) {
	t.Helper()

	var be *business.Error
	if !errors.Is(err, kind) || !errors.As(err, &be) || be.Message == "" {
		t.Fatalf("error = %v, want a client-safe %v", err, kind)
	}
}

func TestCreateDraft(t *testing.T) {
	t.Parallel()

	store, slots := newFakeStore(), availableSlot()

	got, err := newTestService(store, slots).Create(t.Context(), actor, validCreate())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	start := time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC)
	if !slots.in.Start.Equal(start) || slots.in.ExtraMinutes != 15 || slots.in.SpecialistID != specialistID ||
		slots.in.ServiceID != serviceID || slots.in.BranchID != branchID {
		t.Errorf("slot check = %+v, want the requested start with the variant's extra 15 minutes", slots.in)
	}

	if len(store.inserted) != 1 {
		t.Fatalf("inserted %d drafts, want 1", len(store.inserted))
	}

	n := store.inserted[0]
	if n.TenantID != tenantID || n.ConnectionID != connectionID || n.ActorUserID != userID || n.IdempotencyKey != key ||
		!n.ScheduledAt.Equal(start) || !n.EndsAt.Equal(start.Add(75*time.Minute)) || n.DurationMinutes != 75 ||
		n.EstimatedPrice == nil || *n.EstimatedPrice != 25 || n.CurrencyCode == nil || *n.CurrencyCode != "USD" ||
		n.CustomerNotes == nil || *n.CustomerNotes != "Prefiere tijera" || !n.ExpiresAt.Equal(fixedNow.Add(10*time.Minute)) {
		t.Errorf("inserted draft = %+v", n)
	}

	if got.DraftID != draftID || got.Status != StatusDraft || got.Reused || got.Timezone != "America/Guayaquil" ||
		got.ScheduledAt.Location().String() != "America/Guayaquil" || got.ScheduledAt.Hour() != 10 ||
		got.Specialist == nil || got.Specialist.Name != "Luis" || got.Variant == nil || got.Variant.Name != "Largo" {
		t.Errorf("Create() = %+v", got)
	}

	for _, want := range []string{"Ana Pérez", "Corte (Largo)", "Centro", "Luis", "16/09/2026", "10:00", "11:15", "25.00 USD", "12:10", "confirm_appointment_draft"} {
		if !strings.Contains(got.HumanSummary, want) {
			t.Errorf("HumanSummary = %q, missing %q", got.HumanSummary, want)
		}
	}

	if strings.Contains(got.HumanSummary, "tijera") {
		t.Errorf("HumanSummary = %q, must not repeat customer notes", got.HumanSummary)
	}

	for _, seen := range store.tenantsSeen {
		if seen != tenantID {
			t.Fatalf("store used tenant %s, want only the connection tenant", seen)
		}
	}
}

func TestCreateDraftWithoutVariantOrNotes(t *testing.T) {
	t.Parallel()

	store, slots := newFakeStore(), availableSlot()
	slots.check.DurationMinutes = 60

	in := validCreate()
	in.ServiceVariantID, in.CustomerNotes = "", ""

	got, err := newTestService(store, slots).Create(t.Context(), actor, in)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	n := store.inserted[0]
	if slots.in.ExtraMinutes != 0 || n.VariantID != nil || n.CustomerNotes != nil || *n.EstimatedPrice != 20 || n.DurationMinutes != 60 {
		t.Errorf("inserted draft = %+v, slot check = %+v", n, slots.in)
	}

	if got.Variant != nil || !strings.Contains(got.HumanSummary, "20.00 USD") {
		t.Errorf("Create() = %+v", got)
	}
}

func TestCreateDraftRetryWithSameKey(t *testing.T) {
	t.Parallel()

	store, slots := newFakeStore(), availableSlot()
	svc := newTestService(store, slots)

	first, err := svc.Create(t.Context(), actor, validCreate())
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	// The slot is gone meanwhile: a retry must still return the same draft without checking again.
	slots.check.Available = false

	retry, err := svc.Create(t.Context(), actor, validCreate())
	if err != nil {
		t.Fatalf("retry Create() error = %v", err)
	}

	if retry.DraftID != first.DraftID || !retry.Reused || len(store.inserted) != 1 || slots.calls != 1 {
		t.Errorf("retry = %+v, inserts = %d, slot checks = %d; want the same draft reused", retry, len(store.inserted), slots.calls)
	}

	other := actor
	other.ConnectionID = "c0ffee00-0000-4000-8000-000000000002"
	slots.check.Available = true

	if got, err := svc.Create(t.Context(), other, validCreate()); err != nil || got.Reused {
		t.Errorf("same key on another connection = %+v, %v; want a new draft", got, err)
	}
}

func TestCreateDraftKeyReusedWithOtherData(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	svc := newTestService(store, availableSlot())

	if _, err := svc.Create(t.Context(), actor, validCreate()); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}

	changed := validCreate()
	changed.ScheduledAt = "2026-09-16T11:00:00-05:00"

	_, err := svc.Create(t.Context(), actor, changed)
	requireKind(t, err, business.ErrConflict)

	// The same instant written with another offset and uppercase IDs is the same request.
	same := validCreate()
	same.ScheduledAt = "2026-09-16T15:00:00Z"
	same.CustomerID = strings.ToUpper(customerID)

	if got, err := svc.Create(t.Context(), actor, same); err != nil || !got.Reused {
		t.Errorf("equivalent retry = %+v, %v; want reused", got, err)
	}
}

func TestCreateDraftConcurrentInsertWithSameKey(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	winner := Draft{
		ID: "88888888-8888-4888-8888-888888888888", TenantID: tenantID, ConnectionID: connectionID, IdempotencyKey: key,
		CustomerID: customerID, ServiceID: serviceID, VariantID: new(variantID), BranchID: branchID, SpecialistID: new(specialistID),
		ScheduledAt: time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC), CustomerNotes: new("Prefiere tijera"),
		Status: StatusDraft, ExpiresAt: fixedNow.Add(10 * time.Minute), BranchTimezone: "America/Guayaquil",
	}
	store.raceWith = &winner

	got, err := newTestService(store, availableSlot()).Create(t.Context(), actor, validCreate())
	if err != nil || got.DraftID != winner.ID || !got.Reused {
		t.Errorf("Create() = %+v, %v; want the concurrently inserted draft", got, err)
	}
}

func TestCreateDraftErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*CreateInput, *fakeStore, *fakeSlots)
		want   error
	}{
		{name: "missing idempotency key", mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.IdempotencyKey = " " }, want: business.ErrInvalidArgument},
		{name: "short idempotency key", mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.IdempotencyKey = "abc" }, want: business.ErrInvalidArgument},
		{name: "idempotency key with spaces", mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.IdempotencyKey = "reserva de ana" }, want: business.ErrInvalidArgument},
		{name: "invalid customer id", mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.CustomerID = "ana" }, want: business.ErrInvalidArgument},
		{name: "invalid variant id", mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.ServiceVariantID = "largo" }, want: business.ErrInvalidArgument},
		{name: "date without time", mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.ScheduledAt = "2026-09-16" }, want: business.ErrInvalidArgument},
		{name: "datetime without offset", mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.ScheduledAt = "2026-09-16T10:00:00" }, want: business.ErrInvalidArgument},
		{name: "notes too long", mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.CustomerNotes = strings.Repeat("a", 501) }, want: business.ErrInvalidArgument},
		{name: "customer of another tenant", mutate: func(_ *CreateInput, s *fakeStore, _ *fakeSlots) { s.customerErr = ErrNotFound }, want: business.ErrNotFound},
		{name: "inactive customer", mutate: func(_ *CreateInput, s *fakeStore, _ *fakeSlots) { s.customer.Active = false }, want: business.ErrNotFound},
		{name: "variant of another tenant", mutate: func(_ *CreateInput, s *fakeStore, _ *fakeSlots) { s.variantErr = ErrNotFound }, want: business.ErrNotFound},
		{name: "inactive variant", mutate: func(_ *CreateInput, s *fakeStore, _ *fakeSlots) { s.variant.Active = false }, want: business.ErrNotFound},
		{
			name: "service, branch or specialist of another tenant",
			mutate: func(_ *CreateInput, _ *fakeStore, sl *fakeSlots) {
				sl.err = business.NewError(business.ErrNotFound, "El servicio no existe")
			},
			want: business.ErrNotFound,
		},
		{
			name:   "specialist required by the service",
			mutate: func(in *CreateInput, _ *fakeStore, _ *fakeSlots) { in.SpecialistID = "" },
			want:   business.ErrInvalidArgument,
		},
		{name: "slot not available", mutate: func(_ *CreateInput, _ *fakeStore, sl *fakeSlots) { sl.check.Available = false }, want: business.ErrConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in, store, slots := validCreate(), newFakeStore(), availableSlot()
			tt.mutate(&in, store, slots)

			_, err := newTestService(store, slots).Create(t.Context(), actor, in)
			requireKind(t, err, tt.want)

			if len(store.inserted) != 0 {
				t.Errorf("inserted %d drafts, want none", len(store.inserted))
			}
		})
	}
}

func TestCreateDraftStoreFailureIsInternal(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.customerErr = errors.New("connection reset")

	_, err := newTestService(store, availableSlot()).Create(t.Context(), actor, validCreate())

	var be *business.Error
	if err == nil || errors.As(err, &be) {
		t.Errorf("error = %v, want an internal (non client) error", err)
	}
}

// storedDraft is a pending draft of the actor's connection.
func storedDraft() Draft {
	return Draft{
		ID: draftID, TenantID: tenantID, ConnectionID: connectionID, IdempotencyKey: key, Status: StatusDraft,
		ScheduledAt: time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC), ExpiresAt: fixedNow.Add(5 * time.Minute),
		BranchTimezone: "America/Guayaquil",
	}
}

func TestConfirmDraft(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.drafts[draftID] = storedDraft()
	price, usd := 25.0, "USD"
	store.confirmation = Confirmation{Appointment: AppointmentRecord{
		ID: apptID, Status: "pending", Source: "mcp", DurationMinutes: 75, EstimatedPrice: &price, CurrencyCode: &usd,
		ScheduledAt: time.Date(2026, 9, 16, 15, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 9, 16, 16, 15, 0, 0, time.UTC),
	}}

	got, err := newTestService(store, availableSlot()).Confirm(t.Context(), actor, ConfirmInput{DraftID: draftID, IdempotencyKey: key})
	if err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}

	if len(store.confirmCalls) != 1 || store.confirmCalls[0] != draftID+"|"+userID {
		t.Errorf("confirm calls = %v, want the draft confirmed by the connection user", store.confirmCalls)
	}

	a := got.Appointment
	if got.DraftID != draftID || got.Idempotent || a.ID != apptID || a.Status != "pending" || a.Source != "mcp" ||
		a.ScheduledAt.Location().String() != "America/Guayaquil" || a.ScheduledAt.Hour() != 10 || a.EndsAt.Hour() != 11 || got.Message == "" {
		t.Errorf("Confirm() = %+v", got)
	}
}

func TestConfirmDraftErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        ConfirmInput
		mutate    func(*fakeStore)
		want      error
		wantCalls int
	}{
		{name: "invalid draft id", in: ConfirmInput{DraftID: "abc", IdempotencyKey: key}, want: business.ErrInvalidArgument},
		{name: "missing key", in: ConfirmInput{DraftID: draftID}, want: business.ErrInvalidArgument},
		{name: "draft of another tenant", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) {
			d := storedDraft()
			d.TenantID = "99999999-9999-4999-8999-999999999999"
			s.drafts[draftID] = d
		}, want: business.ErrNotFound},
		{name: "draft of another connection", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) {
			d := storedDraft()
			d.ConnectionID = "c0ffee00-0000-4000-8000-000000000002"
			s.drafts[draftID] = d
		}, want: business.ErrNotFound},
		{name: "key does not match the draft", in: ConfirmInput{DraftID: draftID, IdempotencyKey: "otra-clave-0001"}, want: business.ErrNotFound},
		{name: "expired before calling the database", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) {
			d := storedDraft()
			d.ExpiresAt = fixedNow.Add(-time.Second)
			s.drafts[draftID] = d
		}, want: business.ErrConflict},
		{name: "cancelled draft", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) {
			d := storedDraft()
			d.Status = "cancelled"
			s.drafts[draftID] = d
		}, want: business.ErrConflict},
		{name: "specialist taken meanwhile", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) { s.confirmErr = ErrSpecialistUnavailable }, want: business.ErrConflict, wantCalls: 1},
		{name: "expired in the database", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) { s.confirmErr = ErrDraftExpired }, want: business.ErrConflict, wantCalls: 1},
		{name: "invalid status in the database", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) { s.confirmErr = ErrInvalidStatus }, want: business.ErrConflict, wantCalls: 1},
		{name: "role revoked meanwhile", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) { s.confirmErr = ErrUnauthorized }, want: business.ErrForbidden, wantCalls: 1},
		{name: "deleted meanwhile", in: ConfirmInput{DraftID: draftID, IdempotencyKey: key}, mutate: func(s *fakeStore) { s.confirmErr = ErrNotFound }, want: business.ErrNotFound, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeStore()
			store.drafts[draftID] = storedDraft()

			if tt.mutate != nil {
				tt.mutate(store)
			}

			_, err := newTestService(store, availableSlot()).Confirm(t.Context(), actor, tt.in)
			requireKind(t, err, tt.want)

			if len(store.confirmCalls) != tt.wantCalls {
				t.Errorf("confirm calls = %d, want %d", len(store.confirmCalls), tt.wantCalls)
			}
		})
	}
}

func TestConfirmAlreadyConfirmedDraftIsIdempotent(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	d := storedDraft()
	d.Status, d.ExpiresAt = StatusConfirmed, fixedNow.Add(-time.Hour)
	store.drafts[draftID] = d
	store.confirmation = Confirmation{Idempotent: true, Appointment: AppointmentRecord{ID: apptID, Status: "confirmed", Source: "mcp"}}

	got, err := newTestService(store, availableSlot()).Confirm(t.Context(), actor, ConfirmInput{DraftID: draftID, IdempotencyKey: key})
	if err != nil || !got.Idempotent || got.Appointment.ID != apptID {
		t.Errorf("Confirm() = %+v, %v; want the existing appointment even after the TTL", got, err)
	}
}

func TestConfirmStoreFailureIsInternal(t *testing.T) {
	t.Parallel()

	store := newFakeStore()
	store.drafts[draftID] = storedDraft()
	store.confirmErr = errors.New("SQLSTATE 08006")

	_, err := newTestService(store, availableSlot()).Confirm(t.Context(), actor, ConfirmInput{DraftID: draftID, IdempotencyKey: key})

	var be *business.Error
	if err == nil || errors.As(err, &be) {
		t.Errorf("error = %v, want an internal (non client) error", err)
	}
}

func TestHumanSummaryForReusedDrafts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     string
		expiresAt  time.Time
		wantStatus string
		want       string
	}{
		{name: "expired draft", status: StatusDraft, expiresAt: fixedNow.Add(-time.Minute), wantStatus: StatusExpired, want: "expiró"},
		{name: "confirmed draft", status: StatusConfirmed, expiresAt: fixedNow.Add(-time.Minute), wantStatus: StatusConfirmed, want: "ya fue confirmado"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newFakeStore()
			d := storedDraft()
			d.CustomerID, d.ServiceID, d.BranchID = customerID, serviceID, branchID
			d.VariantID, d.SpecialistID, d.CustomerNotes = new(variantID), new(specialistID), new("Prefiere tijera")
			d.Status, d.ExpiresAt = tt.status, tt.expiresAt
			store.drafts[draftID] = d

			got, err := newTestService(store, availableSlot()).Create(t.Context(), actor, validCreate())
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}

			if !got.Reused || got.Status != tt.wantStatus || !strings.Contains(got.HumanSummary, tt.want) {
				t.Errorf("Create() status = %q summary = %q, want %q containing %q", got.Status, got.HumanSummary, tt.wantStatus, tt.want)
			}
		})
	}
}
