// Package mcpserver builds the BookNow Business MCP server and its tools.
// A server is created per request for an already authorized tenant.Access, so tools never
// receive the tenant as input and cannot observe another request's access.
package mcpserver

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/application/business"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/platform/audit"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

const (
	serverName    = "booknow-business-mcp"
	serverVersion = "0.2.0"
	// auditTimeout bounds the audit insert, which runs detached from the client's cancellation.
	auditTimeout = 2 * time.Second
)

// Stable audit error codes (never free-text messages, which could carry personal data).
const (
	codeInvalidArgument = "INVALID_ARGUMENT"
	codeNotFound        = "NOT_FOUND"
	codeInternal        = "INTERNAL"
)

const internalErrorMessage = "Error interno al ejecutar la herramienta. Inténtalo de nuevo más tarde."

// clientError is a tool error message written for the MCP client (full sentences in Spanish).
type clientError string

func (e clientError) Error() string { return string(e) }

// Deps are the collaborators tools need. Business tools are registered only when Business is set.
type Deps struct {
	Business *business.Service
	// Audit is optional; nil disables tool call auditing.
	Audit  audit.Recorder
	Logger *slog.Logger
}

// New returns an MCP server whose tools act on behalf of access.
func New(access tenant.Access, deps Deps) *mcp.Server {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "BookNow Business MCP",
		Version: serverVersion,
	}, nil)

	env := toolEnv{access: access, deps: deps}

	addHealthTool(server, env)

	if deps.Business != nil {
		addBusinessTools(server, env)
	}

	return server
}

type toolEnv struct {
	access tenant.Access
	deps   Deps
}

// toolSpec describes a tool, how to audit its input and how to run it.
type toolSpec[In, Out any] struct {
	tool *mcp.Tool
	// summary returns the audit summary for the input; it must not include personal data.
	summary func(In) map[string]any
	run     func(ctx context.Context, in In) (Out, error)
}

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)}

// register adds a tool whose calls are timed, audited and whose errors are safe for the client.
func register[In, Out any](server *mcp.Server, env toolEnv, spec toolSpec[In, Out]) {
	mcp.AddTool(server, spec.tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		start := time.Now()
		out, err := spec.run(ctx, in)

		status, code, clientErr := classify(err)
		if code == codeInternal {
			env.deps.Logger.ErrorContext(ctx, "mcp tool failed", slog.String("tool", spec.tool.Name), slog.Any("error", err))
		}

		var summary map[string]any
		if spec.summary != nil {
			summary = spec.summary(in)
		}

		env.audit(ctx, spec.tool.Name, summary, status, code, time.Since(start))

		if clientErr != nil {
			var zero Out

			return nil, zero, clientErr
		}

		return nil, out, nil
	})
}

// classify maps a use case error to an audit status, a stable code and the error shown to the client.
func classify(err error) (audit.Status, string, error) {
	if err == nil {
		return audit.StatusSucceeded, "", nil
	}

	var be *business.Error

	switch {
	case errors.As(err, &be) && errors.Is(err, business.ErrInvalidArgument):
		return audit.StatusFailed, codeInvalidArgument, clientError(be.Message)
	case errors.As(err, &be) && errors.Is(err, business.ErrNotFound):
		return audit.StatusFailed, codeNotFound, clientError(be.Message)
	default:
		return audit.StatusFailed, codeInternal, clientError(internalErrorMessage)
	}
}

func (e toolEnv) audit(ctx context.Context, tool string, summary map[string]any, status audit.Status, code string, d time.Duration) {
	if e.deps.Audit == nil {
		return
	}

	// The call already happened: record it even if the client disconnects meanwhile.
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditTimeout)
	defer cancel()

	err := e.deps.Audit.RecordToolCall(auditCtx, audit.ToolCall{
		TenantID:     e.access.TenantID,
		ConnectionID: e.access.ConnectionID,
		ActorUserID:  e.access.UserID,
		ClientID:     e.access.ClientID,
		RequestID:    rand.Text(),
		ToolName:     tool,
		Risk:         audit.RiskRead,
		Summary:      summary,
		Status:       status,
		ErrorCode:    code,
		Duration:     d,
	})
	if err != nil {
		e.deps.Logger.WarnContext(ctx, "mcp tool call not audited", slog.String("tool", tool), slog.Any("error", err))
	}
}

type healthOutput struct {
	Status string `json:"status" jsonschema:"always ok when the server can serve the tenant"`
	Tenant string `json:"tenant" jsonschema:"slug of the business this connection acts on"`
	Role   string `json:"role"   jsonschema:"role of the connected user in the business"`
}

// addHealthTool registers a connectivity check that exposes no business data.
func addHealthTool(server *mcp.Server, env toolEnv) {
	register(server, env, toolSpec[struct{}, healthOutput]{
		tool: &mcp.Tool{
			Name:        "health",
			Title:       "Estado de la conexión",
			Description: "Comprueba que la conexión MCP está autorizada y muestra el negocio y el rol con los que opera. No devuelve datos del negocio.",
			Annotations: readOnly,
		},
		run: func(context.Context, struct{}) (healthOutput, error) {
			return healthOutput{Status: "ok", Tenant: env.access.TenantSlug, Role: string(env.access.Role)}, nil
		},
	})
}
