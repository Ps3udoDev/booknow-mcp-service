package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		header  []string
		want    string
		wantErr error
	}{
		{name: "valid", header: []string{"Bearer abc.def.ghi"}, want: "abc.def.ghi"},
		{name: "scheme is case-insensitive", header: []string{"bearer abc.def.ghi"}, want: "abc.def.ghi"},
		{name: "surrounding spaces", header: []string{"Bearer   abc.def.ghi  "}, want: "abc.def.ghi"},
		{name: "missing header", header: nil, wantErr: ErrMissingToken},
		{name: "empty header", header: []string{""}, wantErr: ErrMissingToken},
		{name: "scheme only", header: []string{"Bearer"}, wantErr: ErrMissingToken},
		{name: "scheme and spaces only", header: []string{"Bearer   "}, wantErr: ErrMissingToken},
		{name: "basic scheme", header: []string{"Basic dXNlcjpwYXNz"}, wantErr: ErrMissingToken},
		{name: "scheme prefix without separator", header: []string{"Bearerabc.def.ghi"}, wantErr: ErrMissingToken},
		{name: "token with inner space", header: []string{"Bearer abc def"}, wantErr: ErrMissingToken},
		{name: "multiple headers", header: []string{"Bearer a.b.c", "Bearer d.e.f"}, wantErr: ErrMissingToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", nil)
			for _, h := range tt.header {
				req.Header.Add("Authorization", h)
			}

			got, err := BearerToken(req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("BearerToken() error = %v, want %v", err, tt.wantErr)
			}

			if got != tt.want {
				t.Errorf("BearerToken() = %q, want %q", got, tt.want)
			}
		})
	}
}
