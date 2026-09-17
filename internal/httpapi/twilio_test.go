package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/integration/twilio"
)

const (
	testTwilioToken      = "test-auth-token"
	testTwilioURL        = "https://example.test/webhooks/twilio"
	testTwilioAccountSID = "AC0123456789abcdef0123456789abcdef"
	testTwilioMessageSID = "SM0123456789abcdef0123456789abcdef"
	testTwilioPhone      = "+593987654321"
)

type recordingProcessor struct {
	mu    sync.Mutex
	calls []twilio.InboundMessage
	err   error
}

func (p *recordingProcessor) ProcessInbound(_ context.Context, msg twilio.InboundMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls = append(p.calls, msg)

	return p.err
}

func (p *recordingProcessor) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.calls)
}

func twilioForm() url.Values {
	return url.Values{
		"MessageSid":    {testTwilioMessageSID},
		"AccountSid":    {testTwilioAccountSID},
		"From":          {"whatsapp:" + testTwilioPhone},
		"To":            {"whatsapp:+14155238886"},
		"Body":          {"Confirmar"},
		"ButtonPayload": {"CONFIRM"},
		"ButtonText":    {"Confirmar"},
		"NumMedia":      {"0"},
	}
}

func newTwilioRouter(t *testing.T, logger *slog.Logger, processor TwilioProcessor) http.Handler {
	t.Helper()

	validator, err := twilio.NewValidator(testTwilioToken, testTwilioURL)
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}

	return NewRouter(logger, Deps{
		DB: &fakePinger{},
		Twilio: TwilioConfig{
			Validator:  validator,
			AccountSID: testTwilioAccountSID,
			Processor:  processor,
		},
	})
}

type twilioRequest struct {
	method      string
	contentType string
	body        string
	signature   string
	noSignature bool
}

func signedTwilioRequest(form url.Values) twilioRequest {
	return twilioRequest{
		method:      http.MethodPost,
		contentType: "application/x-www-form-urlencoded",
		body:        form.Encode(),
		signature:   twilio.Signature(testTwilioToken, testTwilioURL, form),
	}
}

func (tr twilioRequest) do(t *testing.T, router http.Handler) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), tr.method, twilioWebhookPath, strings.NewReader(tr.body))
	if tr.contentType != "" {
		req.Header.Set("Content-Type", tr.contentType)
	}

	if !tr.noSignature {
		req.Header.Set(twilioSignatureHeader, tr.signature)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

func TestTwilioWebhook(t *testing.T) {
	t.Parallel()

	tamperedBody := func() twilioRequest {
		tr := signedTwilioRequest(twilioForm())
		form := twilioForm()
		form.Set("ButtonPayload", "CANCEL")
		tr.body = form.Encode()

		return tr
	}

	otherAccount := func() twilioRequest {
		form := twilioForm()
		form.Set("AccountSid", "ACffffffffffffffffffffffffffffffff")

		return signedTwilioRequest(form)
	}

	tests := []struct {
		name          string
		request       func() twilioRequest
		wantStatus    int
		wantProcessed bool
	}{
		{
			name:          "valid signature is processed",
			request:       func() twilioRequest { return signedTwilioRequest(twilioForm()) },
			wantStatus:    http.StatusOK,
			wantProcessed: true,
		},
		{
			name: "content type with charset is accepted",
			request: func() twilioRequest {
				tr := signedTwilioRequest(twilioForm())
				tr.contentType = "application/x-www-form-urlencoded; charset=UTF-8"

				return tr
			},
			wantStatus:    http.StatusOK,
			wantProcessed: true,
		},
		{name: "tampered body", request: tamperedBody, wantStatus: http.StatusForbidden},
		{
			name: "missing signature header",
			request: func() twilioRequest {
				tr := signedTwilioRequest(twilioForm())
				tr.noSignature = true

				return tr
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "signature from another token",
			request: func() twilioRequest {
				tr := signedTwilioRequest(twilioForm())
				tr.signature = twilio.Signature("other-token", testTwilioURL, twilioForm())

				return tr
			},
			wantStatus: http.StatusForbidden,
		},
		{name: "signed but from another account", request: otherAccount, wantStatus: http.StatusForbidden},
		{
			name: "malformed body",
			request: func() twilioRequest {
				tr := signedTwilioRequest(twilioForm())
				tr.body = "Body=%zz"

				return tr
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "json content type",
			request: func() twilioRequest {
				tr := signedTwilioRequest(twilioForm())
				tr.contentType = "application/json"

				return tr
			},
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name: "missing content type",
			request: func() twilioRequest {
				tr := signedTwilioRequest(twilioForm())
				tr.contentType = ""

				return tr
			},
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name: "body over the limit",
			request: func() twilioRequest {
				form := twilioForm()
				form.Set("Body", strings.Repeat("a", maxTwilioBodyBytes))

				return signedTwilioRequest(form)
			},
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "get is not allowed",
			request: func() twilioRequest {
				tr := signedTwilioRequest(twilioForm())
				tr.method = http.MethodGet

				return tr
			},
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			processor := &recordingProcessor{}
			router := newTwilioRouter(t, slog.New(slog.DiscardHandler), processor)

			rec := tt.request().do(t, router)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if got := processor.count() == 1; got != tt.wantProcessed {
				t.Errorf("processed = %v, want %v", got, tt.wantProcessed)
			}

			if rec.Code == http.StatusForbidden && strings.TrimSpace(rec.Body.String()) != "forbidden" {
				t.Errorf("403 body = %q, must not explain why", rec.Body.String())
			}
		})
	}
}

func TestTwilioWebhookRespondsWithEmptyTwiML(t *testing.T) {
	t.Parallel()

	router := newTwilioRouter(t, slog.New(slog.DiscardHandler), &recordingProcessor{})
	rec := signedTwilioRequest(twilioForm()).do(t, router)

	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/xml") {
		t.Errorf("Content-Type = %q, want text/xml", got)
	}

	if got := rec.Body.String(); !strings.Contains(got, "<Response></Response>") {
		t.Errorf("body = %q, want empty TwiML response", got)
	}
}

func TestTwilioWebhookPassesParsedMessage(t *testing.T) {
	t.Parallel()

	processor := &recordingProcessor{}
	router := newTwilioRouter(t, slog.New(slog.DiscardHandler), processor)

	signedTwilioRequest(twilioForm()).do(t, router)

	if processor.count() != 1 {
		t.Fatalf("processor calls = %d, want 1", processor.count())
	}

	want := twilio.InboundMessage{
		MessageSID:    testTwilioMessageSID,
		AccountSID:    testTwilioAccountSID,
		From:          testTwilioPhone,
		To:            "+14155238886",
		Body:          "Confirmar",
		ButtonPayload: "CONFIRM",
		ButtonText:    "Confirmar",
	}
	if got := processor.calls[0]; got != want {
		t.Errorf("message = %+v, want %+v", got, want)
	}
}

func TestTwilioWebhookProcessorFailureAsksForRetry(t *testing.T) {
	t.Parallel()

	processor := &recordingProcessor{err: errors.New("database unavailable")}
	router := newTwilioRouter(t, slog.New(slog.DiscardHandler), processor)

	rec := signedTwilioRequest(twilioForm()).do(t, router)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d so Twilio retries", rec.Code, http.StatusInternalServerError)
	}

	if strings.Contains(rec.Body.String(), "database") {
		t.Errorf("body leaks the internal error: %q", rec.Body.String())
	}
}

func TestTwilioWebhookLogsNoPII(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	form := twilioForm()
	form.Set("Body", "secret free text from the customer")

	good := signedTwilioRequest(form)
	bad := signedTwilioRequest(form)
	bad.signature = "AvWf1cFY/Q7PnoempGyD5oXAezc="

	for _, processor := range []*recordingProcessor{{}, {err: errors.New("boom")}} {
		router := newTwilioRouter(t, logger, processor)
		good.do(t, router)
		bad.do(t, router)
	}

	out := logs.String()
	for _, secret := range []string{"secret free text", "987654321", testTwilioToken, good.signature, bad.signature} {
		if strings.Contains(out, secret) {
			t.Errorf("logs contain %q:\n%s", secret, out)
		}
	}

	if !strings.Contains(out, testTwilioMessageSID) {
		t.Errorf("logs should identify the message by MessageSid:\n%s", out)
	}
}

func TestTwilioWebhookNotMountedWithoutValidator(t *testing.T) {
	t.Parallel()

	router := NewRouter(slog.New(slog.DiscardHandler), Deps{DB: &fakePinger{}})
	rec := signedTwilioRequest(twilioForm()).do(t, router)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d when Twilio is not configured", rec.Code, http.StatusNotFound)
	}
}
