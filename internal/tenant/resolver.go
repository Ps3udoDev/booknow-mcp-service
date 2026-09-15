// Package tenant resolves the tenant and role an authenticated MCP caller acts with.
// The tenant comes only from an active mcp_connections row for the token's (user, OAuth client) pair:
// never from tool parameters, request bodies or token claims.
package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/auth"
)

// MCPModuleSlug is the tenant add-on that must be enabled to use the MCP server.
const MCPModuleSlug = "business-mcp"

// Role is a tenant_users role allowed to use the MCP server.
type Role string

// Management roles; any other tenant role (e.g. employee) is denied.
const (
	RoleOwner   Role = "owner"
	RoleAdmin   Role = "admin"
	RoleManager Role = "manager"
)

// ErrAccessDenied is wrapped by every authorization failure; callers map it to 403.
// Errors that do not wrap it (database failures) must be treated as internal errors.
var ErrAccessDenied = errors.New("mcp access denied")

// Reasons for denial. All of them wrap ErrAccessDenied.
var (
	ErrClientRequired     = fmt.Errorf("%w: token is not bound to an oauth client", ErrAccessDenied)
	ErrConnectionNotFound = fmt.Errorf("%w: no mcp connection for this user and client", ErrAccessDenied)
	ErrConnectionRevoked  = fmt.Errorf("%w: mcp connection revoked", ErrAccessDenied)
	ErrTenantInactive     = fmt.Errorf("%w: tenant is not active", ErrAccessDenied)
	ErrMembershipInactive = fmt.Errorf("%w: user has no active membership in the tenant", ErrAccessDenied)
	ErrRoleForbidden      = fmt.Errorf("%w: role not allowed", ErrAccessDenied)
	ErrModuleDisabled     = fmt.Errorf("%w: mcp module disabled for the tenant", ErrAccessDenied)
)

// Access is the authorized context of an MCP request.
type Access struct {
	UserID       string
	ClientID     string
	ConnectionID string
	TenantID     string
	TenantSlug   string
	Role         Role
	// Scopes are the ones granted at consent time and stored on the connection.
	Scopes []string
}

// AccessRecord is the authorization state stored for a (user, client) pair.
// When several connections exist, the store returns the active one consented most recently.
type AccessRecord struct {
	ConnectionID     string
	ConnectionActive bool
	Scopes           []string
	TenantID         string
	TenantSlug       string
	TenantActive     bool
	MemberFound      bool
	MemberActive     bool
	Role             string
	ModuleEnabled    bool
}

// AccessStore reads authorization state. It returns ErrConnectionNotFound when the pair has no connection.
type AccessStore interface {
	FindMCPAccess(ctx context.Context, userID, clientID string) (AccessRecord, error)
}

// Resolver authorizes MCP callers. It reads fresh state on every call, so revocations apply immediately.
type Resolver struct {
	store AccessStore
}

// NewResolver returns a Resolver backed by store.
func NewResolver(store AccessStore) *Resolver {
	return &Resolver{store: store}
}

// Resolve returns the caller's tenant access or an error wrapping ErrAccessDenied.
func (r *Resolver) Resolve(ctx context.Context, id auth.Identity) (Access, error) {
	if id.ClientID == "" {
		return Access{}, ErrClientRequired
	}

	record, err := r.store.FindMCPAccess(ctx, id.UserID, id.ClientID)
	if err != nil {
		if errors.Is(err, ErrAccessDenied) {
			return Access{}, err
		}

		return Access{}, fmt.Errorf("find mcp access: %w", err)
	}

	role := Role(record.Role)

	switch {
	case !record.ConnectionActive:
		return Access{}, ErrConnectionRevoked
	case !record.TenantActive:
		return Access{}, ErrTenantInactive
	case !record.MemberFound || !record.MemberActive:
		return Access{}, ErrMembershipInactive
	case role != RoleOwner && role != RoleAdmin && role != RoleManager:
		return Access{}, ErrRoleForbidden
	case !record.ModuleEnabled:
		return Access{}, ErrModuleDisabled
	}

	return Access{
		UserID:       id.UserID,
		ClientID:     id.ClientID,
		ConnectionID: record.ConnectionID,
		TenantID:     record.TenantID,
		TenantSlug:   record.TenantSlug,
		Role:         role,
		Scopes:       record.Scopes,
	}, nil
}
