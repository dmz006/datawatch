// BL427 (phase 5) — request signing for the lifecycle outbound webhook sink.
// Design: docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md
// (section 2(b)).
//
// Every POST carries one signature header that binds a timestamp into the
// signed bytes, so a captured request cannot be replayed later and cannot be
// half-replayed with a fresh timestamp:
//
//	X-Datawatch-Signature: t=<unix-seconds>,v1=<lowercase hex>
//	v1 = hex( HMAC-SHA256( secret, "<t>" + "." + <raw request body> ) )
//
// The secret is resolved from the secrets vault by the caller on every
// delivery and passed in here as bytes; this file never logs it, never
// stores it and never puts it (or the header value) into an error.

package autonomous

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// The wire details below are pending operator confirmation. They live here,
// and only here, so a changed decision is a one-place edit plus its test
// expectation.
const (
	// WebhookSignatureHeader carries the timestamped HMAC.
	WebhookSignatureHeader = "X-Datawatch-Signature"
	// WebhookDeliveryHeader carries the delivery id, identical on every
	// retry of one delivery.
	WebhookDeliveryHeader = "X-Datawatch-Delivery"
	// WebhookEventHeader carries the event kind.
	WebhookEventHeader = "X-Datawatch-Event"

	// webhookSigTimestampKey and webhookSigMACKey name the two fields of the
	// signature header value; webhookSigSeparator joins the timestamp and the
	// body in the signed string.
	webhookSigTimestampKey = "t"
	webhookSigMACKey       = "v1"
	webhookSigSeparator    = "."
)

// WebhookSignatureTolerance is the default replay window the receiver recipe
// uses (5 minutes, from config). Pending operator confirmation; the config key
// autonomous.notify.webhook.signature_tolerance_seconds overrides it.
const WebhookSignatureTolerance = config.DefaultNotifySignatureToleranceSeconds * time.Second

// Verification failures. The messages are fixed text: they never contain the
// header value, the body or the secret.
var (
	ErrWebhookSignatureNoSecret  = errors.New("webhook signature: no secret supplied")
	ErrWebhookSignatureMalformed = errors.New("webhook signature: header is malformed")
	ErrWebhookSignatureStale     = errors.New("webhook signature: timestamp is too old")
	ErrWebhookSignatureFuture    = errors.New("webhook signature: timestamp is too far in the future")
	ErrWebhookSignatureMismatch  = errors.New("webhook signature: signature does not match")
)

func webhookSignatureMAC(secret []byte, ts int64, body []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(strconv.FormatInt(ts, 10)))
	m.Write([]byte(webhookSigSeparator))
	m.Write(body)
	return m.Sum(nil)
}

// SignWebhookBody returns the signature header value for body at time ts:
// "t=<unix-seconds>,v1=<lowercase hex>". It returns "" when secret is empty —
// an unsigned sink is not allowed, so the caller must treat "" as a failure
// rather than send it.
func SignWebhookBody(secret []byte, ts time.Time, body []byte) string {
	if len(secret) == 0 {
		return ""
	}
	t := ts.Unix()
	return webhookSigTimestampKey + "=" + strconv.FormatInt(t, 10) + "," +
		webhookSigMACKey + "=" + hex.EncodeToString(webhookSignatureMAC(secret, t, body))
}

// VerifyWebhookSignature checks header against body: the header must hold
// exactly one timestamp and one signature field, the timestamp must be within
// tolerance of now (a non-positive tolerance means WebhookSignatureTolerance),
// and the signature must match, compared in constant time with hmac.Equal.
// Unknown extra fields are ignored so the format can grow.
func VerifyWebhookSignature(secret []byte, header string, body []byte, now time.Time, tolerance time.Duration) error {
	if len(secret) == 0 {
		return ErrWebhookSignatureNoSecret
	}
	if tolerance <= 0 {
		tolerance = WebhookSignatureTolerance
	}
	var tsStr, macStr string
	var haveTS, haveMAC bool
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return ErrWebhookSignatureMalformed
		}
		switch k {
		case webhookSigTimestampKey:
			if haveTS {
				return ErrWebhookSignatureMalformed
			}
			tsStr, haveTS = v, true
		case webhookSigMACKey:
			if haveMAC {
				return ErrWebhookSignatureMalformed
			}
			macStr, haveMAC = v, true
		}
	}
	if !haveTS || !haveMAC || tsStr == "" || len(tsStr) > 18 {
		return ErrWebhookSignatureMalformed
	}
	for _, c := range tsStr {
		if c < '0' || c > '9' {
			return ErrWebhookSignatureMalformed
		}
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return ErrWebhookSignatureMalformed
	}
	got, err := hex.DecodeString(macStr)
	if err != nil || len(got) != sha256.Size {
		return ErrWebhookSignatureMalformed
	}

	skew := now.Unix() - ts
	if skew > int64(tolerance/time.Second) {
		return ErrWebhookSignatureStale
	}
	if -skew > int64(tolerance/time.Second) {
		return ErrWebhookSignatureFuture
	}
	if !hmac.Equal(got, webhookSignatureMAC(secret, ts, body)) {
		return ErrWebhookSignatureMismatch
	}
	return nil
}
