package twilio

import (
	"net/url"
	"regexp"
	"strings"
)

// whatsappPrefix is how Twilio marks WhatsApp addresses in From and To.
const whatsappPrefix = "whatsapp:"

var (
	messageSIDPattern = regexp.MustCompile(`^(SM|MM)[0-9a-f]{32}$`)
	accountSIDPattern = regexp.MustCompile(`^AC[0-9a-f]{32}$`)
)

// InboundMessage is the part of an incoming WhatsApp webhook this service uses.
// From, To and Body carry PII: never log them.
type InboundMessage struct {
	MessageSID string
	AccountSID string
	// From and To are the addresses without the "whatsapp:" prefix, as Twilio sends them (E.164).
	From string
	To   string
	// Body is the free text the customer wrote.
	Body string
	// ButtonPayload and ButtonText are set when the customer taps a quick reply button.
	ButtonPayload string
	ButtonText    string
}

// ParseInbound extracts an InboundMessage from a webhook form. Call it only after the signature
// has been validated. Values are trimmed; unknown fields are ignored.
func ParseInbound(form url.Values) InboundMessage {
	field := func(name string) string { return strings.TrimSpace(form.Get(name)) }

	return InboundMessage{
		MessageSID:    field("MessageSid"),
		AccountSID:    field("AccountSid"),
		From:          strings.TrimPrefix(field("From"), whatsappPrefix),
		To:            strings.TrimPrefix(field("To"), whatsappPrefix),
		Body:          field("Body"),
		ButtonPayload: field("ButtonPayload"),
		ButtonText:    field("ButtonText"),
	}
}

// ValidMessageSID reports whether sid has the shape of a Twilio message SID, so it is safe to log
// and to use as a deduplication key.
func ValidMessageSID(sid string) bool {
	return messageSIDPattern.MatchString(sid)
}

// ValidAccountSID reports whether sid has the shape of a Twilio account SID.
func ValidAccountSID(sid string) bool {
	return accountSIDPattern.MatchString(sid)
}
