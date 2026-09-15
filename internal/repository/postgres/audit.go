package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/platform/audit"
)

// AuditStore writes MCP tool call audit rows. The service role can only insert them.
type AuditStore struct {
	db DBTX
}

// NewAuditStore returns an audit store that writes through db.
func NewAuditStore(db DBTX) *AuditStore {
	return &AuditStore{db: db}
}

// insertToolCallSQL is a plain INSERT: RETURNING or ON CONFLICT would need SELECT, which the role lacks.
const insertToolCallSQL = `
insert into public.mcp_tool_calls (
	tenant_id, connection_id, actor_auth_user_id, oauth_client_id, request_id, tool_name,
	risk_level, safe_summary, status, error_code, duration_ms
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, nullif($10, ''), $11)`

// RecordToolCall inserts one audit row.
func (s *AuditStore) RecordToolCall(ctx context.Context, call audit.ToolCall) error {
	summary := call.Summary
	if summary == nil {
		summary = map[string]any{}
	}

	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("encode audit summary: %w", err)
	}

	durationMS := int(math.Round(float64(call.Duration.Microseconds()) / 1000))

	if _, err := s.db.Exec(ctx, insertToolCallSQL,
		call.TenantID, call.ConnectionID, call.ActorUserID, call.ClientID, call.RequestID, call.ToolName,
		string(call.Risk), summaryJSON, string(call.Status), call.ErrorCode, durationMS,
	); err != nil {
		return fmt.Errorf("insert mcp tool call: %w", err)
	}

	return nil
}
