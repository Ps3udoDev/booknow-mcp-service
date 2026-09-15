package pii

import (
	"strings"
	"testing"
	"unicode"
)

func TestMaskPhone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		phone string
		want  string
		isNil bool
	}{
		{name: "empty", phone: "", isNil: true},
		{name: "blank", phone: "   ", isNil: true},
		{name: "international with plus", phone: "+593991234567", want: "+593******567"},
		{name: "venezuelan", phone: "+584121234123", want: "+584******123"},
		{name: "local without plus keeps no prefix", phone: "0991234567", want: "*******567"},
		{name: "formatted number", phone: "+593 99-123-4567", want: "+593******567"},
		{name: "surrounding spaces", phone: "  +593991234567 ", want: "+593******567"},
		{name: "short number fully masked", phone: "12345", want: "******"},
		{name: "six digits fully masked", phone: "123456", want: "******"},
		{name: "letters only fully masked", phone: "no tiene", want: "******"},
		{name: "seven digits", phone: "1234567", want: "****567"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := MaskPhone(tt.phone)
			if tt.isNil {
				if got != nil {
					t.Fatalf("MaskPhone(%q) = %q, want nil", tt.phone, *got)
				}

				return
			}

			if got == nil {
				t.Fatalf("MaskPhone(%q) = nil, want %q", tt.phone, tt.want)
			}

			if *got != tt.want {
				t.Errorf("MaskPhone(%q) = %q, want %q", tt.phone, *got, tt.want)
			}
		})
	}
}

// FuzzMaskPhone checks the invariants that protect privacy for any input.
func FuzzMaskPhone(f *testing.F) {
	for _, seed := range []string{"+593991234567", "0991234567", "123", "", "+1 (555) 010-9999", "٠١٢٣٤٥٦٧٨٩", "+" + strings.Repeat("9", 40)} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, phone string) {
		got := MaskPhone(phone)
		if got == nil {
			if strings.TrimSpace(phone) != "" {
				t.Fatalf("MaskPhone(%q) = nil for non-blank input", phone)
			}

			return
		}

		digits := countDigits(phone)
		visible := countDigits(*got)

		// Short numbers reveal nothing; longer ones reveal at most country code (3) + last 3 digits.
		if digits <= 6 && visible != 0 {
			t.Fatalf("MaskPhone(%q) = %q reveals digits of a short number", phone, *got)
		}

		if visible > 6 {
			t.Fatalf("MaskPhone(%q) = %q reveals %d digits", phone, *got, visible)
		}

		if digits > 6 && visible >= digits {
			t.Fatalf("MaskPhone(%q) = %q reveals the whole number", phone, *got)
		}
	})
}

func countDigits(s string) int {
	n := 0

	for _, r := range s {
		if unicode.IsDigit(r) {
			n++
		}
	}

	return n
}
