package mcpserver

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

// connect returns a client session talking to a server built for access over in-memory transports.
func connect(t *testing.T, access tenant.Access) *mcp.ClientSession {
	t.Helper()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := New(access, Deps{}).Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)

	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func TestServerListsHealthTool(t *testing.T) {
	t.Parallel()

	session := connect(t, tenant.Access{TenantSlug: "elvis-studio", Role: tenant.RoleAdmin})

	res, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)

		if tool.Name == "health" && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
			t.Error("health tool must be annotated as read-only")
		}

		// The tenant comes only from the authenticated connection, never from tool input.
		if schema, _ := json.Marshal(tool.InputSchema); strings.Contains(strings.ToLower(string(schema)), "tenant") {
			t.Errorf("tool %q exposes a tenant parameter", tool.Name)
		}
	}

	if !slices.Contains(names, "health") {
		t.Errorf("tools = %v, want health", names)
	}
}

func TestHealthToolReportsResolvedAccess(t *testing.T) {
	t.Parallel()

	session := connect(t, tenant.Access{
		TenantID:   "85a89283-7a3c-4d4c-81cc-fc3d95c03786",
		TenantSlug: "elvis-studio",
		Role:       tenant.RoleManager,
		UserID:     "7170ce3e-5b1d-4747-9d6b-90ebfba7eada",
	})

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "health"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	if res.IsError {
		t.Fatalf("CallTool() returned tool error: %+v", res.Content)
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}

	want := map[string]any{"status": "ok", "tenant": "elvis-studio", "role": "manager"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("structured[%q] = %v, want %v", k, got[k], v)
		}
	}

	// Identifiers are not needed by the model and must not leak through the health tool.
	for _, forbidden := range []string{"85a89283-7a3c-4d4c-81cc-fc3d95c03786", "7170ce3e-5b1d-4747-9d6b-90ebfba7eada"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("health output leaks identifier %q: %s", forbidden, raw)
		}
	}
}
