// SEC-004 (docs/plans/historical-plans/2026-08-28-security-assessment-core.md)
// — the GitHub webhook handler stored a signing secret but never
// verified it, so any party able to reach the listener could forge
// issue_comment/workflow_dispatch payloads and inject text into the
// operator's command stream. Covers the new verifySignature check
// directly and the full handleWebhook path end-to-end.

package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body) //nolint:errcheck
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature_ValidAccepted(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	if !verifySignature("my-secret", body, sign("my-secret", body)) {
		t.Error("a correctly signed body must be accepted")
	}
}

func TestVerifySignature_WrongSecretRejected(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	if verifySignature("my-secret", body, sign("wrong-secret", body)) {
		t.Error("a body signed with a different secret must be rejected")
	}
}

func TestVerifySignature_TamperedBodyRejected(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	sig := sign("my-secret", body)
	tampered := []byte(`{"hello":"world!"}`)
	if verifySignature("my-secret", tampered, sig) {
		t.Error("a signature computed over a different body must be rejected")
	}
}

func TestVerifySignature_MissingHeaderRejected(t *testing.T) {
	if verifySignature("my-secret", []byte(`{}`), "") {
		t.Error("an empty signature header must be rejected when a secret is configured")
	}
}

func TestVerifySignature_MalformedHeaderRejected(t *testing.T) {
	cases := []string{
		"not-even-hex-prefixed",
		"sha1=deadbeef",             // wrong algorithm prefix
		"sha256=not-valid-hex-zzzz", // valid prefix, undecodable hex
	}
	for _, sig := range cases {
		if verifySignature("my-secret", []byte(`{}`), sig) {
			t.Errorf("malformed signature %q must be rejected", sig)
		}
	}
}

func TestVerifySignature_EmptySecretBypasses(t *testing.T) {
	// Documented, intentional: an operator who leaves github_webhook.secret
	// unset gets no verification (matches this codebase's existing
	// "optional secret" convention), same as before this fix -- this
	// confirms that specific, deliberate case keeps working, not a gap.
	if !verifySignature("", []byte(`{}`), "garbage-that-would-never-verify") {
		t.Error("an empty configured secret must skip verification, not reject everything")
	}
}

func TestHandleWebhook_ValidSignatureDeliversMessage(t *testing.T) {
	b := New("127.0.0.1:0", "my-secret")
	defer b.srv.Shutdown(nil) //nolint:errcheck

	payload := `{"comment":{"body":"hello from a real webhook"},"sender":{"login":"octocat"},"issue":{"number":42}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-Hub-Signature-256", sign("my-secret", []byte(payload)))
	rr := httptest.NewRecorder()
	b.handleWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	select {
	case msg := <-b.msgs:
		if msg.Text != "hello from a real webhook" || msg.Sender != "octocat" || msg.GroupID != "issue:42" {
			t.Errorf("unexpected message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("no message delivered for a validly signed payload")
	}
}

func TestHandleWebhook_InvalidSignatureRejectedNoMessage(t *testing.T) {
	b := New("127.0.0.1:0", "my-secret")
	defer b.srv.Shutdown(nil) //nolint:errcheck

	payload := `{"comment":{"body":"forged by an attacker"},"sender":{"login":"not-octocat"},"issue":{"number":1}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64)) // well-formed but wrong
	rr := httptest.NewRecorder()
	b.handleWebhook(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rr.Code)
	}
	select {
	case msg := <-b.msgs:
		t.Fatalf("forged payload must never be delivered, got: %+v", msg)
	case <-time.After(100 * time.Millisecond):
		// expected: no message
	}
}

func TestHandleWebhook_MissingSignatureRejected(t *testing.T) {
	b := New("127.0.0.1:0", "my-secret")
	defer b.srv.Shutdown(nil) //nolint:errcheck

	payload := `{"comment":{"body":"no signature at all"},"sender":{"login":"x"},"issue":{"number":1}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	// No X-Hub-Signature-256 header at all.
	rr := httptest.NewRecorder()
	b.handleWebhook(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rr.Code)
	}
}

func TestHandleWebhook_EmptySecretAllowsUnsigned(t *testing.T) {
	// The deliberate bypass case (see TestVerifySignature_EmptySecretBypasses)
	// exercised through the real handler.
	b := New("127.0.0.1:0", "")
	defer b.srv.Shutdown(nil) //nolint:errcheck

	payload := `{"comment":{"body":"unsigned but secret is unset"},"sender":{"login":"x"},"issue":{"number":7}}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	rr := httptest.NewRecorder()
	b.handleWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200 (no secret configured means no verification)", rr.Code)
	}
	select {
	case <-b.msgs:
	case <-time.After(time.Second):
		t.Fatal("expected a message when no secret is configured")
	}
}
