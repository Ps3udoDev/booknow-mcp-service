package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

// DBTX is satisfied by *pgxpool.Pool and pgx.Tx, so stores run inside or outside a transaction.
type DBTX interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// MCPAccessStore reads MCP authorization state from Supabase tables.
type MCPAccessStore struct {
	db DBTX
}

// NewMCPAccessStore returns a store that queries db.
func NewMCPAccessStore(db DBTX) *MCPAccessStore {
	return &MCPAccessStore{db: db}
}

// findMCPAccessSQL picks the connection for (user, client): an active one first, then the most recently
// consented (the consent flow bumps updated_at on upsert). Membership and module are joined on the
// connection's own tenant, so access in another tenant never leaks into this one.
// is_active NULL counts as active and a missing/NULL module flag as disabled, matching the Next.js MCP.
const findMCPAccessSQL = `
select
	c.id::text,
	c.status = 'active',
	c.scopes,
	t.id::text,
	t.slug,
	t.status = 'active',
	tu.id is not null,
	coalesce(tu.is_active, true),
	coalesce(tu.role::text, ''),
	coalesce(tm.is_enabled, false)
from public.mcp_connections c
join public.tenants t on t.id = c.tenant_id
left join public.tenant_users tu
	on tu.tenant_id = c.tenant_id and tu.auth_user_id = c.auth_user_id
left join public.modules m on m.slug = $3
left join public.tenant_modules tm
	on tm.tenant_id = c.tenant_id and tm.module_id = m.id
where c.auth_user_id = $1 and c.oauth_client_id = $2
order by c.status = 'active' desc, c.updated_at desc, c.created_at desc
limit 1`

// FindMCPAccess returns the authorization state for userID and the OAuth clientID,
// or tenant.ErrConnectionNotFound when no connection exists.
func (s *MCPAccessStore) FindMCPAccess(ctx context.Context, userID, clientID string) (tenant.AccessRecord, error) {
	var r tenant.AccessRecord

	err := s.db.QueryRow(ctx, findMCPAccessSQL, userID, clientID, tenant.MCPModuleSlug).Scan(
		&r.ConnectionID,
		&r.ConnectionActive,
		&r.Scopes,
		&r.TenantID,
		&r.TenantSlug,
		&r.TenantActive,
		&r.MemberFound,
		&r.MemberActive,
		&r.Role,
		&r.ModuleEnabled,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return tenant.AccessRecord{}, tenant.ErrConnectionNotFound
	}

	if err != nil {
		return tenant.AccessRecord{}, fmt.Errorf("query mcp access: %w", err)
	}

	// A member row with is_active = true is still inactive if it does not exist.
	r.MemberActive = r.MemberFound && r.MemberActive

	return r, nil
}
