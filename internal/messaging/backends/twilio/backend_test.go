// SEC-005 (docs/plans/historical-plans/2026-08-28-security-assessment-core.md)
// — mirrors SEC-004 (github backend): the Twilio webhook handler stored an
// auth token but never verified X-Twilio-Signature, so any party able to
// reach the listener could forge inbound SMS and inject text into the
// operator's command stream (the "only process messages from to_number"
// check is trivially spoofable — From is just a POST field the caller
// controls). Covers the new verifySignature check directly and the full
// webhook handler end-to-end.

package twilio

import (
	"crypto/hmac"
	"crypto/sha1" // #nosec G505 -- test mirrors Twilio's documented signature spec
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/messaging"
)

const testURL = "https://example.ts.net/sms"

func sign(authToken, fullURL string, form url.Values) string {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	buf.WriteString(fullURL)
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteString(form.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(buf.String())) //nolint:errcheck
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature_ValidAccepted(t *testing.T) {
	form := url.Values{"From": {"+15551234567"}, "Body": {"hello"}}
	sig := sign("my-token", testURL, form)
	if !verifySignature("my-token", testURL, form, sig) {
		t.Error("a correctly signed request must be accepted")
	}
}

func TestVerifySignature_WrongTokenRejected(t *testing.T) {
	form := url.Values{"From": {"+15551234567"}, "Body": {"hello"}}
	sig := sign("wrong-token", testURL, form)
	if verifySignature("my-token", testURL, form, sig) {
		t.Error("a request signed with a different auth token must be rejected")
	}
}

func TestVerifySignature_TamperedParamRejected(t *testing.T) {
	form := url.Values{"From": {"+15551234567"}, "Body": {"hello"}}
	sig := sign("my-token", testURL, form)
	tampered := url.Values{"From": {"+15551234567"}, "Body": {"hello, forged"}}
	if verifySignature("my-token", testURL, tampered, sig) {
		t.Error("a signature computed over different params must be rejected")
	}
}

func TestVerifySignature_WrongURLRejected(t *testing.T) {
	form := url.Values{"From": {"+15551234567"}, "Body": {"hello"}}
	sig := sign("my-token", testURL, form)
	if verifySignature("my-token", "https://attacker.example/sms", form, sig) {
		t.Error("a signature computed over a different URL must be rejected")
	}
}

func TestVerifySignature_MissingHeaderRejected(t *testing.T) {
	form := url.Values{"From": {"+15551234567"}, "Body": {"hello"}}
	if verifySignature("my-token", testURL, form, "") {
		t.Error("an empty signature header must be rejected when a public URL is configured")
	}
}

func TestVerifySignature_MalformedHeaderRejected(t *testing.T) {
	form := url.Values{"From": {"+15551234567"}, "Body": {"hello"}}
	if verifySignature("my-token", testURL, form, "not-valid-base64-!!!") {
		t.Error("an undecodable signature header must be rejected")
	}
}

func TestVerifySignature_EmptyPublicURLBypasses(t *testing.T) {
	// Documented, intentional: an operator who leaves twilio.webhook_public_url
	// unset gets no verification (matches github_webhook.secret's existing
	// "optional secret" convention) -- confirms that deliberate case keeps
	// working, not a gap.
	form := url.Values{"From": {"+15551234567"}, "Body": {"hello"}}
	if !verifySignature("my-token", "", form, "garbage-that-would-never-verify") {
		t.Error("an empty configured public URL must skip verification, not reject everything")
	}
}

func TestHandleSMS_ValidSignatureDeliversMessage(t *testing.T) {
	b := New("AC_test", "my-token", "+15550001111", "+15552223333", "127.0.0.1:0", testURL)
	msgs := make(chan messaging.Message, 1)
	h := b.handleSMS(func(m messaging.Message) { msgs <- m })

	form := url.Values{"From": {"+15552223333"}, "Body": {"hello from a real webhook"}}
	req := httptest.NewRequest(http.MethodPost, "/sms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	req.Header.Set("X-Twilio-Signature", sign("my-token", testURL, req.PostForm))

	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	select {
	case msg := <-msgs:
		if msg.Text != "hello from a real webhook" || msg.Sender != "+15552223333" {
			t.Errorf("unexpected message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no message delivered for a validly signed request")
	}
}

func TestHandleSMS_InvalidSignatureRejectedNoMessage(t *testing.T) {
	b := New("AC_test", "my-token", "+15550001111", "+15552223333", "127.0.0.1:0", testURL)
	msgs := make(chan messaging.Message, 1)
	h := b.handleSMS(func(m messaging.Message) { msgs <- m })

	form := url.Values{"From": {"+15552223333"}, "Body": {"forged by an attacker"}}
	req := httptest.NewRequest(http.MethodPost, "/sms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", base64.StdEncoding.EncodeToString([]byte("wrong-signature-bytes-00")))

	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rr.Code)
	}
	select {
	case msg := <-msgs:
		t.Fatalf("forged request must never be delivered, got: %+v", msg)
	case <-time.After(100 * time.Millisecond):
		// expected: no message
	}
}

func TestHandleSMS_MissingSignatureRejected(t *testing.T) {
	b := New("AC_test", "my-token", "+15550001111", "+15552223333", "127.0.0.1:0", testURL)
	h := b.handleSMS(func(m messaging.Message) {})

	form := url.Values{"From": {"+15552223333"}, "Body": {"no signature at all"}}
	req := httptest.NewRequest(http.MethodPost, "/sms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rr.Code)
	}
}

func TestHandleSMS_EmptyPublicURLAllowsUnsigned(t *testing.T) {
	// The deliberate bypass case (see TestVerifySignature_EmptyPublicURLBypasses)
	// exercised through the real handler.
	b := New("AC_test", "my-token", "+15550001111", "+15552223333", "127.0.0.1:0", "")
	msgs := make(chan messaging.Message, 1)
	h := b.handleSMS(func(m messaging.Message) { msgs <- m })

	form := url.Values{"From": {"+15552223333"}, "Body": {"unsigned but public URL unset"}}
	req := httptest.NewRequest(http.MethodPost, "/sms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (no public URL configured means no verification)", rr.Code)
	}
	select {
	case <-msgs:
	case <-time.After(time.Second):
		t.Fatal("expected a message when no public URL is configured")
	}
}

func TestHandleSMS_WrongFromNumberIgnoredEvenWithValidSignature(t *testing.T) {
	// A validly-signed request (so it IS really from Twilio) but From
	// doesn't match the configured to_number -- the secondary filter
	// documented in handleSMS. This is about ignoring an unexpected
	// Twilio-forwarded number, not a security boundary on its own.
	b := New("AC_test", "my-token", "+15550001111", "+15552223333", "127.0.0.1:0", testURL)
	msgs := make(chan messaging.Message, 1)
	h := b.handleSMS(func(m messaging.Message) { msgs <- m })

	form := url.Values{"From": {"+19995551234"}, "Body": {"from some other real Twilio number"}}
	req := httptest.NewRequest(http.MethodPost, "/sms", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("ParseForm: %v", err)
	}
	req.Header.Set("X-Twilio-Signature", sign("my-token", testURL, req.PostForm))

	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (still a valid Twilio request, just ignored)", rr.Code)
	}
	select {
	case msg := <-msgs:
		t.Fatalf("message from an unexpected number must not be delivered, got: %+v", msg)
	case <-time.After(100 * time.Millisecond):
		// expected: no message
	}
}
