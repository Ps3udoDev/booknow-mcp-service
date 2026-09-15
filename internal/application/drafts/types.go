// Package drafts implements the two-step appointment writes of the BookNow Business MCP:
// create_appointment_draft stores a short-lived draft for human approval, and confirm_appointment_draft
// turns it into a pending appointment through the transactional confirm_mcp_appointment_draft function.
// Tenant, connection and user always come from the authorized MCP connection.
package drafts

import (
	"context"
	"errors"
	"time"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
)

// Draft statuses (mcp_appointment_drafts.status). StatusExpired is also reported for pending drafts past their TTL.
const (
	StatusDraft     = "draft"
	StatusConfirmed = "confirmed"
	StatusExpired   = "expired"
)

// Store errors.
var (
	ErrNotFound = errors.New("not found")
	// The following come from confirm_mcp_appointment_draft.
	ErrUnauthorized          = errors.New("actor not allowed to confirm the draft")
	ErrInvalidStatus         = errors.New("draft is not pending")
	ErrDraftExpired          = errors.New("draft expired")
	ErrSpecialistUnavailable = errors.New("specialist unavailable")
)

// Store persists drafts. Lookups filter by tenant and return ErrNotFound for rows of other tenants.
type Store interface {
	Customer(ctx context.Context, tenantID, customerID string) (Customer, error)
	Variant(ctx context.Context, tenantID, serviceID, variantID string) (Variant, error)
	DraftByKey(ctx context.Context, tenantID, connectionID, idempotencyKey string) (Draft, error)
	Draft(ctx context.Context, tenantID, draftID string) (Draft, error)
	// InsertDraft returns created=false (and no ID) when the connection already used the idempotency key.
	InsertDraft(ctx context.Context, d NewDraft) (id string, created bool, err error)
	// Confirm runs confirm_mcp_appointment_draft; its errors map to the Err* values above.
	Confirm(ctx context.Context, draftID, actorUserID string) (Confirmation, error)
}

// Slots checks that a booking fits the schedule (implemented by business.Service).
type Slots interface {
	CheckSlot(ctx context.Context, tenantID string, in business.SlotCheckInput) (business.SlotCheck, error)
}

// Actor is the authorized MCP connection that performs the operation.
type Actor struct {
	TenantID     string
	ConnectionID string
	UserID       string
}

// Customer is the customer being booked.
type Customer struct {
	ID        string
	FirstName string
	LastName  string
	Active    bool
}

// Variant is a service variant with its duration and price modifiers.
type Variant struct {
	ID               string
	ServiceID        string
	Name             string
	DurationModifier int
	PriceModifier    float64
	Active           bool
}

// NewDraft is a draft to insert.
type NewDraft struct {
	TenantID        string
	ConnectionID    string
	ActorUserID     string
	CustomerID      string
	ServiceID       string
	VariantID       *string
	BranchID        string
	SpecialistID    *string
	ScheduledAt     time.Time
	EndsAt          time.Time
	DurationMinutes int
	EstimatedPrice  *float64
	CurrencyCode    *string
	CustomerNotes   *string
	IdempotencyKey  string
	ExpiresAt       time.Time
}

// Draft is a stored draft with the names needed to describe it.
type Draft struct {
	ID                     string
	TenantID               string
	ConnectionID           string
	CustomerID             string
	ServiceID              string
	VariantID              *string
	BranchID               string
	SpecialistID           *string
	ScheduledAt            time.Time
	EndsAt                 time.Time
	DurationMinutes        int
	EstimatedPrice         *float64
	CurrencyCode           *string
	CustomerNotes          *string
	Status                 string
	IdempotencyKey         string
	ExpiresAt              time.Time
	ConfirmedAppointmentID *string

	CustomerFirstName string
	CustomerLastName  string
	ServiceName       string
	VariantName       *string
	BranchName        string
	BranchTimezone    string
	SpecialistName    *string
}

// Confirmation is the result of confirm_mcp_appointment_draft.
type Confirmation struct {
	Idempotent  bool
	Appointment AppointmentRecord
}

// AppointmentRecord is the appointment returned by the confirmation.
type AppointmentRecord struct {
	ID              string
	Status          string
	Source          string
	ScheduledAt     time.Time
	EndsAt          time.Time
	DurationMinutes int
	EstimatedPrice  *float64
	CurrencyCode    *string
}

// CreateInput are the create_appointment_draft arguments.
type CreateInput struct {
	CustomerID       string
	ServiceID        string
	ServiceVariantID string
	BranchID         string
	SpecialistID     string
	// ScheduledAt is an RFC 3339 datetime with offset.
	ScheduledAt    string
	CustomerNotes  string
	IdempotencyKey string
}

// ConfirmInput are the confirm_appointment_draft arguments.
type ConfirmInput struct {
	DraftID        string
	IdempotencyKey string
}

// Ref is a named entity reference.
type Ref = business.Ref

// DraftView is a draft safe to return to the MCP client. Times are in the branch timezone.
type DraftView struct {
	DraftID                string    `json:"draftId"`
	Status                 string    `json:"status"`
	Reused                 bool      `json:"reused"`
	ScheduledAt            time.Time `json:"scheduledAt"`
	EndsAt                 time.Time `json:"endsAt"`
	Timezone               string    `json:"timezone"`
	DurationMinutes        int       `json:"durationMinutes"`
	EstimatedPrice         *float64  `json:"estimatedPrice"`
	CurrencyCode           *string   `json:"currencyCode"`
	ExpiresAt              time.Time `json:"expiresAt"`
	Customer               Ref       `json:"customer"`
	Service                Ref       `json:"service"`
	Variant                *Ref      `json:"variant"`
	Branch                 Ref       `json:"branch"`
	Specialist             *Ref      `json:"specialist"`
	ConfirmedAppointmentID *string   `json:"confirmedAppointmentId,omitempty"`
	HumanSummary           string    `json:"humanSummary"`
}

// AppointmentView is a confirmed appointment safe to return to the MCP client.
type AppointmentView struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	Source          string    `json:"source"`
	ScheduledAt     time.Time `json:"scheduledAt"`
	EndsAt          time.Time `json:"endsAt"`
	DurationMinutes int       `json:"durationMinutes"`
	EstimatedPrice  *float64  `json:"estimatedPrice"`
	CurrencyCode    *string   `json:"currencyCode"`
}

// ConfirmResult is the result of confirm_appointment_draft.
type ConfirmResult struct {
	DraftID     string          `json:"draftId"`
	Idempotent  bool            `json:"idempotent"`
	Timezone    string          `json:"timezone"`
	Appointment AppointmentView `json:"appointment"`
	Message     string          `json:"message"`
}
