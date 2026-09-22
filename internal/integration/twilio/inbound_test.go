package twilio

import (
	"net/url"
	"testing"
)

func TestParseInbound(t *testing.T) {
	t.Parallel()

	form := url.Values{
		"MessageSid":    {"SM0123456789abcdef0123456789abcdef"},
		"AccountSid":    {"AC0123456789abcdef0123456789abcdef"},
		"From":          {"whatsapp:+593987654321"},
		"To":            {"whatsapp:+14155238886"},
		"Body":          {"  sí, confirmo  "},
		"ButtonPayload": {"CONFIRM"},
		"ButtonText":    {"Confirmar"},
		"ProfileName":   {"ignored"},
	}

	want := InboundMessage{
		MessageSID:    "SM0123456789abcdef0123456789abcdef",
		AccountSID:    "AC0123456789abcdef0123456789abcdef",
		From:          "+593987654321",
		To:            "+14155238886",
		Body:          "sí, confirmo",
		ButtonPayload: "CONFIRM",
		ButtonText:    "Confirmar",
	}

	if got := ParseInbound(form); got != want {
		t.Errorf("ParseInbound() = %+v, want %+v", got, want)
	}
}

func TestParseInboundWithoutWhatsAppPrefix(t *testing.T) {
	t.Parallel()

	got := ParseInbound(url.Values{"From": {"+593987654321"}})
	if got.From != "+593987654321" {
		t.Errorf("From = %q, want the number unchanged", got.From)
	}
}

func TestValidSIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		check func(string) bool
		sid   string
		want  bool
	}{
		{name: "sms message", check: ValidMessageSID, sid: "SM0123456789abcdef0123456789abcdef", want: true},
		{name: "media message", check: ValidMessageSID, sid: "MM0123456789abcdef0123456789abcdef", want: true},
		{name: "message too short", check: ValidMessageSID, sid: "SM0123"},
		{name: "message uppercase hex", check: ValidMessageSID, sid: "SM0123456789ABCDEF0123456789ABCDEF"},
		{name: "message with newline", check: ValidMessageSID, sid: "SM0123456789abcdef0123456789abcdef\n"},
		{name: "account sid as message", check: ValidMessageSID, sid: "AC0123456789abcdef0123456789abcdef"},
		{name: "empty message", check: ValidMessageSID, sid: ""},
		{name: "account", check: ValidAccountSID, sid: "AC0123456789abcdef0123456789abcdef", want: true},
		{name: "account wrong prefix", check: ValidAccountSID, sid: "SM0123456789abcdef0123456789abcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.check(tt.sid); got != tt.want {
				t.Errorf("check(%q) = %v, want %v", tt.sid, got, tt.want)
			}
		})
	}
}
