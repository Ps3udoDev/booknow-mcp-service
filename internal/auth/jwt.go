// Package auth verifies Supabase Auth access tokens (session and OAuth 2.1) issued for BookNow users.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

// ErrInvalidToken wraps every verification failure; callers map it to 401 without exposing the reason.
var ErrInvalidToken = errors.New("invalid token")

const (
	// signingAlg is the only algorithm accepted; pinning it blocks alg=none and HS256/public-key confusion.
	signingAlg = "ES256"
	// authenticatedRole is the Supabase role of a signed-in user (not anon or service_role).
	authenticatedRole = "authenticated"
	clockSkewLeeway   = 30 * time.Second
	jwksHTTPTimeout   = 5 * time.Second
	// jwksUnknownKIDRefreshEvery rate-limits JWKS refetches triggered by tokens with an unknown kid.
	jwksUnknownKIDRefreshEvery = 5 * time.Minute
)

// Identity is the verified caller. Tenant and role are resolved later from the database, never from the token.
type Identity struct {
	UserID string
	// ClientID is the OAuth client that obtained the token; empty for direct Supabase sessions.
	ClientID  string
	ExpiresAt time.Time
}

// Verifier validates Supabase JWTs against the project's public JWKS.
type Verifier struct {
	keys   keyfunc.Keyfunc
	parser *jwt.Parser
}

type supabaseClaims struct {
	jwt.RegisteredClaims

	ClientID    string `json:"client_id"`
	Role        string `json:"role"`
	IsAnonymous bool   `json:"is_anonymous"`
}

// NewVerifier fetches the JWKS of the Supabase project at supabaseURL and returns a Verifier.
// It fails if the JWKS cannot be fetched, so a wrong SUPABASE_URL is caught at startup.
// ctx bounds the background JWKS refresh goroutine: pass the process lifetime context.
func NewVerifier(ctx context.Context, logger *slog.Logger, supabaseURL, audience string) (*Verifier, error) {
	issuer := IssuerURL(supabaseURL)
	jwksURL := issuer + "/.well-known/jwks.json"

	failOnFirstFetch := false

	keys, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{jwksURL}, keyfunc.Override{
		HTTPTimeout:               jwksHTTPTimeout,
		NoErrorReturnFirstHTTPReq: &failOnFirstFetch,
		// Also bounds the refresh request itself; a request never waits long for the rate limiter.
		RateLimitWaitMax:  jwksHTTPTimeout,
		RefreshUnknownKID: rate.NewLimiter(rate.Every(jwksUnknownKIDRefreshEvery), 1),
		RefreshErrorHandlerFunc: func(u string) func(context.Context, error) {
			return func(ctx context.Context, err error) {
				logger.WarnContext(ctx, "jwks refresh failed", slog.String("url", u), slog.Any("error", err))
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("load supabase jwks: %w", err)
	}

	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{signingAlg}),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockSkewLeeway),
	)

	return &Verifier{keys: keys, parser: parser}, nil
}

// IssuerURL returns the Supabase Auth issuer (the OAuth authorization server) for a project URL.
func IssuerURL(supabaseURL string) string {
	return strings.TrimRight(supabaseURL, "/") + "/auth/v1"
}

// Verify checks signature, algorithm, iss, aud, exp, nbf and iat, plus Supabase-specific claims.
func (v *Verifier) Verify(ctx context.Context, token string) (Identity, error) {
	var claims supabaseClaims

	if _, err := v.parser.ParseWithClaims(token, &claims, v.keyfunc(ctx)); err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}

	switch {
	case !isUUID(claims.Subject):
		return Identity{}, fmt.Errorf("%w: sub is not a user id", ErrInvalidToken)
	case claims.Role != authenticatedRole:
		return Identity{}, fmt.Errorf("%w: role %q is not allowed", ErrInvalidToken, claims.Role)
	case claims.IsAnonymous:
		return Identity{}, fmt.Errorf("%w: anonymous users are not allowed", ErrInvalidToken)
	}

	return Identity{
		UserID:    claims.Subject,
		ClientID:  claims.ClientID,
		ExpiresAt: claims.ExpiresAt.Time,
	}, nil
}

// keyfunc requires a kid so a token is checked against exactly one published key.
func (v *Verifier) keyfunc(ctx context.Context) jwt.Keyfunc {
	lookup := v.keys.KeyfuncCtx(ctx)

	return func(token *jwt.Token) (any, error) {
		if kid, _ := token.Header["kid"].(string); kid == "" {
			return nil, errors.New("missing kid header")
		}

		return lookup(token)
	}
}

// isUUID reports whether s is a canonical hyphenated UUID (Supabase user ids).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	for i := range len(s) {
		c := s[i]

		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHex(c) {
				return false
			}
		}
	}

	return true
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}
