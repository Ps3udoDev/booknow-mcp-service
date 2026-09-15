package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/platform/audit"
)

type auditRow struct {
	tool, risk, status, errorCode string
	durationMS                    int
	summary                       string
	clientID, actor, connection   string
}

// readAuditRow reads an audit row as postgres (the service role cannot select audit rows).
func readAuditRow(t *testing.T, s *seeder, requestID string) auditRow {
	t.Helper()

	var r auditRow

	err := s.tx.QueryRow(t.Context(), `
		select tool_name, risk_level, status, coalesce(error_code, ''), duration_ms,
		       safe_summary, oauth_client_id, actor_auth_user_id::text, connection_id::text
		from public.mcp_tool_calls where request_id = $1`, requestID).
		Scan(&r.tool, &r.risk, &r.status, &r.errorCode, &r.durationMS, &r.summary, &r.clientID, &r.actor, &r.connection)
	if err != nil {
		t.Fatalf("read audit row %s: %v", requestID, err)
	}

	return r
}

func TestAuditStoreIntegration(t *testing.T) {
	t.Parallel()

	pool := integrationPool(t)
	s := newSeeder(t, pool)
	userID, tenantID, _, connID := s.grantedFixture()

	store := NewAuditStore(s.tx)
	s.asServiceRole()

	failed := audit.ToolCall{
		TenantID:     tenantID,
		ConnectionID: connID,
		ActorUserID:  userID,
		ClientID:     testClientID,
		RequestID:    "req-audit-1",
		ToolName:     "list_appointments",
		Risk:         audit.RiskRead,
		Summary:      map[string]any{"startDate": "2026-09-01", "limit": 20},
		Status:       audit.StatusFailed,
		ErrorCode:    "INVALID_ARGUMENT",
		Duration:     1500 * time.Microsecond,
	}

	succeeded := failed
	succeeded.RequestID, succeeded.Status, succeeded.ErrorCode, succeeded.Summary = "req-audit-2", audit.StatusSucceeded, "", nil

	for _, call := range []audit.ToolCall{failed, succeeded} {
		if err := store.RecordToolCall(t.Context(), call); err != nil {
			t.Fatalf("RecordToolCall(%s) as service role error = %v", call.RequestID, err)
		}
	}

	s.exec(`reset role`)

	want := auditRow{
		tool: "list_appointments", risk: "read", status: "failed", errorCode: "INVALID_ARGUMENT", durationMS: 2,
		clientID: testClientID, actor: userID, connection: connID,
	}

	first := readAuditRow(t, s, "req-audit-1")

	var summary map[string]any
	if err := json.Unmarshal([]byte(first.summary), &summary); err != nil || summary["startDate"] != "2026-09-01" {
		t.Errorf("safe_summary = %s, want startDate", first.summary)
	}

	first.summary = ""
	if first != want {
		t.Errorf("failed call row = %+v, want %+v", first, want)
	}

	second := readAuditRow(t, s, "req-audit-2")
	if second.status != "succeeded" || second.errorCode != "" || second.summary != "{}" {
		t.Errorf("succeeded call row = %+v (summary %s), want no error code and empty summary", second, second.summary)
	}
}
