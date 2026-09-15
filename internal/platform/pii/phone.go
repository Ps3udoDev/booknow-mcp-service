// Package pii masks personal data before it leaves the service towards an LLM or the audit log.
package pii

import (
	"strings"
	"unicode"
)

const (
	fullMask = "******"
	// minDigitsToReveal: numbers this short are fully masked (the last digits would identify them).
	minDigitsToReveal = 7
	// minDigitsForCountryCode keeps at least 4 hidden digits when the country code is also shown.
	minDigitsForCountryCode = 10
	visibleTail             = 3
	countryCodeDigits       = 3
)

// MaskPhone returns a masked phone number, or nil when there is none.
// It reveals only the last 3 digits and, for international numbers of 10+ digits, the "+" and the
// first 3 digits (e.g. +593******567). Formatting characters are dropped. Numbers of 6 digits or less
// are fully masked.
func MaskPhone(phone string) *string {
	trimmed := strings.TrimSpace(phone)
	if trimmed == "" {
		return nil
	}

	digits := make([]rune, 0, len(trimmed))
	for _, r := range trimmed {
		if unicode.IsDigit(r) {
			digits = append(digits, r)
		}
	}

	if len(digits) < minDigitsToReveal {
		return new(fullMask)
	}

	var b strings.Builder

	hidden := len(digits) - visibleTail

	if strings.HasPrefix(trimmed, "+") && len(digits) >= minDigitsForCountryCode {
		b.WriteByte('+')
		b.WriteString(string(digits[:countryCodeDigits]))

		hidden -= countryCodeDigits
	}

	b.WriteString(strings.Repeat("*", hidden))
	b.WriteString(string(digits[len(digits)-visibleTail:]))

	return new(b.String())
}
