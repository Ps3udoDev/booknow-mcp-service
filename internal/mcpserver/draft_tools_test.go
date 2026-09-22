package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/drafts"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/platform/audit"
)

const (
	draftCustomerID = "44444444-4444-4444-8444-444444444444"
	draftServiceID  = "11111111-1111-4111-8111-111111111111"
	draftBranchID   = "22222222-2222-4222-8222-222222222222"
	draftSpecID     = "33333333-3333-4333-8333-333333333333"
	draftID         = "66666666-6666-4666-8666-666666666666"
	draftKey        = "reserva-ana-0001"
	draftNotes      = "Alergia al tinte, llamar al 0991234567"
)

// stubDraftStore keeps one draft in memory and records the tenants and connections it was asked for.
type stubDraftStore struct {
	mu      sync.Mutex
	draft   *drafts.Draft
	seen    []string
	confirm error
}

func (s *stubDraftStore) record(tenantID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seen = append(s.seen, tenantID)
}

func (s *stubDraftStore) Customer(_ context.Context, tenantID, id string) (drafts.Customer, error) {
	s.record(tenantID)

	return drafts.Customer{ID: id, FirstName: "Ana", LastName: "Pérez", Active: true}, nil
}

func (s *stubDraftStore) Variant(_ context.Context, tenantID, _, _ string) (drafts.Variant, error) {
	s.record(tenantID)

	return drafts.Variant{}, drafts.ErrNotFound
}

func (s *stubDraftStore) DraftByKey(_ context.Context, tenantID, connectionID, key string) (drafts.Draft, error) {
	s.record(tenantID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.draft == nil || s.draft.ConnectionID != connectionID || s.draft.IdempotencyKey != key {
		return drafts.Draft{}, drafts.ErrNotFound
	}

	return *s.draft, nil
}

func (s *stubDraftStore) Draft(_ context.Context, tenantID, _ string) (drafts.Draft, error) {
	s.record(tenantID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.draft == nil || s.draft.TenantID != tenantID {
		return drafts.Draft{}, drafts.ErrNotFound
	}

	return *s.draft, nil
}

func (s *stubDraftStore) InsertDraft(_ context.Context, d drafts.NewDraft) (string, bool, error) {
	s.record(d.TenantID)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.draft = &drafts.Draft{
		ID: draftID, TenantID: d.TenantID, ConnectionID: d.ConnectionID, CustomerID: d.CustomerID, ServiceID: d.ServiceID,
		BranchID: d.BranchID, SpecialistID: d.SpecialistID, ScheduledAt: d.ScheduledAt, EndsAt: d.EndsAt, DurationMinutes: d.DurationMinutes,
		EstimatedPrice: d.EstimatedPrice, CurrencyCode: d.CurrencyCode, CustomerNotes: d.CustomerNotes, Status: drafts.StatusDraft,
		IdempotencyKey: d.IdempotencyKey, ExpiresAt: d.ExpiresAt, CustomerFirstName: "Ana", CustomerLastName: "Pérez",
		ServiceName: "Corte", BranchName: "Centro", BranchTimezone: "America/Guayaquil",
	}

	return draftID, true, nil
}

func (s *stubDraftStore) Confirm(_ context.Context, _, _ string) (drafts.Confirmation, error) {
	if s.confirm != nil {
		return drafts.Confirmation{}, s.confirm
	}

	return drafts.Confirmation{Appointment: drafts.AppointmentRecord{
		ID: "77777777-7777-4777-8777-777777777777", Status: "pending", Source: "mcp",
		ScheduledAt: time.Date(2030, 1, 9, 15, 0, 0, 0, time.UTC), EndsAt: time.Date(2030, 1, 9, 16, 0, 0, 0, time.UTC), DurationMinutes: 60,
	}}, nil
}

type stubSlots struct{ available bool }

func (s stubSlots) CheckSlot(context.Context, string, business.SlotCheckInput) (business.SlotCheck, error) {
	return business.SlotCheck{
		Service:  business.SlotService{ID: draftServiceID, Name: "Corte", DurationMinutes: 60, BasePrice: 20, RequiresSpecialist: true, Active: true},
		Branch:   business.SlotBranch{ID: draftBranchID, Name: "Centro", Timezone: "America/Guayaquil", Active: true},
		Timezone: "America/Guayaquil", DurationMinutes: 60, Available: s.available,
	}, nil
}

func draftDeps(store *stubDraftStore, rec *recorder, available bool) Deps {
	deps := Deps{
		Business: business.NewService(&stubStore{}),
		Drafts:   drafts.NewService(store, stubSlots{available: available}, 10*time.Minute),
	}

	if rec != nil {
		deps.Audit = rec
	}

	return deps
}

func createDraftArgs() map[string]any {
	return map[string]any{
		"customerId": draftCustomerID, "serviceId": draftServiceID, "branchId": draftBranchID, "specialistId": draftSpecID,
		"scheduledAt": "2030-01-09T10:00:00-05:00", "customerNotes": draftNotes, "idempotencyKey": draftKey,
	}
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) protocol error = %v", name, err)
	}

	return res
}

func TestDraftToolsRegistration(t *testing.T) {
	t.Parallel()

	res, err := connectWith(t, draftDeps(&stubDraftStore{}, nil, true)).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	writes := map[string]bool{}

	for _, tool := range res.Tools {
		if tool.Name != "create_appointment_draft" && tool.Name != "confirm_appointment_draft" {
			continue
		}

		a := tool.Annotations
		if a == nil || a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || !a.IdempotentHint {
			t.Errorf("tool %s annotations = %+v, want write, non-destructive, idempotent", tool.Name, a)
		}

		if schema, _ := json.Marshal(tool.InputSchema); strings.Contains(strings.ToLower(string(schema)), "tenant") {
			t.Errorf("tool %s exposes a tenant parameter: %s", tool.Name, schema)
		}

		writes[tool.Name] = true
	}

	if len(writes) != 2 {
		t.Errorf("write tools = %v, want create_appointment_draft and confirm_appointment_draft", writes)
	}
}

func TestCreateAndConfirmDraftTools(t *testing.T) {
	t.Parallel()

	store, rec := &stubDraftStore{}, &recorder{}
	session := connectWith(t, draftDeps(store, rec, true))

	created := callTool(t, session, "create_appointment_draft", createDraftArgs())
	if created.IsError {
		t.Fatalf("create tool error: %s", toolText(created))
	}

	raw, _ := json.Marshal(created.StructuredContent)
	if !strings.Contains(string(raw), `"draftId":"`+draftID+`"`) || !strings.Contains(string(raw), "humanSummary") ||
		strings.Contains(string(raw), "0991234567") {
		t.Errorf("create structured content = %s, want the draft with a summary and no phone from the notes", raw)
	}

	confirmed := callTool(t, session, "confirm_appointment_draft", map[string]any{"draftId": draftID, "idempotencyKey": draftKey})
	if confirmed.IsError {
		t.Fatalf("confirm tool error: %s", toolText(confirmed))
	}

	store.mu.Lock()
	for _, seen := range store.seen {
		if seen != testAccess.TenantID {
			t.Errorf("store used tenant %s, want only the connection tenant", seen)
		}
	}

	if store.draft.ConnectionID != testAccess.ConnectionID {
		t.Errorf("draft connection = %s, want the authorized connection", store.draft.ConnectionID)
	}
	store.mu.Unlock()

	calls := rec.recorded()
	if len(calls) != 2 {
		t.Fatalf("audit records = %d, want 2", len(calls))
	}

	for _, call := range calls {
		if call.Risk != audit.RiskWrite || call.Status != audit.StatusSucceeded || call.TenantID != testAccess.TenantID {
			t.Errorf("audit record = %+v, want a successful write", call)
		}

		summary, _ := json.Marshal(call.Summary)
		if strings.Contains(string(summary), "Alergia") || strings.Contains(string(summary), "0991234567") || strings.Contains(string(summary), draftKey) {
			t.Errorf("audit summary = %s, must not contain notes or the idempotency key", summary)
		}
	}

	createSummary, _ := json.Marshal(calls[0].Summary)
	if !strings.Contains(string(createSummary), draftCustomerID) || !strings.Contains(string(createSummary), `"hasNotes":true`) {
		t.Errorf("create audit summary = %s, want ids and hasNotes", createSummary)
	}
}

func TestDraftToolErrorCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		available  bool
		confirmErr error
		tool       string
		args       map[string]any
		wantCode   string
		wantStatus audit.Status
		wantText   string
	}{
		{name: "slot taken", tool: "create_appointment_draft", args: createDraftArgs(), wantCode: "CONFLICT", wantStatus: audit.StatusFailed, wantText: "no está disponible"},
		{
			name: "role revoked before confirming", available: true, confirmErr: drafts.ErrUnauthorized,
			tool: "confirm_appointment_draft", wantCode: "FORBIDDEN", wantStatus: audit.StatusDenied, wantText: "permisos",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, rec := &stubDraftStore{confirm: tt.confirmErr}, &recorder{}
			session := connectWith(t, draftDeps(store, rec, tt.available))

			args := tt.args
			if tt.tool == "confirm_appointment_draft" {
				if res := callTool(t, session, "create_appointment_draft", createDraftArgs()); res.IsError {
					t.Fatalf("create tool error: %s", toolText(res))
				}

				args = map[string]any{"draftId": draftID, "idempotencyKey": draftKey}
			}

			res := callTool(t, session, tt.tool, args)
			if !res.IsError || !strings.Contains(toolText(res), tt.wantText) {
				t.Errorf("result isError=%v text=%q, want error containing %q", res.IsError, toolText(res), tt.wantText)
			}

			calls := rec.recorded()
			if len(calls) == 0 {
				t.Fatal("no audit records")
			}

			last := calls[len(calls)-1]

			if last.ToolName != tt.tool || last.ErrorCode != tt.wantCode || last.Status != tt.wantStatus {
				t.Errorf("audit = %+v, want %s with code %s and status %s", last, tt.tool, tt.wantCode, tt.wantStatus)
			}
		})
	}
}

func TestCreateDraftToolRejectsTenantArgument(t *testing.T) {
	t.Parallel()

	store := &stubDraftStore{}
	args := createDraftArgs()
	args["tenantId"] = "00000000-0000-4000-8000-000000000666"

	res := callTool(t, connectWith(t, draftDeps(store, nil, true)), "create_appointment_draft", args)

	store.mu.Lock()
	defer store.mu.Unlock()

	if !res.IsError || len(store.seen) != 0 || store.draft != nil {
		t.Errorf("result isError=%v, store calls = %v; want the call rejected before reaching the store", res.IsError, store.seen)
	}
}
