package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/auth"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

const (
	testPublicURL      = "https://mcp.example.test"
	testIssuer         = "https://abc.supabase.co/auth/v1"
	testAllowedOrigin  = "https://allowed.example"
	testMetadataURL    = testPublicURL + "/.well-known/oauth-protected-resource/mcp"
	secretInternalText = "pgx: connection refused to 10.0.0.5"
	verifierDetail     = "unknown kid key-7f3a in jwks"
)

// fakeVerifier accepts only the tokens it knows.
type fakeVerifier struct {
	mu     sync.Mutex
	tokens map[string]auth.Identity
	calls  []string
}

func (v *fakeVerifier) Verify(_ context.Context, token string) (auth.Identity, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.calls = append(v.calls, token)

	id, ok := v.tokens[token]
	if !ok {
		return auth.Identity{}, fmt.Errorf("%w: %s", auth.ErrInvalidToken, verifierDetail)
	}

	return id, nil
}

func (v *fakeVerifier) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()

	return len(v.calls)
}

// fakeResolver returns a fixed outcome per user.
type fakeResolver struct {
	access map[string]tenant.Access
	errs   map[string]error
}

func (r *fakeResolver) Resolve(_ context.Context, id auth.Identity) (tenant.Access, error) {
	if err, ok := r.errs[id.UserID]; ok {
		return tenant.Access{}, err
	}

	access, ok := r.access[id.UserID]
	if !ok {
		return tenant.Access{}, tenant.ErrConnectionNotFound
	}

	access.UserID, access.ClientID = id.UserID, id.ClientID

	return access, nil
}

type fakePingerOK struct{}

func (fakePingerOK) Ping(context.Context) error { return nil }

func newMCPTestServer(t *testing.T, verifier TokenVerifier, resolver AccessResolver) *httptest.Server {
	t.Helper()

	router := NewRouter(slog.New(slog.DiscardHandler), Deps{
		DB: fakePingerOK{},
		MCP: MCPConfig{
			PublicURL:           testPublicURL,
			AuthorizationServer: testIssuer,
			AllowedOrigins:      []string{testAllowedOrigin},
			Tokens:              verifier,
			Access:              resolver,
		},
	})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	return srv
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`

// mcpResponse is a fully read HTTP response.
type mcpResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func doMCP(t *testing.T, method, url string, headers map[string]string, body string) mcpResponse {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)

			continue
		}

		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return mcpResponse{StatusCode: resp.StatusCode, Header: resp.Header, Body: raw}
}

type jsonRPCErrorBody struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Error   struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			ErrorCode string `json:"errorCode"`
		} `json:"data"`
	} `json:"error"`
}

func decodeRPCError(t *testing.T, resp mcpResponse) (jsonRPCErrorBody, string) {
	t.Helper()

	raw := resp.Body

	var body jsonRPCErrorBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body is not JSON-RPC: %v (%s)", err, raw)
	}

	if body.JSONRPC != "2.0" || body.ID != nil {
		t.Errorf("body = %s, want jsonrpc 2.0 with null id", raw)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	return body, string(raw)
}

func TestMCPAuthentication(t *testing.T) {
	t.Parallel()

	verifier := &fakeVerifier{tokens: map[string]auth.Identity{}}
	srv := newMCPTestServer(t, verifier, &fakeResolver{})

	tests := []struct {
		name          string
		authorization string
		wantChallenge string
	}{
		{
			name:          "missing token",
			wantChallenge: `Bearer resource_metadata="` + testMetadataURL + `"`,
		},
		{
			name:          "malformed authorization header",
			authorization: "Basic dXNlcjpwYXNz",
			wantChallenge: `Bearer resource_metadata="` + testMetadataURL + `"`,
		},
		{
			name:          "invalid token",
			authorization: "Bearer not-a-known-token",
			wantChallenge: `Bearer error="invalid_token", resource_metadata="` + testMetadataURL + `"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := doMCP(t, http.MethodPost, srv.URL+"/mcp", map[string]string{"Authorization": tt.authorization}, initializeBody)

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}

			if got := resp.Header.Get("WWW-Authenticate"); got != tt.wantChallenge {
				t.Errorf("WWW-Authenticate = %q, want %q", got, tt.wantChallenge)
			}

			body, raw := decodeRPCError(t, resp)
			if body.Error.Code != -32001 {
				t.Errorf("error code = %d, want -32001", body.Error.Code)
			}

			if strings.Contains(raw, "key-7f3a") {
				t.Errorf("body leaks verifier details: %s", raw)
			}
		})
	}
}

func TestMCPAccessDenied(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err           error
		wantErrorCode string
	}{
		{err: tenant.ErrClientRequired, wantErrorCode: "MCP_CLIENT_REQUIRED"},
		{err: tenant.ErrConnectionNotFound, wantErrorCode: "MCP_CONNECTION_NOT_FOUND"},
		{err: tenant.ErrConnectionRevoked, wantErrorCode: "MCP_CONNECTION_REVOKED"},
		{err: tenant.ErrTenantInactive, wantErrorCode: "TENANT_INACTIVE"},
		{err: tenant.ErrMembershipInactive, wantErrorCode: "MEMBER_INACTIVE"},
		{err: tenant.ErrRoleForbidden, wantErrorCode: "ROLE_FORBIDDEN"},
		{err: tenant.ErrModuleDisabled, wantErrorCode: "MODULE_DISABLED"},
		{err: fmt.Errorf("find mcp access: %w", tenant.ErrModuleDisabled), wantErrorCode: "MODULE_DISABLED"},
	}

	for _, tt := range tests {
		t.Run(tt.wantErrorCode, func(t *testing.T) {
			t.Parallel()

			verifier := &fakeVerifier{tokens: map[string]auth.Identity{"tok": {UserID: "u1", ClientID: "c1"}}}
			srv := newMCPTestServer(t, verifier, &fakeResolver{errs: map[string]error{"u1": tt.err}})

			resp := doMCP(t, http.MethodPost, srv.URL+"/mcp", map[string]string{"Authorization": "Bearer tok"}, initializeBody)

			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}

			body, _ := decodeRPCError(t, resp)
			if body.Error.Code != -32002 {
				t.Errorf("error code = %d, want -32002", body.Error.Code)
			}

			if body.Error.Data.ErrorCode != tt.wantErrorCode {
				t.Errorf("errorCode = %q, want %q", body.Error.Data.ErrorCode, tt.wantErrorCode)
			}
		})
	}
}

func TestMCPResolverFailureIsInternalError(t *testing.T) {
	t.Parallel()

	verifier := &fakeVerifier{tokens: map[string]auth.Identity{"tok": {UserID: "u1", ClientID: "c1"}}}
	srv := newMCPTestServer(t, verifier, &fakeResolver{errs: map[string]error{"u1": errors.New(secretInternalText)}})

	resp := doMCP(t, http.MethodPost, srv.URL+"/mcp", map[string]string{"Authorization": "Bearer tok"}, initializeBody)

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}

	body, raw := decodeRPCError(t, resp)
	if body.Error.Code != -32603 {
		t.Errorf("error code = %d, want -32603", body.Error.Code)
	}

	if strings.Contains(raw, "10.0.0.5") {
		t.Errorf("body leaks internal error: %s", raw)
	}
}

// headerTransport adds a bearer token to every request made by the SDK client.
type headerTransport struct {
	token string
}

func (h headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+h.token)

	return http.DefaultTransport.RoundTrip(req)
}

func TestMCPServesToolsWithEachCallersAccess(t *testing.T) {
	t.Parallel()

	verifier := &fakeVerifier{tokens: map[string]auth.Identity{
		"token-a": {UserID: "user-a", ClientID: "client-a"},
		"token-b": {UserID: "user-b", ClientID: "client-b"},
	}}
	resolver := &fakeResolver{access: map[string]tenant.Access{
		"user-a": {TenantSlug: "tenant-a", Role: tenant.RoleOwner},
		"user-b": {TenantSlug: "tenant-b", Role: tenant.RoleManager},
	}}
	srv := newMCPTestServer(t, verifier, resolver)

	for _, tc := range []struct{ token, wantTenant, wantRole string }{
		{token: "token-a", wantTenant: "tenant-a", wantRole: "owner"},
		{token: "token-b", wantTenant: "tenant-b", wantRole: "manager"},
		{token: "token-a", wantTenant: "tenant-a", wantRole: "owner"},
	} {
		client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)

		session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
			Endpoint:             srv.URL + "/mcp",
			HTTPClient:           &http.Client{Transport: headerTransport{token: tc.token}},
			DisableStandaloneSSE: true,
			MaxRetries:           -1,
		}, nil)
		if err != nil {
			t.Fatalf("Connect(%s) error = %v", tc.token, err)
		}

		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "health"})
		_ = session.Close()

		if err != nil {
			t.Fatalf("CallTool(%s) error = %v", tc.token, err)
		}

		raw, _ := json.Marshal(res.StructuredContent)

		var out struct{ Tenant, Role string }
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decode health output: %v", err)
		}

		if out.Tenant != tc.wantTenant || out.Role != tc.wantRole {
			t.Errorf("health with %s = %+v, want tenant %s role %s", tc.token, out, tc.wantTenant, tc.wantRole)
		}
	}
}

func TestMCPOriginValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		origin       string
		wantStatus   int
		wantACAO     string
		wantVerified bool
	}{
		{name: "no origin (non-browser client)", wantStatus: http.StatusUnauthorized, wantVerified: true},
		{name: "allowed origin", origin: testAllowedOrigin, wantStatus: http.StatusUnauthorized, wantACAO: testAllowedOrigin, wantVerified: true},
		{name: "allowed origin in different case", origin: "https://ALLOWED.example", wantStatus: http.StatusUnauthorized, wantACAO: "https://ALLOWED.example", wantVerified: true},
		{name: "foreign origin", origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "null origin", origin: "null", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			verifier := &fakeVerifier{tokens: map[string]auth.Identity{}}
			srv := newMCPTestServer(t, verifier, &fakeResolver{})

			resp := doMCP(t, http.MethodPost, srv.URL+"/mcp", map[string]string{
				"Origin": tt.origin, "Authorization": "Bearer unknown",
			}, initializeBody)

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if got := resp.Header.Get("Access-Control-Allow-Origin"); got != tt.wantACAO {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantACAO)
			}

			if verified := verifier.callCount() > 0; verified != tt.wantVerified {
				t.Errorf("token verified = %v, want %v (foreign origins must be rejected before auth)", verified, tt.wantVerified)
			}
		})
	}
}

func TestMCPPreflight(t *testing.T) {
	t.Parallel()

	srv := newMCPTestServer(t, &fakeVerifier{}, &fakeResolver{})

	allowed := doMCP(t, http.MethodOptions, srv.URL+"/mcp", map[string]string{
		"Origin": testAllowedOrigin, "Access-Control-Request-Method": http.MethodPost,
		"Access-Control-Request-Headers": "authorization, content-type, mcp-protocol-version",
	}, "")

	if allowed.StatusCode != http.StatusNoContent {
		t.Fatalf("allowed preflight status = %d, want 204", allowed.StatusCode)
	}

	if got := allowed.Header.Get("Access-Control-Allow-Origin"); got != testAllowedOrigin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, testAllowedOrigin)
	}

	if methods := allowed.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(methods, http.MethodPost) {
		t.Errorf("Access-Control-Allow-Methods = %q, want POST", methods)
	}

	allowHeaders := strings.ToLower(allowed.Header.Get("Access-Control-Allow-Headers"))
	for _, h := range []string{"authorization", "content-type", "mcp-protocol-version"} {
		if !strings.Contains(allowHeaders, h) {
			t.Errorf("Access-Control-Allow-Headers = %q, missing %q", allowHeaders, h)
		}
	}

	if exposed := strings.ToLower(allowed.Header.Get("Access-Control-Expose-Headers")); !strings.Contains(exposed, "www-authenticate") {
		t.Errorf("Access-Control-Expose-Headers = %q, want www-authenticate for OAuth discovery", exposed)
	}

	foreign := doMCP(t, http.MethodOptions, srv.URL+"/mcp", map[string]string{
		"Origin": "https://evil.example", "Access-Control-Request-Method": http.MethodPost,
	}, "")

	if foreign.StatusCode != http.StatusForbidden {
		t.Errorf("foreign preflight status = %d, want 403", foreign.StatusCode)
	}
}

func TestMCPTransportRules(t *testing.T) {
	t.Parallel()

	verifier := &fakeVerifier{tokens: map[string]auth.Identity{"tok": {UserID: "u1", ClientID: "c1"}}}
	resolver := &fakeResolver{access: map[string]tenant.Access{"u1": {TenantSlug: "t", Role: tenant.RoleOwner}}}
	srv := newMCPTestServer(t, verifier, resolver)

	authz := map[string]string{"Authorization": "Bearer tok"}
	withHeaders := func(extra map[string]string) map[string]string {
		h := map[string]string{"Authorization": "Bearer tok"}
		maps.Copy(h, extra)

		return h
	}

	tests := []struct {
		name       string
		method     string
		headers    map[string]string
		body       string
		wantStatus int
	}{
		{name: "initialize succeeds", method: http.MethodPost, headers: authz, body: initializeBody, wantStatus: http.StatusOK},
		{name: "GET stream not offered", method: http.MethodGet, headers: authz, wantStatus: http.StatusMethodNotAllowed},
		{name: "DELETE session not offered", method: http.MethodDelete, headers: authz, wantStatus: http.StatusMethodNotAllowed},
		{name: "Accept without event-stream", method: http.MethodPost, headers: withHeaders(map[string]string{"Accept": "application/json"}), body: initializeBody, wantStatus: http.StatusBadRequest},
		{name: "wrong content type", method: http.MethodPost, headers: withHeaders(map[string]string{"Content-Type": "text/plain"}), body: initializeBody, wantStatus: http.StatusUnsupportedMediaType},
		{name: "body over limit", method: http.MethodPost, headers: authz, body: `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"` + strings.Repeat("a", maxMCPBodyBytes) + `"}}`, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "notification accepted without body", method: http.MethodPost, headers: authz, body: `{"jsonrpc":"2.0","method":"notifications/initialized"}`, wantStatus: http.StatusAccepted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := doMCP(t, tt.method, srv.URL+"/mcp", tt.headers, tt.body)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, tt.wantStatus, resp.Body)
			}
		})
	}
}

func TestProtectedResourceMetadata(t *testing.T) {
	t.Parallel()

	srv := newMCPTestServer(t, &fakeVerifier{}, &fakeResolver{})

	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		resp := doMCP(t, http.MethodGet, srv.URL+path, nil, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, resp.StatusCode)
		}

		var meta struct {
			Resource               string   `json:"resource"`
			AuthorizationServers   []string `json:"authorization_servers"`
			BearerMethodsSupported []string `json:"bearer_methods_supported"`
			ScopesSupported        []string `json:"scopes_supported"`
		}

		if err := json.Unmarshal(resp.Body, &meta); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}

		if meta.Resource != testPublicURL+"/mcp" {
			t.Errorf("%s resource = %q, want %q", path, meta.Resource, testPublicURL+"/mcp")
		}

		if !slices.Equal(meta.AuthorizationServers, []string{testIssuer}) {
			t.Errorf("%s authorization_servers = %v, want [%s]", path, meta.AuthorizationServers, testIssuer)
		}

		if !slices.Equal(meta.BearerMethodsSupported, []string{"header"}) {
			t.Errorf("%s bearer_methods_supported = %v, want [header]", path, meta.BearerMethodsSupported)
		}

		if !slices.Contains(meta.ScopesSupported, "openid") {
			t.Errorf("%s scopes_supported = %v, want openid", path, meta.ScopesSupported)
		}
	}
}
