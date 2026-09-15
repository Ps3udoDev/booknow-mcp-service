package auth

import (
	"errors"
	"net/http"
	"strings"
)

// ErrMissingToken means the request has no well-formed "Authorization: Bearer <token>" header.
var ErrMissingToken = errors.New("missing bearer token")

// BearerToken extracts the token from a single Authorization header (RFC 6750 §2.1).
// The token itself is never included in errors.
func BearerToken(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", ErrMissingToken
	}

	scheme, token, found := strings.Cut(strings.TrimSpace(values[0]), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", ErrMissingToken
	}

	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", ErrMissingToken
	}

	return token, nil
}
