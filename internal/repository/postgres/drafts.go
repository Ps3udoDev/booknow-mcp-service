package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/drafts"
)

// DraftStore implements drafts.Store. The service role can insert and read drafts but not update them;
// appointments are only created by confirm_mcp_appointment_draft (security definer).
type DraftStore struct {
	db DBTX
}

var _ drafts.Store = (*DraftStore)(nil)

// NewDraftStore returns a draft store that queries db.
func NewDraftStore(db DBTX) *DraftStore {
	return &DraftStore{db: db}
}

// Customer returns a customer of the tenant.
func (s *DraftStore) Customer(ctx context.Context, tenantID, customerID string) (drafts.Customer, error) {
	var c drafts.Customer

	err := s.db.QueryRow(ctx, `
		select id::text, coalesce(first_name, ''), coalesce(last_name, ''), coalesce(is_active, true)
		from public.customers where id = $1 and tenant_id = $2`, customerID, tenantID).
		Scan(&c.ID, &c.FirstName, &c.LastName, &c.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return drafts.Customer{}, drafts.ErrNotFound
	}

	if err != nil {
		return drafts.Customer{}, fmt.Errorf("query customer: %w", err)
	}

	return c, nil
}

// Variant returns a variant of the tenant's service.
func (s *DraftStore) Variant(ctx context.Context, tenantID, serviceID, variantID string) (drafts.Variant, error) {
	var v drafts.Variant

	err := s.db.QueryRow(ctx, `
		select id::text, service_id::text, name, coalesce(duration_modifier, 0), coalesce(price_modifier, 0)::float8, coalesce(is_active, true)
		from public.service_variants where id = $1 and tenant_id = $2 and service_id = $3`, variantID, tenantID, serviceID).
		Scan(&v.ID, &v.ServiceID, &v.Name, &v.DurationModifier, &v.PriceModifier, &v.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return drafts.Variant{}, drafts.ErrNotFound
	}

	if err != nil {
		return drafts.Variant{}, fmt.Errorf("query service variant: %w", err)
	}

	return v, nil
}

// insertDraftSQL leaves status and confirmation columns to their defaults: the role cannot write them.
const insertDraftSQL = `
insert into public.mcp_appointment_drafts (
	tenant_id, connection_id, actor_auth_user_id, customer_id, service_id, service_variant_id, branch_id, specialist_id,
	scheduled_at, ends_at, duration_minutes, estimated_price, currency_code, customer_notes, idempotency_key, expires_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
on conflict (connection_id, idempotency_key) do nothing
returning id::text`

// InsertDraft inserts a draft unless the connection already used the idempotency key.
func (s *DraftStore) InsertDraft(ctx context.Context, d drafts.NewDraft) (string, bool, error) {
	var id string

	err := s.db.QueryRow(ctx, insertDraftSQL,
		d.TenantID, d.ConnectionID, d.ActorUserID, d.CustomerID, d.ServiceID, d.VariantID, d.BranchID, d.SpecialistID,
		d.ScheduledAt, d.EndsAt, d.DurationMinutes, d.EstimatedPrice, d.CurrencyCode, d.CustomerNotes, d.IdempotencyKey, d.ExpiresAt,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("insert draft: %w", err)
	}

	return id, true, nil
}

// selectDraftSQL reads a draft with its names; joins repeat the tenant so foreign keys cannot leak names.
const selectDraftSQL = `
select d.id::text, d.tenant_id::text, d.connection_id::text, d.customer_id::text, d.service_id::text,
       d.service_variant_id::text, d.branch_id::text, d.specialist_id::text,
       d.scheduled_at, d.ends_at, d.duration_minutes, d.estimated_price::float8, d.currency_code, d.customer_notes,
       d.status, d.idempotency_key, d.expires_at, d.confirmed_appointment_id::text,
       coalesce(c.first_name, ''), coalesce(c.last_name, ''), coalesce(sv.name, ''), v.name, coalesce(b.name, ''),
       coalesce(nullif(b.timezone, ''), nullif(t.timezone, ''), 'America/Guayaquil'), p.full_name
from public.mcp_appointment_drafts d
join public.tenants t on t.id = d.tenant_id
left join public.customers c on c.id = d.customer_id and c.tenant_id = d.tenant_id
left join public.services sv on sv.id = d.service_id and sv.tenant_id = d.tenant_id
left join public.service_variants v on v.id = d.service_variant_id and v.tenant_id = d.tenant_id
left join public.branches b on b.id = d.branch_id and b.tenant_id = d.tenant_id
left join public.profiles p on p.id = d.specialist_id and p.tenant_id = d.tenant_id
where d.tenant_id = $1`

// DraftByKey returns the draft a connection created with an idempotency key.
func (s *DraftStore) DraftByKey(ctx context.Context, tenantID, connectionID, idempotencyKey string) (drafts.Draft, error) {
	return s.draft(ctx, selectDraftSQL+` and d.connection_id = $2 and d.idempotency_key = $3`, tenantID, connectionID, idempotencyKey)
}

// Draft returns a draft of the tenant.
func (s *DraftStore) Draft(ctx context.Context, tenantID, draftID string) (drafts.Draft, error) {
	return s.draft(ctx, selectDraftSQL+` and d.id = $2`, tenantID, draftID)
}

func (s *DraftStore) draft(ctx context.Context, sql string, args ...any) (drafts.Draft, error) {
	var d drafts.Draft

	err := s.db.QueryRow(ctx, sql, args...).Scan(
		&d.ID, &d.TenantID, &d.ConnectionID, &d.CustomerID, &d.ServiceID, &d.VariantID, &d.BranchID, &d.SpecialistID,
		&d.ScheduledAt, &d.EndsAt, &d.DurationMinutes, &d.EstimatedPrice, &d.CurrencyCode, &d.CustomerNotes,
		&d.Status, &d.IdempotencyKey, &d.ExpiresAt, &d.ConfirmedAppointmentID,
		&d.CustomerFirstName, &d.CustomerLastName, &d.ServiceName, &d.VariantName, &d.BranchName, &d.BranchTimezone, &d.SpecialistName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return drafts.Draft{}, drafts.ErrNotFound
	}

	if err != nil {
		return drafts.Draft{}, fmt.Errorf("query draft: %w", err)
	}

	return d, nil
}

// confirmErrors maps the message prefixes raised by confirm_mcp_appointment_draft.
var confirmErrors = map[string]error{
	"DRAFT_NOT_FOUND:":        drafts.ErrNotFound,
	"UNAUTHORIZED:":           drafts.ErrUnauthorized,
	"INVALID_DRAFT_STATUS:":   drafts.ErrInvalidStatus,
	"DRAFT_EXPIRED:":          drafts.ErrDraftExpired,
	"SPECIALIST_UNAVAILABLE:": drafts.ErrSpecialistUnavailable,
}

// raiseException is the SQLSTATE of a plain RAISE EXCEPTION.
const raiseException = "P0001"

type confirmResult struct {
	Idempotent  bool `json:"idempotent"`
	Appointment *struct {
		ID              string     `json:"id"`
		Status          string     `json:"status"`
		Source          string     `json:"source"`
		ScheduledAt     time.Time  `json:"scheduled_at"`
		EndsAt          *time.Time `json:"ends_at"`
		DurationMinutes int        `json:"duration_minutes"`
		EstimatedPrice  *float64   `json:"estimated_price"`
		CurrencyCode    *string    `json:"currency_code"`
	} `json:"appointment"`
}

// Confirm runs confirm_mcp_appointment_draft, which locks the draft, checks the actor and overlaps,
// and creates the appointment atomically.
func (s *DraftStore) Confirm(ctx context.Context, draftID, actorUserID string) (drafts.Confirmation, error) {
	var raw []byte

	err := s.db.QueryRow(ctx, `select public.confirm_mcp_appointment_draft($1, $2)`, draftID, actorUserID).Scan(&raw)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == raiseException {
			for prefix, mapped := range confirmErrors {
				if strings.HasPrefix(pgErr.Message, prefix) {
					return drafts.Confirmation{}, mapped
				}
			}
		}

		return drafts.Confirmation{}, fmt.Errorf("confirm draft: %w", err)
	}

	var res confirmResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return drafts.Confirmation{}, fmt.Errorf("decode confirmation: %w", err)
	}

	if res.Appointment == nil {
		return drafts.Confirmation{}, errors.New("confirm draft: result without appointment")
	}

	a := res.Appointment

	endsAt := a.ScheduledAt.Add(time.Duration(a.DurationMinutes) * time.Minute)
	if a.EndsAt != nil {
		endsAt = *a.EndsAt
	}

	return drafts.Confirmation{
		Idempotent: res.Idempotent,
		Appointment: drafts.AppointmentRecord{
			ID: a.ID, Status: a.Status, Source: a.Source, ScheduledAt: a.ScheduledAt, EndsAt: endsAt,
			DurationMinutes: a.DurationMinutes, EstimatedPrice: a.EstimatedPrice, CurrencyCode: a.CurrencyCode,
		},
	}, nil
}
