package postgres

import (
	"crypto/rand"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/auth"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

const (
	testClientID  = "9a8b7c6d-5e4f-3a2b-1c0d-9e8f7a6b5c4d"
	otherClientID = "11111111-2222-3333-4444-555555555555"
)

// integrationPool connects to TEST_DATABASE_URL or skips the test.
func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	pool, err := NewPool(t.Context(), url, 4)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

// seeder writes fixtures inside a transaction that is rolled back when the test ends.
type seeder struct {
	t  *testing.T
	tx pgx.Tx
}

func newSeeder(t *testing.T, pool *pgxpool.Pool) *seeder {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	t.Cleanup(func() { _ = tx.Rollback(t.Context()) })

	return &seeder{t: t, tx: tx}
}

// asServiceRole switches the rest of the transaction to the production least-privilege role,
// so store queries are checked against the same grants as in Supabase. Seed data before calling it.
// The local membership comes from supabase/roles.sql.
func (s *seeder) asServiceRole() {
	s.t.Helper()

	s.exec(`set local role booknow_mcp_service`)
}

func (s *seeder) scanID(sql string, args ...any) string {
	s.t.Helper()

	var id string
	if err := s.tx.QueryRow(s.t.Context(), sql, args...).Scan(&id); err != nil {
		s.t.Fatalf("seed %q: %v", strings.Fields(sql)[2], err)
	}

	return id
}

func (s *seeder) exec(sql string, args ...any) {
	s.t.Helper()

	if _, err := s.tx.Exec(s.t.Context(), sql, args...); err != nil {
		s.t.Fatalf("exec %q: %v", sql, err)
	}
}

func (s *seeder) user() string {
	s.t.Helper()

	return s.scanID(`insert into auth.users (id) values (gen_random_uuid()) returning id::text`)
}

func (s *seeder) tenant(status string) (id, slug string) {
	s.t.Helper()

	slug = "test-" + strings.ToLower(rand.Text())
	id = s.scanID(`insert into public.tenants (slug, name, status) values ($1, $1, $2::public.tenant_status) returning id::text`, slug, status)

	return id, slug
}

// member adds a tenant_users row; a nil active leaves is_active NULL.
func (s *seeder) member(tenantID, userID, role string, active *bool) {
	s.t.Helper()

	s.exec(`insert into public.tenant_users (tenant_id, auth_user_id, email, full_name, role, is_active)
		values ($1, $2, $3, 'Test User', $4::public.tenant_role, $5)`,
		tenantID, userID, strings.ToLower(rand.Text())+"@example.test", role, active)
}

func (s *seeder) module(tenantID string, enabled bool) {
	s.t.Helper()

	moduleID := s.scanID(`insert into public.modules (slug, name) values ($1, 'BookNow Business MCP')
		on conflict (slug) do update set slug = excluded.slug returning id::text`, tenant.MCPModuleSlug)
	s.exec(`insert into public.tenant_modules (tenant_id, module_id, is_enabled) values ($1, $2, $3)`, tenantID, moduleID, enabled)
}

func (s *seeder) connection(tenantID, userID, clientID, status string, updatedAt time.Time) string {
	s.t.Helper()

	return s.scanID(`insert into public.mcp_connections (tenant_id, auth_user_id, oauth_client_id, status, scopes, updated_at)
		values ($1, $2, $3, $4, array['appointments:read', 'customers:read'], $5) returning id::text`,
		tenantID, userID, clientID, status, updatedAt)
}

// grantedFixture seeds a user with full MCP access to one tenant.
func (s *seeder) grantedFixture() (userID, tenantID, slug, connectionID string) {
	s.t.Helper()

	userID = s.user()
	tenantID, slug = s.tenant("active")
	s.member(tenantID, userID, "owner", new(true))
	s.module(tenantID, true)
	connectionID = s.connection(tenantID, userID, testClientID, "active", time.Now())

	return userID, tenantID, slug, connectionID
}

func TestMCPAccessStoreIntegration(t *testing.T) {
	t.Parallel()

	pool := integrationPool(t)
	hourAgo := time.Now().Add(-time.Hour)

	tests := []struct {
		name string
		// setup seeds data and returns the user to query plus the expected record.
		setup   func(s *seeder) (userID, clientID string, want tenant.AccessRecord)
		wantErr error
	}{
		{
			name: "fully granted connection",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				userID, tenantID, slug, connID := s.grantedFixture()

				return userID, testClientID, tenant.AccessRecord{
					ConnectionID: connID, ConnectionActive: true, Scopes: []string{"appointments:read", "customers:read"},
					TenantID: tenantID, TenantSlug: slug, TenantActive: true,
					MemberFound: true, MemberActive: true, Role: "owner", ModuleEnabled: true,
				}
			},
		},
		{
			name: "user without connections",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				return s.user(), testClientID, tenant.AccessRecord{}
			},
			wantErr: tenant.ErrConnectionNotFound,
		},
		{
			name: "connection belongs to another oauth client",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				userID, _, _, _ := s.grantedFixture()

				return userID, otherClientID, tenant.AccessRecord{}
			},
			wantErr: tenant.ErrConnectionNotFound,
		},
		{
			name: "connection belongs to another user",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				s.grantedFixture()

				return s.user(), testClientID, tenant.AccessRecord{}
			},
			wantErr: tenant.ErrConnectionNotFound,
		},
		{
			name: "only a revoked connection",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				userID, tenantID, slug, connID := s.grantedFixture()
				s.exec(`update public.mcp_connections set status = 'revoked', revoked_at = now() where id = $1`, connID)

				return userID, testClientID, tenant.AccessRecord{
					ConnectionID: connID, ConnectionActive: false, Scopes: []string{"appointments:read", "customers:read"},
					TenantID: tenantID, TenantSlug: slug, TenantActive: true,
					MemberFound: true, MemberActive: true, Role: "owner", ModuleEnabled: true,
				}
			},
		},
		{
			name: "active connection wins over a newer revoked one",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				userID, tenantID, slug, _ := s.grantedFixture()
				s.exec(`update public.mcp_connections set updated_at = $2 where auth_user_id = $1`, userID, hourAgo)

				otherTenantID, _ := s.tenant("active")
				s.member(otherTenantID, userID, "owner", new(true))
				s.connection(otherTenantID, userID, testClientID, "revoked", time.Now())

				var connID string
				if err := s.tx.QueryRow(s.t.Context(), `select id::text from public.mcp_connections where tenant_id = $1`, tenantID).Scan(&connID); err != nil {
					s.t.Fatalf("lookup connection: %v", err)
				}

				return userID, testClientID, tenant.AccessRecord{
					ConnectionID: connID, ConnectionActive: true, Scopes: []string{"appointments:read", "customers:read"},
					TenantID: tenantID, TenantSlug: slug, TenantActive: true,
					MemberFound: true, MemberActive: true, Role: "owner", ModuleEnabled: true,
				}
			},
		},
		{
			name: "most recently consented active connection wins",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				userID, _, _, _ := s.grantedFixture()
				s.exec(`update public.mcp_connections set updated_at = $2 where auth_user_id = $1`, userID, hourAgo)

				newTenantID, newSlug := s.tenant("active")
				s.member(newTenantID, userID, "admin", new(true))
				s.module(newTenantID, true)
				connID := s.connection(newTenantID, userID, testClientID, "active", time.Now())

				return userID, testClientID, tenant.AccessRecord{
					ConnectionID: connID, ConnectionActive: true, Scopes: []string{"appointments:read", "customers:read"},
					TenantID: newTenantID, TenantSlug: newSlug, TenantActive: true,
					MemberFound: true, MemberActive: true, Role: "admin", ModuleEnabled: true,
				}
			},
		},
		{
			name: "membership in a different tenant does not count",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				userID := s.user()
				tenantID, slug := s.tenant("active")
				s.module(tenantID, true)
				connID := s.connection(tenantID, userID, testClientID, "active", time.Now())

				otherTenantID, _ := s.tenant("active")
				s.member(otherTenantID, userID, "owner", new(true))

				return userID, testClientID, tenant.AccessRecord{
					ConnectionID: connID, ConnectionActive: true, Scopes: []string{"appointments:read", "customers:read"},
					TenantID: tenantID, TenantSlug: slug, TenantActive: true,
					MemberFound: false, MemberActive: false, Role: "", ModuleEnabled: true,
				}
			},
		},
		{
			name: "deactivated membership, trial tenant and disabled module",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				userID := s.user()
				tenantID, slug := s.tenant("trial")
				s.member(tenantID, userID, "employee", new(false))
				s.module(tenantID, false)
				connID := s.connection(tenantID, userID, testClientID, "active", time.Now())

				return userID, testClientID, tenant.AccessRecord{
					ConnectionID: connID, ConnectionActive: true, Scopes: []string{"appointments:read", "customers:read"},
					TenantID: tenantID, TenantSlug: slug, TenantActive: false,
					MemberFound: true, MemberActive: false, Role: "employee", ModuleEnabled: false,
				}
			},
		},
		{
			name: "null is_active counts as active and missing module as disabled",
			setup: func(s *seeder) (string, string, tenant.AccessRecord) {
				userID := s.user()
				tenantID, slug := s.tenant("active")
				s.member(tenantID, userID, "manager", nil)
				connID := s.connection(tenantID, userID, testClientID, "active", time.Now())

				return userID, testClientID, tenant.AccessRecord{
					ConnectionID: connID, ConnectionActive: true, Scopes: []string{"appointments:read", "customers:read"},
					TenantID: tenantID, TenantSlug: slug, TenantActive: true,
					MemberFound: true, MemberActive: true, Role: "manager", ModuleEnabled: false,
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newSeeder(t, pool)
			userID, clientID, want := tt.setup(s)
			s.asServiceRole()

			got, err := NewMCPAccessStore(s.tx).FindMCPAccess(t.Context(), userID, clientID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("FindMCPAccess() error = %v, want %v", err, tt.wantErr)
			}

			if !equalRecords(got, want) {
				t.Errorf("FindMCPAccess() =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

// TestResolverIntegration checks that revocation and role changes cut access on the very next request.
func TestResolverIntegration(t *testing.T) {
	t.Parallel()

	pool := integrationPool(t)
	s := newSeeder(t, pool)
	userID, tenantID, _, connID := s.grantedFixture()

	resolver := tenant.NewResolver(NewMCPAccessStore(s.tx))
	identity := auth.Identity{UserID: userID, ClientID: testClientID}

	// Role and status changes below are admin writes, so this test keeps the postgres role.

	access, err := resolver.Resolve(t.Context(), identity)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if access.TenantID != tenantID || access.Role != tenant.RoleOwner {
		t.Fatalf("Resolve() = %+v, want tenant %s as owner", access, tenantID)
	}

	s.exec(`update public.tenant_users set role = 'employee' where auth_user_id = $1`, userID)

	if _, err := resolver.Resolve(t.Context(), identity); !errors.Is(err, tenant.ErrRoleForbidden) {
		t.Fatalf("Resolve() after role downgrade error = %v, want ErrRoleForbidden", err)
	}

	s.exec(`update public.tenant_users set role = 'manager' where auth_user_id = $1`, userID)
	s.exec(`update public.mcp_connections set status = 'revoked', revoked_at = now() where id = $1`, connID)

	if _, err := resolver.Resolve(t.Context(), identity); !errors.Is(err, tenant.ErrConnectionRevoked) {
		t.Fatalf("Resolve() after revocation error = %v, want ErrConnectionRevoked", err)
	}
}

func equalRecords(a, b tenant.AccessRecord) bool {
	return reflect.DeepEqual(a, b)
}

func TestRecordConnectionUseIntegration(t *testing.T) {
	t.Parallel()

	pool := integrationPool(t)
	s := newSeeder(t, pool)
	_, _, _, connID := s.grantedFixture()

	lastUsed := func() *time.Time {
		t.Helper()

		var ts *time.Time
		if err := s.tx.QueryRow(t.Context(), `select last_used_at from public.mcp_connections where id = $1`, connID).Scan(&ts); err != nil {
			t.Fatalf("read last_used_at: %v", err)
		}

		return ts
	}

	store := NewMCPAccessStore(s.tx)
	s.asServiceRole()

	if err := store.RecordConnectionUse(t.Context(), connID); err != nil {
		t.Fatalf("RecordConnectionUse() as service role error = %v", err)
	}

	first := lastUsed()
	if first == nil {
		t.Fatal("last_used_at = NULL after first use, want a timestamp")
	}

	s.exec(`reset role`)
	s.exec(`update public.mcp_connections set last_used_at = now() - interval '2 minutes' where id = $1`, connID)
	recent := lastUsed()
	s.asServiceRole()

	if err := store.RecordConnectionUse(t.Context(), connID); err != nil {
		t.Fatalf("second RecordConnectionUse() error = %v", err)
	}

	if got := lastUsed(); !got.Equal(*recent) {
		t.Errorf("last_used_at changed within the throttle window: %v -> %v", *recent, *got)
	}

	s.exec(`reset role`)
	s.exec(`update public.mcp_connections set last_used_at = now() - interval '10 minutes' where id = $1`, connID)
	stale := lastUsed()
	s.asServiceRole()

	if err := store.RecordConnectionUse(t.Context(), connID); err != nil {
		t.Fatalf("third RecordConnectionUse() error = %v", err)
	}

	if got := lastUsed(); !got.After(*stale) {
		t.Errorf("last_used_at = %v, want it refreshed after the throttle window (was %v)", *got, *stale)
	}
}
