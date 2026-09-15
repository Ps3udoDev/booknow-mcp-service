package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/auth"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/mcpserver"
	"github.com/Ps3udoDev/booknow-mcp-service/internal/tenant"
)

const (
	mcpPath                = "/mcp"
	protectedResourcePath  = "/.well-known/oauth-protected-resource"
	maxMCPBodyBytes        = 1 << 20
	corsPreflightMaxAgeSec = "600"
)

// JSON-RPC error codes returned before a request reaches the MCP server (parity with the Next.js MCP).
const (
	rpcCodeUnauthenticated = -32001
	rpcCodeAccessDenied    = -32002
	rpcCodeForbidden       = -32000
	rpcCodeInternal        = -32603
)

// mcpScopesSupported mirrors what Supabase Auth issues; per-tool domain scopes are not supported by Supabase yet.
var mcpScopesSupported = []string{"openid", "profile", "email", "offline_access"}

// TokenVerifier authenticates a bearer token.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (auth.Identity, error)
}

// AccessResolver authorizes an authenticated caller for a tenant.
type AccessResolver interface {
	Resolve(ctx context.Context, id auth.Identity) (tenant.Access, error)
}

// MCPConfig wires the /mcp endpoint.
type MCPConfig struct {
	// PublicURL is the public origin of this service; the resource identifier is PublicURL + /mcp.
	PublicURL string
	// AuthorizationServer is the OAuth issuer that clients must use (Supabase Auth).
	AuthorizationServer string
	// AllowedOrigins are the browser origins allowed to call /mcp.
	AllowedOrigins []string
	Tokens         TokenVerifier
	Access         AccessResolver
}

func mountMCP(r chi.Router, logger *slog.Logger, cfg MCPConfig) {
	metadataURL := cfg.PublicURL + protectedResourcePath + mcpPath

	metadata := sdkauth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               cfg.PublicURL + mcpPath,
		AuthorizationServers:   []string{cfg.AuthorizationServer},
		ScopesSupported:        mcpScopesSupported,
		BearerMethodsSupported: []string{"header"},
	})
	// RFC 9728 path-suffixed location, plus the root location some clients probe first.
	r.Handle(protectedResourcePath+mcpPath, metadata)
	r.Handle(protectedResourcePath, metadata)

	// Stateless with JSON responses: any instance can serve any request (Cloud Run scales horizontally)
	// and no long-lived SSE stream is held open, so the server's regular timeouts apply safely.
	streamable := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		access, ok := accessFromContext(req.Context())
		if !ok {
			return nil
		}

		return mcpserver.New(access)
	}, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		Logger:              logger,
		MaxRequestBodyBytes: maxMCPBodyBytes,
	})

	r.Handle(mcpPath, originGuard(cfg.AllowedOrigins)(requireMCPAccess(logger, cfg, metadataURL)(streamable)))
}

// originGuard rejects browser requests from origins outside the allowlist before any authentication,
// as required by the MCP transport spec against DNS rebinding and cross-site calls.
// Requests without Origin (native MCP clients) pass through.
func originGuard(allowed []string) func(http.Handler) http.Handler {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		allowedSet[strings.ToLower(o)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)

				return
			}

			if _, ok := allowedSet[strings.ToLower(origin)]; !ok {
				writeRPCError(w, http.StatusForbidden, rpcCodeForbidden, "Origin not allowed.", "ORIGIN_NOT_ALLOWED")

				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id, MCP-Protocol-Version")

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, MCP-Protocol-Version, Mcp-Session-Id, Last-Event-ID")
				h.Set("Access-Control-Max-Age", corsPreflightMaxAgeSec)
				w.WriteHeader(http.StatusNoContent)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// requireMCPAccess authenticates the bearer token and resolves tenant access on every request.
// The resolved access travels in the request context; tools never receive the tenant as input.
func requireMCPAccess(logger *slog.Logger, cfg MCPConfig, metadataURL string) func(http.Handler) http.Handler {
	challenge := `resource_metadata="` + metadataURL + `"`

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			log := logger.With(slog.String("request_id", middleware.GetReqID(ctx)))

			token, err := auth.BearerToken(r)
			if err != nil {
				w.Header().Set("WWW-Authenticate", "Bearer "+challenge)
				writeRPCError(w, http.StatusUnauthorized, rpcCodeUnauthenticated, "Authentication required.", "")

				return
			}

			identity, err := cfg.Tokens.Verify(ctx, token)
			if err != nil {
				log.InfoContext(ctx, "mcp token rejected", slog.Any("error", err))
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", `+challenge)
				writeRPCError(w, http.StatusUnauthorized, rpcCodeUnauthenticated, "Invalid or expired access token.", "")

				return
			}

			access, err := cfg.Access.Resolve(ctx, identity)
			if err != nil {
				if denial, ok := denialFor(err); ok {
					log.WarnContext(ctx, "mcp access denied", slog.String("error_code", denial.code))
					writeRPCError(w, http.StatusForbidden, rpcCodeAccessDenied, denial.message, denial.code)

					return
				}

				log.ErrorContext(ctx, "mcp access resolution failed", slog.Any("error", err))
				writeRPCError(w, http.StatusInternalServerError, rpcCodeInternal, "Internal error.", "")

				return
			}

			next.ServeHTTP(w, r.WithContext(withAccess(ctx, access)))
		})
	}
}

type denial struct {
	err     error
	code    string
	message string
}

// denials maps authorization failures to stable codes and messages shown to MCP clients.
var denials = []denial{
	{tenant.ErrClientRequired, "MCP_CLIENT_REQUIRED", "El token no está vinculado a un cliente OAuth. Conecta el cliente MCP mediante el flujo de consentimiento."},
	{tenant.ErrConnectionNotFound, "MCP_CONNECTION_NOT_FOUND", "No existe una conexión MCP autorizada para este usuario y cliente. Completa el flujo de consentimiento."},
	{tenant.ErrConnectionRevoked, "MCP_CONNECTION_REVOKED", "Esta conexión MCP ha sido revocada."},
	{tenant.ErrTenantInactive, "TENANT_INACTIVE", "El negocio asociado a la conexión no está activo."},
	{tenant.ErrMembershipInactive, "MEMBER_INACTIVE", "El usuario no tiene una membresía activa en este negocio."},
	{tenant.ErrRoleForbidden, "ROLE_FORBIDDEN", "El rol del usuario no permite usar el MCP (roles permitidos: owner, admin, manager)."},
	{tenant.ErrModuleDisabled, "MODULE_DISABLED", "El módulo BookNow Business MCP no está habilitado para este negocio."},
	{tenant.ErrAccessDenied, "ACCESS_DENIED", "Acceso denegado."},
}

func denialFor(err error) (denial, bool) {
	for _, d := range denials {
		if errors.Is(err, d.err) {
			return d, true
		}
	}

	return denial{}, false
}

// writeRPCError writes a JSON-RPC error without id, as the MCP transport allows for HTTP-level rejections.
func writeRPCError(w http.ResponseWriter, status, code int, message, errorCode string) {
	type rpcErrorData struct {
		ErrorCode string `json:"errorCode"`
	}

	type rpcError struct {
		Code    int           `json:"code"`
		Message string        `json:"message"`
		Data    *rpcErrorData `json:"data,omitempty"`
	}

	body := struct {
		JSONRPC string   `json:"jsonrpc"`
		ID      any      `json:"id"`
		Error   rpcError `json:"error"`
	}{JSONRPC: "2.0", Error: rpcError{Code: code, Message: message}}

	if errorCode != "" {
		body.Error.Data = &rpcErrorData{ErrorCode: errorCode}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type accessContextKey struct{}

func withAccess(ctx context.Context, access tenant.Access) context.Context {
	return context.WithValue(ctx, accessContextKey{}, access)
}

func accessFromContext(ctx context.Context) (tenant.Access, bool) {
	access, ok := ctx.Value(accessContextKey{}).(tenant.Access)

	return access, ok
}
