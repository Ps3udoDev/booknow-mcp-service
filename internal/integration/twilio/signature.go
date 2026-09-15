// Package twilio implements the parts of the Twilio API this service needs: verification of the
// X-Twilio-Signature header of incoming webhooks and (later) sending WhatsApp messages.
package twilio

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // SHA-1 is what the Twilio signature protocol requires.
	"encoding/base64"
	"errors"
	"maps"
	"net/url"
	"slices"
	"strings"
)

// Signature computes the value Twilio puts in X-Twilio-Signature for a form-encoded request:
// the full request URL followed by every parameter name and value sorted by name, signed with
// HMAC-SHA1 using the account auth token and encoded as base64.
//
// SHA-1 is not a choice: it is what the Twilio protocol requires.
func Signature(authToken, requestURL string, form url.Values) string {
	var b strings.Builder

	b.WriteString(requestURL)

	for _, key := range slices.Sorted(maps.Keys(form)) {
		for _, value := range form[key] {
			b.WriteString(key)
			b.WriteString(value)
		}
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(b.String()))

	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// Validator checks the signature of incoming Twilio webhooks against a fixed public URL.
// The URL must be exactly the one configured in Twilio: it is part of what Twilio signs.
type Validator struct {
	authToken  string
	webhookURL string
}

// NewValidator returns a Validator for the account auth token and the public webhook URL.
func NewValidator(authToken, webhookURL string) (*Validator, error) {
	if strings.TrimSpace(authToken) == "" {
		return nil, errors.New("twilio: auth token is required")
	}

	parsed, err := url.Parse(webhookURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return nil, errors.New("twilio: webhook URL must be absolute, including scheme and host")
	}

	return &Validator{authToken: authToken, webhookURL: webhookURL}, nil
}

// ValidateForm reports whether signature matches the form parameters of a request to the webhook URL.
// It compares in constant time and never reveals why a signature failed.
func (v *Validator) ValidateForm(signature string, form url.Values) bool {
	want, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}

	got, err := base64.StdEncoding.DecodeString(Signature(v.authToken, v.webhookURL, form))
	if err != nil {
		return false
	}

	return hmac.Equal(want, got)
}

// String hides the auth token so a Validator cannot end up in a log.
func (v *Validator) String() string {
	return "twilio.Validator{webhookURL: " + v.webhookURL + "}"
}
