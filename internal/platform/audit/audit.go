// Package audit describes MCP tool call audit records (table mcp_tool_calls).
// Records must never contain secrets or personal data: summaries hold only identifiers, dates,
// paging values and sizes, and error codes are stable identifiers, not free-text messages.
package audit

import (
	"context"
	"time"
)

// Status is the outcome of a tool call.
type Status string

// Allowed statuses (the table also accepts "started", which this service does not use).
const (
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusDenied    Status = "denied"
)

// Risk classifies what a tool can do.
type Risk string

// Risk levels.
const (
	RiskRead  Risk = "read"
	RiskWrite Risk = "write"
)

// ToolCall is one audited tool invocation.
type ToolCall struct {
	TenantID     string
	ConnectionID string
	ActorUserID  string
	ClientID     string
	// RequestID is unique per tool call.
	RequestID string
	ToolName  string
	Risk      Risk
	// Summary holds non-sensitive parameters only.
	Summary   map[string]any
	Status    Status
	ErrorCode string
	Duration  time.Duration
}

// Recorder persists tool call audit records.
type Recorder interface {
	RecordToolCall(ctx context.Context, call ToolCall) error
}
