package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"
)

// TestVerifierIntegration verifies a real access token issued by a local Supabase Auth (`supabase start`).
// Enable with TEST_SUPABASE_URL=http://127.0.0.1:54321 and TEST_SUPABASE_PUBLISHABLE_KEY (from `supabase status`).
func TestVerifierIntegration(t *testing.T) {
	t.Parallel()

	supabaseURL := os.Getenv("TEST_SUPABASE_URL")
	apiKey := os.Getenv("TEST_SUPABASE_PUBLISHABLE_KEY")

	if supabaseURL == "" || apiKey == "" {
		t.Skip("TEST_SUPABASE_URL or TEST_SUPABASE_PUBLISHABLE_KEY not set; skipping integration test")
	}

	token := signUpTestUser(t, supabaseURL, apiKey)

	v, err := NewVerifier(t.Context(), slog.New(slog.DiscardHandler), supabaseURL, "authenticated")
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}

	id, err := v.Verify(t.Context(), token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if !isUUID(id.UserID) {
		t.Errorf("UserID = %q, want a uuid", id.UserID)
	}

	if id.ClientID != "" {
		t.Errorf("ClientID = %q, want empty for a password session", id.ClientID)
	}
}

// signUpTestUser creates a throwaway user in the local Auth instance and returns its access token.
func signUpTestUser(t *testing.T, supabaseURL, apiKey string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"email":    fmt.Sprintf("go-auth-test-%s@example.test", rand.Text()),
		"password": rand.Text(),
	})
	if err != nil {
		t.Fatalf("marshal signup body: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, supabaseURL+"/auth/v1/signup", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build signup request: %v", err)
	}

	req.Header.Set("apikey", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("signup request: %v", err)
	}
	defer resp.Body.Close()

	var out struct {
		AccessToken string `json:"access_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode signup response: %v", err)
	}

	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		t.Fatalf("signup status = %d, no access token (is email autoconfirm enabled locally?)", resp.StatusCode)
	}

	return out.AccessToken
}
