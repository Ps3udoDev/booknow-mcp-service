package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAudience = "authenticated"
	testUserID   = "7170ce3e-5b1d-4747-9d6b-90ebfba7eada"
	testClientID = "9a8b7c6d-5e4f-3a2b-1c0d-9e8f7a6b5c4d"
)

type signingKey struct {
	kid  string
	priv *ecdsa.PrivateKey
}

func newSigningKey(t *testing.T, kid string) signingKey {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	return signingKey{kid: kid, priv: priv}
}

// fakeAuthServer serves a Supabase-like JWKS endpoint whose key set can change during a test.
type fakeAuthServer struct {
	*httptest.Server

	mu   sync.Mutex
	keys []signingKey
	// extra holds raw JWKs published verbatim, e.g. keys of other algorithms.
	extra []any
}

func newFakeAuthServer(t *testing.T, keys ...signingKey) *fakeAuthServer {
	t.Helper()

	s := &fakeAuthServer{keys: keys}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
			http.NotFound(w, r)

			return
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		type jwk struct {
			Kty string `json:"kty"`
			Crv string `json:"crv"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			Kid string `json:"kid"`
			X   string `json:"x"`
			Y   string `json:"y"`
		}

		set := struct {
			Keys []any `json:"keys"`
		}{Keys: append([]any(nil), s.extra...)}

		for _, k := range s.keys {
			ecdhKey, err := k.priv.PublicKey.ECDH()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)

				return
			}

			// Uncompressed point: 0x04 || X (32 bytes) || Y (32 bytes).
			raw := ecdhKey.Bytes()
			set.Keys = append(set.Keys, jwk{
				Kty: "EC", Crv: "P-256", Alg: "ES256", Use: "sig", Kid: k.kid,
				X: base64.RawURLEncoding.EncodeToString(raw[1:33]),
				Y: base64.RawURLEncoding.EncodeToString(raw[33:]),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *fakeAuthServer) addRawJWK(key any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.extra = append(s.extra, key)
}

func (s *fakeAuthServer) setKeys(keys ...signingKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.keys = keys
}

func (s *fakeAuthServer) issuer() string { return s.URL + "/auth/v1" }

// validClaims mirrors a Supabase OAuth access token.
func validClaims(issuer string) jwt.MapClaims {
	now := time.Now()

	return jwt.MapClaims{
		"iss":          issuer,
		"sub":          testUserID,
		"aud":          testAudience,
		"exp":          now.Add(time.Hour).Unix(),
		"iat":          now.Unix(),
		"role":         "authenticated",
		"is_anonymous": false,
		"client_id":    testClientID,
		"session_id":   "327e8d7c-e03a-4a3a-baba-237bbfe845e7",
	}
}

func signES256(t *testing.T, key signingKey, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	if key.kid != "" {
		token.Header["kid"] = key.kid
	}

	signed, err := token.SignedString(key.priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	return signed
}

func newTestVerifier(t *testing.T, supabaseURL string) *Verifier {
	t.Helper()

	v, err := NewVerifier(t.Context(), slog.New(slog.DiscardHandler), supabaseURL, testAudience)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	return v
}

func TestVerifierAcceptsValidToken(t *testing.T) {
	t.Parallel()

	key := newSigningKey(t, "key-1")
	srv := newFakeAuthServer(t, key)
	v := newTestVerifier(t, srv.URL)

	claims := validClaims(srv.issuer())
	expiresAt := time.Now().Add(30 * time.Minute).Truncate(time.Second)
	claims["exp"] = expiresAt.Unix()

	got, err := v.Verify(t.Context(), signES256(t, key, claims))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	want := Identity{UserID: testUserID, ClientID: testClientID, ExpiresAt: expiresAt}
	if got.UserID != want.UserID || got.ClientID != want.ClientID || !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("Verify() = %+v, want %+v", got, want)
	}
}

func TestVerifierAcceptsDirectSessionWithoutClientID(t *testing.T) {
	t.Parallel()

	key := newSigningKey(t, "key-1")
	srv := newFakeAuthServer(t, key)
	v := newTestVerifier(t, srv.URL)

	claims := validClaims(srv.issuer())
	delete(claims, "client_id")

	got, err := v.Verify(t.Context(), signES256(t, key, claims))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if got.ClientID != "" {
		t.Errorf("ClientID = %q, want empty for a non-OAuth session", got.ClientID)
	}
}

func TestVerifierRejectsInvalidTokens(t *testing.T) {
	t.Parallel()

	key := newSigningKey(t, "key-1")
	otherKey := newSigningKey(t, "key-1") // same kid, different private key
	srv := newFakeAuthServer(t, key)
	v := newTestVerifier(t, srv.URL)

	now := time.Now()

	withClaim := func(name string, value any) string {
		claims := validClaims(srv.issuer())
		if value == nil {
			delete(claims, name)
		} else {
			claims[name] = value
		}

		return signES256(t, key, claims)
	}

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "garbage", token: "not.a.jwt"},
		{name: "expired", token: withClaim("exp", now.Add(-time.Hour).Unix())},
		{name: "missing exp", token: withClaim("exp", nil)},
		{name: "not yet valid", token: withClaim("nbf", now.Add(time.Hour).Unix())},
		{name: "issued in the future", token: withClaim("iat", now.Add(time.Hour).Unix())},
		{name: "wrong issuer", token: withClaim("iss", "https://evil.example.com/auth/v1")},
		{name: "missing issuer", token: withClaim("iss", nil)},
		{name: "wrong audience", token: withClaim("aud", "service_role")},
		{name: "missing audience", token: withClaim("aud", nil)},
		{name: "missing sub", token: withClaim("sub", nil)},
		{name: "sub not a uuid", token: withClaim("sub", "admin")},
		{name: "anon role", token: withClaim("role", "anon")},
		{name: "service role", token: withClaim("role", "service_role")},
		{name: "anonymous user", token: withClaim("is_anonymous", true)},
		{name: "signed by unknown key with known kid", token: signES256(t, otherKey, validClaims(srv.issuer()))},
		{name: "unknown kid", token: signES256(t, signingKey{kid: "key-9", priv: key.priv}, validClaims(srv.issuer()))},
		{name: "missing kid", token: signES256(t, signingKey{priv: key.priv}, validClaims(srv.issuer()))},
		{name: "tampered payload", token: tamperPayload(t, signES256(t, key, validClaims(srv.issuer())))},
		{name: "alg none", token: unsignedToken(t, key.kid, validClaims(srv.issuer()))},
		{name: "alg HS256 with public key as secret", token: hmacWithPublicKey(t, key, validClaims(srv.issuer()))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := v.Verify(t.Context(), tt.token)
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("Verify() error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestVerifierRejectsValidlySignedNonES256Token(t *testing.T) {
	t.Parallel()

	key := newSigningKey(t, "key-1")
	srv := newFakeAuthServer(t, key)

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	// The JWKS legitimately publishes an RS256 key; only the ES256 pin keeps it from being accepted.
	srv.addRawJWK(map[string]string{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "rsa-1",
		"n": base64.RawURLEncoding.EncodeToString(rsaKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaKey.E)).Bytes()),
	})

	v := newTestVerifier(t, srv.URL)

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, validClaims(srv.issuer()))
	token.Header["kid"] = "rsa-1"

	signed, err := token.SignedString(rsaKey)
	if err != nil {
		t.Fatalf("sign rs256 token: %v", err)
	}

	if _, err := v.Verify(t.Context(), signed); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Verify() error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifierPicksUpRotatedKey(t *testing.T) {
	t.Parallel()

	oldKey := newSigningKey(t, "key-1")
	newKey := newSigningKey(t, "key-2")
	srv := newFakeAuthServer(t, oldKey)
	v := newTestVerifier(t, srv.URL)

	// Supabase publishes the new key before signing with it.
	srv.setKeys(oldKey, newKey)

	if _, err := v.Verify(t.Context(), signES256(t, newKey, validClaims(srv.issuer()))); err != nil {
		t.Fatalf("Verify() with rotated key error = %v", err)
	}
}

func TestNewVerifierFailsWhenJWKSUnavailable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	if _, err := NewVerifier(t.Context(), slog.New(slog.DiscardHandler), srv.URL, testAudience); err == nil {
		t.Fatal("NewVerifier() error = nil, want error when JWKS cannot be fetched")
	}
}

func tamperPayload(t *testing.T, token string) string {
	t.Helper()

	parts := strings.Split(token, ".")
	claims := validClaims("ignored")
	claims["sub"] = "00000000-0000-0000-0000-000000000000"

	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}

	parts[1] = base64.RawURLEncoding.EncodeToString(payload)

	return strings.Join(parts, ".")
}

func unsignedToken(t *testing.T, kid string, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	token.Header["kid"] = kid

	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none token: %v", err)
	}

	return signed
}

// hmacWithPublicKey builds the classic algorithm-confusion token: HS256 keyed with the public key bytes.
func hmacWithPublicKey(t *testing.T, key signingKey, claims jwt.MapClaims) string {
	t.Helper()

	ecdhKey, err := key.priv.PublicKey.ECDH()
	if err != nil {
		t.Fatalf("public key bytes: %v", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = key.kid

	signed, err := token.SignedString(ecdhKey.Bytes())
	if err != nil {
		t.Fatalf("sign hs256 token: %v", err)
	}

	return signed
}

func TestIssuerURL(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"https://abc.supabase.co", "https://abc.supabase.co/"} {
		if got := IssuerURL(in); got != "https://abc.supabase.co/auth/v1" {
			t.Errorf("IssuerURL(%q) = %q, want https://abc.supabase.co/auth/v1", in, got)
		}
	}
}
