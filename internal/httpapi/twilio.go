package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Ps3udoDev/booknow-mcp-service/internal/integration/twilio"
)

const (
	twilioWebhookPath     = "/webhooks/twilio"
	twilioSignatureHeader = "X-Twilio-Signature"
	// maxTwilioBodyBytes bounds the form Twilio posts; a WhatsApp message is far below it.
	maxTwilioBodyBytes = 64 << 10
	emptyTwiML         = `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`
)

// TwilioProcessor handles an authenticated inbound WhatsApp message.
// It must return nil once the message is recorded, even if it was ignored or a duplicate:
// an error answers 500 and Twilio retries.
type TwilioProcessor interface {
	ProcessInbound(ctx context.Context, msg twilio.InboundMessage) error
}

// TwilioConfig wires POST /webhooks/twilio. The route is mounted only when Validator is set.
type TwilioConfig struct {
	Validator *twilio.Validator
	// AccountSID is the only account whose webhooks are accepted.
	AccountSID string
	// Processor is optional until the inbound use case exists; nil acknowledges without acting.
	Processor TwilioProcessor
}

func mountTwilio(r chi.Router, logger *slog.Logger, cfg TwilioConfig) {
	r.Post(twilioWebhookPath, twilioWebhook(logger, cfg))
}

// twilioWebhook authenticates the request before reading any field: content type, bounded raw body,
// signature over that body, then account. Logs carry only the MessageSid and the outcome, never
// the body, phone numbers or the signature.
func twilioWebhook(logger *slog.Logger, cfg TwilioConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := logger.With(slog.String("request_id", middleware.GetReqID(ctx)))

		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/x-www-form-urlencoded" {
			writePlain(w, http.StatusUnsupportedMediaType, "unsupported media type\n")

			return
		}

		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTwilioBodyBytes))
		if err != nil {
			if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
				log.WarnContext(ctx, "twilio webhook body too large")
				writePlain(w, http.StatusRequestEntityTooLarge, "request too large\n")

				return
			}

			writePlain(w, http.StatusBadRequest, "bad request\n")

			return
		}

		form, err := url.ParseQuery(string(raw))
		if err != nil || !cfg.Validator.ValidateForm(r.Header.Get(twilioSignatureHeader), form) {
			log.WarnContext(ctx, "twilio webhook rejected", slog.String("reason", "invalid_signature"))
			writePlain(w, http.StatusForbidden, "forbidden\n")

			return
		}

		msg := twilio.ParseInbound(form)
		if msg.AccountSID != cfg.AccountSID {
			log.WarnContext(ctx, "twilio webhook rejected", slog.String("reason", "unexpected_account"))
			writePlain(w, http.StatusForbidden, "forbidden\n")

			return
		}

		if twilio.ValidMessageSID(msg.MessageSID) {
			log = log.With(slog.String("message_sid", msg.MessageSID))
		}

		if cfg.Processor == nil {
			log.InfoContext(ctx, "twilio webhook acknowledged", slog.String("result", "not_processed"))
			writeTwiML(w)

			return
		}

		if err := cfg.Processor.ProcessInbound(ctx, msg); err != nil {
			log.ErrorContext(ctx, "twilio webhook processing failed", slog.Any("error", err))
			writePlain(w, http.StatusInternalServerError, "internal error\n")

			return
		}

		log.InfoContext(ctx, "twilio webhook processed")
		writeTwiML(w)
	}
}

func writeTwiML(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(emptyTwiML))
}
