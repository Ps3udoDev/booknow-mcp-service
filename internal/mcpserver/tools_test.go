package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/platform/audit"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

const secretDBError = "pq: password authentication failed for booknow_mcp_service at 10.1.2.3"

var testAccess = tenant.Access{
	UserID:       "7170ce3e-5b1d-4747-9d6b-90ebfba7eada",
	ClientID:     "9a8b7c6d-5e4f-3a2b-1c0d-9e8f7a6b5c4d",
	ConnectionID: "c0ffee00-0000-4000-8000-000000000001",
	TenantID:     "85a89283-7a3c-4d4c-81cc-fc3d95c03786",
	TenantSlug:   "elvis-studio",
	Role:         tenant.RoleAdmin,
}

// stubStore implements business.Store with canned data and records the tenants it was asked for.
type stubStore struct {
	mu          sync.Mutex
	tenantsSeen []string
	customerQ   business.CustomerQuery
	err         error
	serviceErr  error
}

func (s *stubStore) seen(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tenantsSeen = append(s.tenantsSeen, id)
}

func (s *stubStore) TenantProfile(_ context.Context, tenantID string) (business.TenantProfile, error) {
	s.seen(tenantID)

	return business.TenantProfile{Name: "Elvis Studio", Slug: "elvis-studio", Timezone: "America/Guayaquil"}, s.err
}

func (s *stubStore) SnapshotCounts(_ context.Context, q business.SnapshotQuery) (business.SnapshotCounts, error) {
	s.seen(q.TenantID)

	return business.SnapshotCounts{ActiveBranches: 1}, s.err
}

func (s *stubStore) ScheduleSummary(_ context.Context, q business.ScheduleQuery) (business.ScheduleCounts, error) {
	s.seen(q.TenantID)

	return business.ScheduleCounts{}, s.err
}

func (s *stubStore) ListAppointments(_ context.Context, q business.AppointmentQuery) ([]business.AppointmentRow, int, error) {
	s.seen(q.TenantID)

	return nil, 0, s.err
}

func (s *stubStore) SearchCustomers(_ context.Context, q business.CustomerQuery) ([]business.CustomerRow, error) {
	s.seen(q.TenantID)
	s.customerQ = q
	phone := "+593991234567"

	return []business.CustomerRow{{ID: "c1", FirstName: "Ana", LastName: "Pérez", Phone: &phone}}, s.err
}

func (s *stubStore) SlotService(_ context.Context, tenantID, _ string) (business.SlotService, error) {
	s.seen(tenantID)

	return business.SlotService{}, s.serviceErr
}

func (s *stubStore) SlotBranch(_ context.Context, tenantID, _ string) (business.SlotBranch, error) {
	s.seen(tenantID)

	return business.SlotBranch{}, s.err
}

func (s *stubStore) Availability(_ context.Context, q business.AvailabilityQuery) (business.Availability, error) {
	s.seen(q.TenantID)

	return business.Availability{}, s.err
}

type recorder struct {
	mu    sync.Mutex
	calls []audit.ToolCall
	err   error
}

func (r *recorder) RecordToolCall(_ context.Context, call audit.ToolCall) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, call)

	return r.err
}

func (r *recorder) recorded() []audit.ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.calls)
}

func connectWith(t *testing.T, deps Deps) *mcp.ClientSession {
	t.Helper()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := New(testAccess, deps).Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func toolText(res *mcp.CallToolResult) string {
	var b strings.Builder

	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}

	return b.String()
}

func TestToolsRegistered(t *testing.T) {
	t.Parallel()

	res, err := connectWith(t, Deps{Business: business.NewService(&stubStore{})}).ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	var names []string

	for _, tool := range res.Tools {
		names = append(names, tool.Name)

		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s must be annotated read-only", tool.Name)
		}

		if schema, _ := json.Marshal(tool.InputSchema); strings.Contains(strings.ToLower(string(schema)), "tenant") {
			t.Errorf("tool %s exposes a tenant parameter: %s", tool.Name, schema)
		}
	}

	slices.Sort(names)

	want := []string{"get_business_snapshot", "get_schedule_summary", "health", "list_appointments", "list_available_slots", "search_customers"}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestSearchCustomersToolUsesConnectionTenantAndAudits(t *testing.T) {
	t.Parallel()

	store, rec := &stubStore{}, &recorder{}
	session := connectWith(t, Deps{Business: business.NewService(store), Audit: rec})

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "search_customers",
		Arguments: map[string]any{"query": "+593 99123", "limit": 5},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	if res.IsError {
		t.Fatalf("tool error: %s", toolText(res))
	}

	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"phone_masked":"+593******567"`) || strings.Contains(string(raw), "991234") {
		t.Errorf("structured content = %s, want masked phone only", raw)
	}

	if store.customerQ.TenantID != testAccess.TenantID || store.customerQ.Limit != 5 {
		t.Errorf("store query = %+v, want tenant from the connection and limit 5", store.customerQ)
	}

	calls := rec.recorded()
	if len(calls) != 1 {
		t.Fatalf("audit records = %d, want 1", len(calls))
	}

	call := calls[0]
	if call.ToolName != "search_customers" || call.Status != audit.StatusSucceeded || call.Risk != audit.RiskRead ||
		call.TenantID != testAccess.TenantID || call.ConnectionID != testAccess.ConnectionID ||
		call.ActorUserID != testAccess.UserID || call.ClientID != testAccess.ClientID || call.RequestID == "" || call.ErrorCode != "" {
		t.Errorf("audit record = %+v", call)
	}

	summary, _ := json.Marshal(call.Summary)
	if strings.Contains(string(summary), "99123") || !strings.Contains(string(summary), "queryLength") {
		t.Errorf("audit summary = %s, want query length without the query text", summary)
	}
}

func TestToolArgumentsCannotOverrideTenant(t *testing.T) {
	t.Parallel()

	store := &stubStore{}
	session := connectWith(t, Deps{Business: business.NewService(store)})

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "search_customers",
		Arguments: map[string]any{"query": "ana", "tenantId": "00000000-0000-4000-8000-000000000666", "tenant_id": "00000000-0000-4000-8000-000000000666"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	for _, seen := range store.tenantsSeen {
		if seen != testAccess.TenantID {
			t.Fatalf("store used tenant %s (result error %v: %s), want only the connection tenant", seen, res.IsError, toolText(res))
		}
	}
}

func TestToolErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		store       *stubStore
		call        *mcp.CallToolParams
		wantCode    string
		wantMessage string
	}{
		{
			name:        "invalid argument",
			store:       &stubStore{},
			call:        &mcp.CallToolParams{Name: "search_customers", Arguments: map[string]any{"query": "ab"}},
			wantCode:    "INVALID_ARGUMENT",
			wantMessage: "query debe tener",
		},
		{
			name:  "not found",
			store: &stubStore{serviceErr: business.ErrNotFound},
			call: &mcp.CallToolParams{Name: "list_available_slots", Arguments: map[string]any{
				"serviceId": "11111111-1111-4111-8111-111111111111", "branchId": "22222222-2222-4222-8222-222222222222", "date": "2030-01-01",
			}},
			wantCode:    "NOT_FOUND",
			wantMessage: "servicio no existe",
		},
		{
			name:        "internal error is not exposed",
			store:       &stubStore{err: errors.New(secretDBError)},
			call:        &mcp.CallToolParams{Name: "get_business_snapshot"},
			wantCode:    "INTERNAL",
			wantMessage: "Error interno",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &recorder{}
			session := connectWith(t, Deps{Business: business.NewService(tt.store), Audit: rec})

			res, err := session.CallTool(t.Context(), tt.call)
			if err != nil {
				t.Fatalf("CallTool() protocol error = %v, want a tool error result", err)
			}

			text := toolText(res)
			if !res.IsError || !strings.Contains(text, tt.wantMessage) || strings.Contains(text, "10.1.2.3") {
				t.Errorf("result isError=%v text=%q, want tool error containing %q without internals", res.IsError, text, tt.wantMessage)
			}

			calls := rec.recorded()
			if len(calls) != 1 || calls[0].Status != audit.StatusFailed || calls[0].ErrorCode != tt.wantCode {
				t.Errorf("audit = %+v, want one failed call with code %s", calls, tt.wantCode)
			}
		})
	}
}

func TestAuditFailureDoesNotFailTool(t *testing.T) {
	t.Parallel()

	session := connectWith(t, Deps{Business: business.NewService(&stubStore{}), Audit: &recorder{err: errors.New("insert failed")}})

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_business_snapshot"})
	if err != nil || res.IsError {
		t.Fatalf("CallTool() = %v, %v; want success even if audit fails", res, err)
	}
}

func TestAuditOutlivesClientCancellation(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	session := connectWith(t, Deps{Business: business.NewService(&stubStore{}), Audit: rec})

	if _, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "health"}); err != nil {
		t.Fatalf("CallTool(health) error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for len(rec.recorded()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if calls := rec.recorded(); len(calls) != 1 || calls[0].ToolName != "health" {
		t.Errorf("audit = %+v, want the health call recorded", calls)
	}
}
