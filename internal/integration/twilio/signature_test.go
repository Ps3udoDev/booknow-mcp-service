package twilio

import (
	"net/url"
	"strings"
	"testing"
)

// Example from Twilio's request validation documentation, recomputed here with an independent
// implementation of the documented steps (see the 7.1 report in docs/handoff/fase7).
const (
	exampleToken     = "12345"
	exampleURL       = "https://mycompany.com/myapp.php?foo=1&bar=2"
	exampleSignature = "GvWf1cFY/Q7PnoempGyD5oXAezc="
)

func exampleForm() url.Values {
	return url.Values{
		"CallSid": {"CA1234567890ABCDE"},
		"Caller":  {"+14158675310"},
		"Digits":  {"1234"},
		"From":    {"+14158675310"},
		"To":      {"+18005551212"},
	}
}

func TestSignature(t *testing.T) {
	t.Parallel()

	if got := Signature(exampleToken, exampleURL, exampleForm()); got != exampleSignature {
		t.Errorf("Signature() = %q, want %q", got, exampleSignature)
	}
}

func TestValidatorValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		token     string
		url       string
		signature string
		mutate    func(url.Values)
		want      bool
	}{
		{name: "valid signature", want: true},
		{name: "empty signature", signature: " "},
		{name: "signature that is not base64", signature: "not-base64!!"},
		{name: "signature of the right length but wrong", signature: "AvWf1cFY/Q7PnoempGyD5oXAezc="},
		{name: "wrong auth token", token: "54321"},
		{name: "another url", url: "https://mycompany.com/myapp.php?foo=1"},
		{name: "url without query string", url: "https://mycompany.com/myapp.php"},
		{name: "changed value", mutate: func(f url.Values) { f.Set("Digits", "1235") }},
		{name: "added parameter", mutate: func(f url.Values) { f.Set("Body", "si") }},
		{name: "removed parameter", mutate: func(f url.Values) { f.Del("Digits") }},
		{name: "value moved to another key", mutate: func(f url.Values) { f.Set("Digits", ""); f.Set("DigitsX", "1234") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			token, target, signature := exampleToken, exampleURL, exampleSignature
			if tt.token != "" {
				token = tt.token
			}

			if tt.url != "" {
				target = tt.url
			}

			if tt.signature != "" {
				signature = tt.signature
			}

			form := exampleForm()
			if tt.mutate != nil {
				tt.mutate(form)
			}

			v, err := NewValidator(token, target)
			if err != nil {
				t.Fatalf("NewValidator() error = %v", err)
			}

			if got := v.ValidateForm(signature, form); got != tt.want {
				t.Errorf("ValidateForm() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidatorRepeatedValues(t *testing.T) {
	t.Parallel()

	form := url.Values{"Body": {"si", "no"}}
	signature := Signature(exampleToken, exampleURL, form)

	v, _ := NewValidator(exampleToken, exampleURL)
	if !v.ValidateForm(signature, form) {
		t.Error("repeated values must validate")
	}

	// Concatenating the values differently must not produce the same signature.
	if Signature(exampleToken, exampleURL, url.Values{"Body": {"sino"}}) == signature {
		t.Error("repeated values and the concatenated value share a signature")
	}
}

func TestNewValidatorRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ token, url string }{
		"empty token":        {"", exampleURL},
		"blank token":        {"   ", exampleURL},
		"empty url":          {exampleToken, ""},
		"relative url":       {exampleToken, "/webhooks/twilio"},
		"url without scheme": {exampleToken, "mycompany.com/webhooks/twilio"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewValidator(tt.token, tt.url); err == nil {
				t.Error("NewValidator() error = nil, want error")
			}
		})
	}
}

func TestValidatorDoesNotLeakTheToken(t *testing.T) {
	t.Parallel()

	v, err := NewValidator("super-secret-token", exampleURL)
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	if s := v.String(); strings.Contains(s, "super-secret-token") {
		t.Errorf("String() = %q, want no token", s)
	}
}

// FuzzValidateForm checks that arbitrary input never panics and never validates by accident.
func FuzzValidateForm(f *testing.F) {
	f.Add("GvWf1cFY/Q7PnoempGyD5oXAezc=", "Body", "si")
	f.Add("", "", "")
	f.Add("%%%", "From", "whatsapp:+58412")

	v, err := NewValidator(exampleToken, exampleURL)
	if err != nil {
		f.Fatalf("NewValidator() error = %v", err)
	}

	f.Fuzz(func(t *testing.T, signature, key, value string) {
		form := url.Values{}
		if key != "" {
			form.Set(key, value)
		}

		if v.ValidateForm(signature, form) && signature != Signature(exampleToken, exampleURL, form) {
			t.Errorf("signature %q accepted for %v", signature, form)
		}
	})
}
