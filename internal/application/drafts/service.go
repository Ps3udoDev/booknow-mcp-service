package drafts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
)

const (
	minKeyLength   = 8
	maxKeyLength   = 100
	maxNotesRunes  = 500
	scheduledAtFmt = "2006-01-02T15:04:05-07:00"
)

var weekdayNames = [...]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}

// Service implements create_appointment_draft and confirm_appointment_draft.
type Service struct {
	store Store
	slots Slots
	ttl   time.Duration
	now   func() time.Time
}

// NewService returns a Service whose drafts expire after ttl.
func NewService(store Store, slots Slots, ttl time.Duration) *Service {
	return &Service{store: store, slots: slots, ttl: ttl, now: time.Now}
}

// Create validates the booking against the schedule and stores a draft for human approval.
// Retrying with the same idempotency key and data returns the stored draft without checking the schedule again.
func (s *Service) Create(ctx context.Context, actor Actor, in CreateInput) (DraftView, error) {
	req, err := parseCreate(in)
	if err != nil {
		return DraftView{}, err
	}

	existing, err := s.store.DraftByKey(ctx, actor.TenantID, actor.ConnectionID, req.key)
	switch {
	case err == nil:
		return s.reuse(existing, req)
	case !errors.Is(err, ErrNotFound):
		return DraftView{}, fmt.Errorf("find draft by key: %w", err)
	}

	customer, err := s.store.Customer(ctx, actor.TenantID, req.customerID)
	if err := entityErr(err, customer.Active, "El cliente no existe o no está activo en este negocio."); err != nil {
		return DraftView{}, err
	}

	var variant Variant

	if req.variantID != nil {
		variant, err = s.store.Variant(ctx, actor.TenantID, req.serviceID, *req.variantID)
		if err := entityErr(err, variant.Active, "La variante no existe, no está activa o no pertenece al servicio."); err != nil {
			return DraftView{}, err
		}
	}

	check, err := s.slots.CheckSlot(ctx, actor.TenantID, business.SlotCheckInput{
		ServiceID: req.serviceID, BranchID: req.branchID, SpecialistID: deref(req.specialistID),
		Start: req.scheduledAt, ExtraMinutes: variant.DurationModifier,
	})
	if err != nil {
		return DraftView{}, fmt.Errorf("check slot: %w", err)
	}

	if check.Service.RequiresSpecialist && req.specialistID == nil {
		return DraftView{}, business.NewError(business.ErrInvalidArgument,
			"specialistId es obligatorio para este servicio. Usa list_available_slots para ver los especialistas libres.")
	}

	if !check.Available {
		return DraftView{}, business.NewError(business.ErrConflict,
			"El horario solicitado no está disponible. Consulta list_available_slots y elige otro horario.")
	}

	price := math.Round((check.Service.BasePrice+variant.PriceModifier)*100) / 100

	id, created, err := s.store.InsertDraft(ctx, NewDraft{
		TenantID: actor.TenantID, ConnectionID: actor.ConnectionID, ActorUserID: actor.UserID,
		CustomerID: req.customerID, ServiceID: req.serviceID, VariantID: req.variantID, BranchID: req.branchID,
		SpecialistID: req.specialistID, ScheduledAt: req.scheduledAt,
		EndsAt:          req.scheduledAt.Add(time.Duration(check.DurationMinutes) * time.Minute),
		DurationMinutes: check.DurationMinutes, EstimatedPrice: &price, CurrencyCode: check.Service.CurrencyCode,
		CustomerNotes: req.notes, IdempotencyKey: req.key, ExpiresAt: s.now().Add(s.ttl),
	})
	if err != nil {
		return DraftView{}, fmt.Errorf("insert draft: %w", err)
	}

	if !created {
		// A concurrent call with the same key won the insert.
		existing, err := s.store.DraftByKey(ctx, actor.TenantID, actor.ConnectionID, req.key)
		if err != nil {
			return DraftView{}, fmt.Errorf("find concurrent draft: %w", err)
		}

		return s.reuse(existing, req)
	}

	draft, err := s.store.Draft(ctx, actor.TenantID, id)
	if err != nil {
		return DraftView{}, fmt.Errorf("read created draft: %w", err)
	}

	return s.view(draft, false), nil
}

// Confirm turns a pending draft of the actor's connection into a pending appointment.
// Confirming an already confirmed draft returns the same appointment.
func (s *Service) Confirm(ctx context.Context, actor Actor, in ConfirmInput) (ConfirmResult, error) {
	draftID := strings.ToLower(strings.TrimSpace(in.DraftID))
	if !business.IsUUID(draftID) {
		return ConfirmResult{}, business.NewError(business.ErrInvalidArgument, "draftId debe ser un UUID válido.")
	}

	key, err := parseKey(in.IdempotencyKey)
	if err != nil {
		return ConfirmResult{}, err
	}

	notFound := business.NewError(business.ErrNotFound, "El borrador no existe o no pertenece a esta conexión.")

	draft, err := s.store.Draft(ctx, actor.TenantID, draftID)
	if errors.Is(err, ErrNotFound) {
		return ConfirmResult{}, notFound
	}

	if err != nil {
		return ConfirmResult{}, fmt.Errorf("read draft: %w", err)
	}

	// Only the connection that created the draft, with the key it used, may confirm it.
	if draft.ConnectionID != actor.ConnectionID || draft.IdempotencyKey != key {
		return ConfirmResult{}, notFound
	}

	switch status := s.status(draft); status {
	case StatusDraft, StatusConfirmed:
	case StatusExpired:
		return ConfirmResult{}, expiredError()
	default:
		return ConfirmResult{}, business.NewError(business.ErrConflict,
			fmt.Sprintf("El borrador no se puede confirmar (estado: %s).", status))
	}

	confirmation, err := s.store.Confirm(ctx, draftID, actor.UserID)
	if err != nil {
		return ConfirmResult{}, confirmError(err)
	}

	loc := location(draft.BranchTimezone)
	a := confirmation.Appointment

	message := "Cita creada en estado pendiente."
	if confirmation.Idempotent {
		message = "El borrador ya estaba confirmado; se devuelve la cita existente."
	}

	return ConfirmResult{
		DraftID:    draftID,
		Idempotent: confirmation.Idempotent,
		Timezone:   loc.String(),
		Appointment: AppointmentView{
			ID: a.ID, Status: a.Status, Source: a.Source, ScheduledAt: a.ScheduledAt.In(loc), EndsAt: a.EndsAt.In(loc),
			DurationMinutes: a.DurationMinutes, EstimatedPrice: a.EstimatedPrice, CurrencyCode: a.CurrencyCode,
		},
		Message: message,
	}, nil
}

func confirmError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return business.NewError(business.ErrNotFound, "El borrador no existe o no pertenece a esta conexión.")
	case errors.Is(err, ErrUnauthorized):
		return business.NewError(business.ErrForbidden, "Ya no tienes permisos para confirmar citas en este negocio.")
	case errors.Is(err, ErrDraftExpired):
		return expiredError()
	case errors.Is(err, ErrInvalidStatus):
		return business.NewError(business.ErrConflict, "El borrador ya no está pendiente y no se puede confirmar.")
	case errors.Is(err, ErrSpecialistUnavailable):
		return business.NewError(business.ErrConflict,
			"El especialista ya no está disponible en ese horario. Consulta list_available_slots y crea un nuevo borrador.")
	default:
		return fmt.Errorf("confirm draft: %w", err)
	}
}

func expiredError() error {
	return business.NewError(business.ErrConflict,
		"El borrador expiró sin confirmarse. Crea uno nuevo con otra idempotencyKey si el usuario aún quiere la cita.")
}

// createRequest is a validated and normalized CreateInput.
type createRequest struct {
	customerID, serviceID, branchID string
	variantID, specialistID, notes  *string
	scheduledAt                     time.Time
	key                             string
}

func parseCreate(in CreateInput) (createRequest, error) {
	var (
		req createRequest
		err error
	)

	if req.key, err = parseKey(in.IdempotencyKey); err != nil {
		return createRequest{}, err
	}

	for _, f := range []struct {
		name  string
		value string
		dst   *string
	}{
		{"customerId", in.CustomerID, &req.customerID},
		{"serviceId", in.ServiceID, &req.serviceID},
		{"branchId", in.BranchID, &req.branchID},
	} {
		if *f.dst, err = parseUUID(f.name, f.value); err != nil {
			return createRequest{}, err
		}
	}

	if req.variantID, err = optionalUUID("serviceVariantId", in.ServiceVariantID); err != nil {
		return createRequest{}, err
	}

	if req.specialistID, err = optionalUUID("specialistId", in.SpecialistID); err != nil {
		return createRequest{}, err
	}

	if req.scheduledAt, err = time.Parse(time.RFC3339, strings.TrimSpace(in.ScheduledAt)); err != nil {
		return createRequest{}, business.NewError(business.ErrInvalidArgument,
			"scheduledAt debe ser fecha y hora ISO 8601 con zona horaria, por ejemplo "+scheduledAtFmt+".")
	}

	notes := strings.TrimSpace(in.CustomerNotes)
	if utf8.RuneCountInString(notes) > maxNotesRunes {
		return createRequest{}, business.NewError(business.ErrInvalidArgument,
			fmt.Sprintf("customerNotes no puede superar %d caracteres.", maxNotesRunes))
	}

	if notes != "" {
		req.notes = &notes
	}

	return req, nil
}

func parseKey(value string) (string, error) {
	key := strings.TrimSpace(value)

	valid := len(key) >= minKeyLength && len(key) <= maxKeyLength
	for i := 0; valid && i < len(key); i++ {
		c := key[i]
		valid = ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') || strings.IndexByte("._:-", c) >= 0
	}

	if !valid {
		return "", business.NewError(business.ErrInvalidArgument, fmt.Sprintf(
			"idempotencyKey debe tener entre %d y %d caracteres: letras, números, punto, guion, guion bajo o dos puntos.",
			minKeyLength, maxKeyLength))
	}

	return key, nil
}

func parseUUID(field, value string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(value))
	if !business.IsUUID(id) {
		return "", business.NewError(business.ErrInvalidArgument, field+" debe ser un UUID válido.")
	}

	return id, nil
}

func optionalUUID(field, value string) (*string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	id, err := parseUUID(field, value)
	if err != nil {
		return nil, err
	}

	return &id, nil
}

// reuse returns a draft found by idempotency key, provided it was created with the same data.
func (s *Service) reuse(d Draft, req createRequest) (DraftView, error) {
	same := d.CustomerID == req.customerID && d.ServiceID == req.serviceID && d.BranchID == req.branchID &&
		equalPtr(d.VariantID, req.variantID) && equalPtr(d.SpecialistID, req.specialistID) &&
		equalPtr(d.CustomerNotes, req.notes) && d.ScheduledAt.Equal(req.scheduledAt)
	if !same {
		return DraftView{}, business.NewError(business.ErrConflict,
			"idempotencyKey ya se usó para un borrador con otros datos. Usa una clave nueva para una reserva distinta.")
	}

	return s.view(d, true), nil
}

// status reports pending drafts past their TTL as expired.
func (s *Service) status(d Draft) string {
	if d.Status == StatusDraft && !s.now().Before(d.ExpiresAt) {
		return StatusExpired
	}

	return d.Status
}

func (s *Service) view(d Draft, reused bool) DraftView {
	loc := location(d.BranchTimezone)

	v := DraftView{
		DraftID: d.ID, Status: s.status(d), Reused: reused,
		ScheduledAt: d.ScheduledAt.In(loc), EndsAt: d.EndsAt.In(loc), Timezone: loc.String(),
		DurationMinutes: d.DurationMinutes, EstimatedPrice: d.EstimatedPrice, CurrencyCode: d.CurrencyCode,
		ExpiresAt:              d.ExpiresAt.In(loc),
		Customer:               Ref{ID: d.CustomerID, Name: strings.TrimSpace(d.CustomerFirstName + " " + d.CustomerLastName)},
		Service:                Ref{ID: d.ServiceID, Name: d.ServiceName},
		Variant:                ref(d.VariantID, d.VariantName),
		Branch:                 Ref{ID: d.BranchID, Name: d.BranchName},
		Specialist:             ref(d.SpecialistID, d.SpecialistName),
		ConfirmedAppointmentID: d.ConfirmedAppointmentID,
	}

	v.HumanSummary = humanSummary(v)

	return v
}

// humanSummary describes the booking in Spanish for the user to approve. It never includes customer notes or phones.
func humanSummary(v DraftView) string {
	service := v.Service.Name
	if v.Variant != nil {
		service += " (" + v.Variant.Name + ")"
	}

	with := ""
	if v.Specialist != nil {
		with = " con " + v.Specialist.Name
	}

	booking := fmt.Sprintf("%s: %s en %s%s, el %s %s de %s a %s (%s)",
		v.Customer.Name, service, v.Branch.Name, with,
		weekdayNames[v.ScheduledAt.Weekday()], v.ScheduledAt.Format("02/01/2006"),
		v.ScheduledAt.Format("15:04"), v.EndsAt.Format("15:04"), v.Timezone)

	switch v.Status {
	case StatusDraft:
		return fmt.Sprintf("Borrador de cita pendiente de aprobación para %s. Precio estimado: %s. Expira a las %s. "+
			"Muestra este resumen al usuario y llama a confirm_appointment_draft solo si lo aprueba explícitamente.",
			booking, price(v.EstimatedPrice, v.CurrencyCode), v.ExpiresAt.Format("15:04"))
	case StatusConfirmed:
		return "Este borrador ya fue confirmado y la cita está creada (" + booking + "). No hace falta volver a confirmarlo."
	case StatusExpired:
		return "Este borrador expiró sin confirmarse (" + booking + "). Crea uno nuevo con otra idempotencyKey si el usuario aún quiere la cita."
	default:
		return "Este borrador está en estado " + v.Status + " y no se puede confirmar (" + booking + ")."
	}
}

func price(amount *float64, currency *string) string {
	if amount == nil {
		return "sin definir"
	}

	if currency == nil || *currency == "" {
		return fmt.Sprintf("%.2f", *amount)
	}

	return fmt.Sprintf("%.2f %s", *amount, *currency)
}

func entityErr(err error, active bool, message string) error {
	if errors.Is(err, ErrNotFound) || (err == nil && !active) {
		return business.NewError(business.ErrNotFound, message)
	}

	if err != nil {
		return fmt.Errorf("lookup: %w", err)
	}

	return nil
}

// location resolves the branch timezone; the store already falls back to the tenant timezone.
func location(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil || name == "" {
		return time.UTC
	}

	return loc
}

func ref(id, name *string) *Ref {
	if id == nil {
		return nil
	}

	return &Ref{ID: *id, Name: deref(name)}
}

func equalPtr(a, b *string) bool {
	return deref(a) == deref(b)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
