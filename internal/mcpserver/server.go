// Package mcpserver builds the BookNow Business MCP server and its tools.
// A server is created per request for an already authorized tenant.Access, so tools never
// receive the tenant as input and cannot observe another request's access.
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

const (
	serverName    = "booknow-business-mcp"
	serverVersion = "0.1.0"
)

// New returns an MCP server whose tools act on behalf of access.
func New(access tenant.Access) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "BookNow Business MCP",
		Version: serverVersion,
	}, nil)

	addHealthTool(server, access)

	return server
}

type healthOutput struct {
	Status string `json:"status" jsonschema:"always ok when the server can serve the tenant"`
	Tenant string `json:"tenant" jsonschema:"slug of the business this connection acts on"`
	Role   string `json:"role"   jsonschema:"role of the connected user in the business"`
}

// addHealthTool registers a connectivity check that exposes no business data.
func addHealthTool(server *mcp.Server, access tenant.Access) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "health",
		Title:       "Estado de la conexión",
		Description: "Comprueba que la conexión MCP está autorizada y muestra el negocio y el rol con los que opera. No devuelve datos del negocio.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)},
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, healthOutput, error) {
		return nil, healthOutput{Status: "ok", Tenant: access.TenantSlug, Role: string(access.Role)}, nil
	})
}
