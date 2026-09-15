package postgres

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/auth"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

// TestRemoteSmoke runs the full token → tenant access path against a real Supabase project. It is read-only.
// Load the variables from the gitignored .env.smoke: set -a; . ./.env.smoke; set +a
//
//	SMOKE_SUPABASE_URL   project origin
//	SMOKE_DATABASE_URL   Session Pooler URL for the booknow_mcp_service role
//	SMOKE_ACCESS_TOKEN   OAuth access token of an owner/admin/manager (e.g. from MCP Inspector)
//
// It never logs the token, the connection string or user identifiers.
func TestRemoteSmoke(t *testing.T) {
	supabaseURL := os.Getenv("SMOKE_SUPABASE_URL")
	databaseURL := os.Getenv("SMOKE_DATABASE_URL")
	token := os.Getenv("SMOKE_ACCESS_TOKEN")

	if supabaseURL == "" || databaseURL == "" || token == "" {
		t.Skip("SMOKE_* variables not set; skipping remote smoke test")
	}

	verifier, err := auth.NewVerifier(t.Context(), slog.New(slog.DiscardHandler), supabaseURL, "authenticated")
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	identity, err := verifier.Verify(t.Context(), token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	t.Logf("token: valid, oauth client present = %t, expires in %s", identity.ClientID != "", time.Until(identity.ExpiresAt).Round(time.Minute))

	pool, err := NewPool(t.Context(), databaseURL, 2)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(pool.Close)

	var (
		currentUser, serverVersion       string
		canUpdateConnections, canReadCRM bool
	)

	err = pool.QueryRow(t.Context(), `select current_user, current_setting('server_version'),
		has_table_privilege('public.mcp_connections', 'UPDATE'),
		has_table_privilege('public.customers', 'SELECT')`).Scan(&currentUser, &serverVersion, &canUpdateConnections, &canReadCRM)
	if err != nil {
		t.Fatalf("privilege query error = %v", err)
	}

	t.Logf("database: user=%s postgres=%s", currentUser, serverVersion)

	if currentUser != "booknow_mcp_service" {
		t.Errorf("connected as %q, want booknow_mcp_service (least-privilege role)", currentUser)
	}

	if canUpdateConnections || canReadCRM {
		t.Errorf("role has excess privileges: UPDATE mcp_connections=%t, SELECT customers=%t", canUpdateConnections, canReadCRM)
	}

	access, err := tenant.NewResolver(NewMCPAccessStore(pool)).Resolve(t.Context(), identity)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	t.Logf("access: tenant=%s role=%s scopes=%v", access.TenantSlug, access.Role, access.Scopes)

	// A token alone must never grant access without its own OAuth client binding.
	if _, err := tenant.NewResolver(NewMCPAccessStore(pool)).Resolve(t.Context(), auth.Identity{UserID: identity.UserID}); err == nil {
		t.Error("Resolve() without client id succeeded, want ErrClientRequired")
	}
}
