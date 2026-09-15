package tenant

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/auth"
)

const (
	testUserID   = "7170ce3e-5b1d-4747-9d6b-90ebfba7eada"
	testClientID = "9a8b7c6d-5e4f-3a2b-1c0d-9e8f7a6b5c4d"
)

type fakeStore struct {
	record AccessRecord
	err    error

	gotUserID, gotClientID string
}

func (s *fakeStore) FindMCPAccess(_ context.Context, userID, clientID string) (AccessRecord, error) {
	s.gotUserID, s.gotClientID = userID, clientID

	return s.record, s.err
}

func grantedRecord() AccessRecord {
	return AccessRecord{
		ConnectionID:     "c0000000-0000-0000-0000-000000000001",
		ConnectionActive: true,
		Scopes:           []string{"appointments:read"},
		TenantID:         "t0000000-0000-0000-0000-000000000001",
		TenantSlug:       "elvis-studio",
		TenantActive:     true,
		MemberFound:      true,
		MemberActive:     true,
		Role:             "manager",
		ModuleEnabled:    true,
	}
}

func TestResolverGrantsAccess(t *testing.T) {
	t.Parallel()

	store := &fakeStore{record: grantedRecord()}
	identity := auth.Identity{UserID: testUserID, ClientID: testClientID}

	got, err := NewResolver(store).Resolve(t.Context(), identity)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if store.gotUserID != testUserID || store.gotClientID != testClientID {
		t.Errorf("store queried with (%q, %q), want (%q, %q)", store.gotUserID, store.gotClientID, testUserID, testClientID)
	}

	want := Access{
		UserID:       testUserID,
		ClientID:     testClientID,
		ConnectionID: "c0000000-0000-0000-0000-000000000001",
		TenantID:     "t0000000-0000-0000-0000-000000000001",
		TenantSlug:   "elvis-studio",
		Role:         RoleManager,
		Scopes:       []string{"appointments:read"},
	}

	if got.UserID != want.UserID || got.ClientID != want.ClientID || got.ConnectionID != want.ConnectionID ||
		got.TenantID != want.TenantID || got.TenantSlug != want.TenantSlug || got.Role != want.Role ||
		!slices.Equal(got.Scopes, want.Scopes) {
		t.Errorf("Resolve() = %+v, want %+v", got, want)
	}
}

func TestResolverAllowsOnlyManagementRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role    string
		wantErr error
	}{
		{role: "owner"},
		{role: "admin"},
		{role: "manager"},
		{role: "employee", wantErr: ErrRoleForbidden},
		{role: "", wantErr: ErrRoleForbidden},
		{role: "OWNER", wantErr: ErrRoleForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			t.Parallel()

			record := grantedRecord()
			record.Role = tt.role

			got, err := NewResolver(&fakeStore{record: record}).Resolve(t.Context(), auth.Identity{UserID: testUserID, ClientID: testClientID})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Resolve() error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr == nil && string(got.Role) != tt.role {
				t.Errorf("Role = %q, want %q", got.Role, tt.role)
			}
		})
	}
}

func TestResolverDeniesAccess(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("connection reset")

	tests := []struct {
		name     string
		identity auth.Identity
		mutate   func(*AccessRecord)
		storeErr error
		wantErr  error
		// wantDenied is false for infrastructure failures, which must not be reported as a 403.
		wantDenied bool
	}{
		{
			name:     "token without oauth client",
			identity: auth.Identity{UserID: testUserID},
			wantErr:  ErrClientRequired, wantDenied: true,
		},
		{
			name:     "no connection for user and client",
			storeErr: ErrConnectionNotFound,
			wantErr:  ErrConnectionNotFound, wantDenied: true,
		},
		{
			name:    "connection revoked",
			mutate:  func(r *AccessRecord) { r.ConnectionActive = false },
			wantErr: ErrConnectionRevoked, wantDenied: true,
		},
		{
			name:    "tenant not active",
			mutate:  func(r *AccessRecord) { r.TenantActive = false },
			wantErr: ErrTenantInactive, wantDenied: true,
		},
		{
			name:    "not a member of the tenant",
			mutate:  func(r *AccessRecord) { r.MemberFound = false },
			wantErr: ErrMembershipInactive, wantDenied: true,
		},
		{
			name:    "membership deactivated",
			mutate:  func(r *AccessRecord) { r.MemberActive = false },
			wantErr: ErrMembershipInactive, wantDenied: true,
		},
		{
			name:    "role degraded",
			mutate:  func(r *AccessRecord) { r.Role = "employee" },
			wantErr: ErrRoleForbidden, wantDenied: true,
		},
		{
			name:    "mcp module disabled",
			mutate:  func(r *AccessRecord) { r.ModuleEnabled = false },
			wantErr: ErrModuleDisabled, wantDenied: true,
		},
		{
			name:    "revocation checked before everything else",
			mutate:  func(r *AccessRecord) { *r = AccessRecord{ConnectionID: r.ConnectionID} },
			wantErr: ErrConnectionRevoked, wantDenied: true,
		},
		{
			name:     "database failure",
			storeErr: storeErr,
			wantErr:  storeErr, wantDenied: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			record := grantedRecord()
			if tt.mutate != nil {
				tt.mutate(&record)
			}

			identity := tt.identity
			if identity == (auth.Identity{}) {
				identity = auth.Identity{UserID: testUserID, ClientID: testClientID}
			}

			got, err := NewResolver(&fakeStore{record: record, err: tt.storeErr}).Resolve(t.Context(), identity)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Resolve() error = %v, want %v", err, tt.wantErr)
			}

			if denied := errors.Is(err, ErrAccessDenied); denied != tt.wantDenied {
				t.Errorf("errors.Is(err, ErrAccessDenied) = %v, want %v", denied, tt.wantDenied)
			}

			if got.TenantID != "" {
				t.Errorf("Resolve() returned tenant %q alongside an error", got.TenantID)
			}
		})
	}
}
