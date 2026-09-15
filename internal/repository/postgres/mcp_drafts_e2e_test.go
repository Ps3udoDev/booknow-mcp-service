package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/drafts"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/mcpserver"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

// TestMCPDraftFlowEndToEnd drives the real MCP tools against the local database as booknow_mcp_service:
// available slots → draft → retry → confirm → replay → the appointment is listed.
func TestMCPDraftFlowEndToEnd(t *testing.T) {
	t.Parallel()

	s := newSeeder(t, integrationPool(t))
	f := seedDrafts(s)
	s.asServiceRole()

	businessService := business.NewService(NewBusinessStore(s.tx))
	server := mcpserver.New(tenant.Access{
		UserID: f.admin, ClientID: testClientID, ConnectionID: f.connection, TenantID: f.tenantA, Role: tenant.RoleAdmin,
	}, mcpserver.Deps{
		Business: businessService,
		Drafts:   drafts.NewService(NewDraftStore(s.tx), businessService, 10*time.Minute),
		Audit:    NewAuditStore(s.tx),
	})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	session, err := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// A Wednesday 8 to 14 days ahead: Luis works 09:00-12:00 and no dated fixtures apply.
	day := time.Now().In(gye).AddDate(0, 0, 8)
	for day.Weekday() != time.Wednesday {
		day = day.AddDate(0, 0, 1)
	}

	date := day.Format("2006-01-02")

	var slots business.AvailableSlots

	call(t, session, "list_available_slots", map[string]any{
		"serviceId": f.serviceA, "branchId": f.branchA, "date": date, "specialistId": f.luis,
	}, &slots)

	if len(slots.Slots) == 0 {
		t.Fatalf("no slots for Luis on %s", date)
	}

	createArgs := map[string]any{
		"customerId": f.customerAna, "serviceId": f.serviceA, "serviceVariantId": f.variantA, "branchId": f.branchA,
		"specialistId": f.luis, "scheduledAt": slots.Slots[0].Start.Format(time.RFC3339), "customerNotes": "Primera visita",
		"idempotencyKey": "e2e-reserva-0001",
	}

	var draft, retry drafts.DraftView

	call(t, session, "create_appointment_draft", createArgs, &draft)
	call(t, session, "create_appointment_draft", createArgs, &retry)

	if draft.Status != drafts.StatusDraft || draft.DurationMinutes != 75 || draft.EstimatedPrice == nil || *draft.EstimatedPrice != 25 ||
		draft.Specialist == nil || draft.Specialist.Name != "Luis" || draft.HumanSummary == "" {
		t.Errorf("draft = %+v", draft)
	}

	if retry.DraftID != draft.DraftID || !retry.Reused {
		t.Errorf("retry = %+v, want the same draft reused", retry)
	}

	confirmArgs := map[string]any{"draftId": draft.DraftID, "idempotencyKey": "e2e-reserva-0001"}

	var confirmed, replay drafts.ConfirmResult

	call(t, session, "confirm_appointment_draft", confirmArgs, &confirmed)
	call(t, session, "confirm_appointment_draft", confirmArgs, &replay)

	if confirmed.Idempotent || confirmed.Appointment.Status != "pending" || confirmed.Appointment.Source != "mcp" ||
		!confirmed.Appointment.ScheduledAt.Equal(slots.Slots[0].Start) {
		t.Errorf("confirmed = %+v", confirmed)
	}

	if !replay.Idempotent || replay.Appointment.ID != confirmed.Appointment.ID {
		t.Errorf("replay = %+v, want the same appointment", replay)
	}

	var listed business.AppointmentList

	call(t, session, "list_appointments", map[string]any{"startDate": date, "endDate": date}, &listed)

	if listed.Pagination.Total != 1 || listed.Items[0].ID != confirmed.Appointment.ID || *listed.Items[0].Source != "mcp" {
		t.Errorf("listed = %+v, want the new mcp appointment", listed)
	}

	var after business.AvailableSlots

	call(t, session, "list_available_slots", map[string]any{
		"serviceId": f.serviceA, "branchId": f.branchA, "date": date, "specialistId": f.luis,
	}, &after)

	if len(after.Slots) > 0 && after.Slots[0].Start.Equal(slots.Slots[0].Start) {
		t.Errorf("slot %s still offered after booking", slots.Slots[0].Start)
	}
}

func call(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}

	raw, _ := json.Marshal(res.StructuredContent)
	if res.IsError {
		t.Fatalf("CallTool(%s) tool error: %+v", name, res.Content)
	}

	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s result %s: %v", name, raw, err)
	}
}
