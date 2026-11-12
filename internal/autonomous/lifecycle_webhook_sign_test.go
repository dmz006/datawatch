package autonomous

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	sigTestSecret = []byte("test-signing-secret-not-real")
	sigTestBody   = []byte(`{"version":1,"delivery_id":"0123456789abcdef0123456789abcdef"}`)
	sigTestNow    = time.Unix(1_800_000_000, 0).UTC()
)

func TestWebhookSignatureFormat(t *testing.T) {
	got := SignWebhookBody(sigTestSecret, sigTestNow, sigTestBody)
	re := regexp.MustCompile(`^t=1800000000,v1=[0-9a-f]{64}$`)
	if !re.MatchString(got) {
		t.Fatalf("header value %q does not match the documented format", got)
	}
	// Independent computation of HMAC-SHA256(secret, "<t>.<body>").
	m := hmac.New(sha256.New, sigTestSecret)
	m.Write([]byte("1800000000." + string(sigTestBody)))
	if want := "t=1800000000,v1=" + hex.EncodeToString(m.Sum(nil)); got != want {
		t.Errorf("SignWebhookBody = %q, want %q", got, want)
	}
	if again := SignWebhookBody(sigTestSecret, sigTestNow, sigTestBody); again != got {
		t.Error("signing must be deterministic for the same inputs")
	}
	if SignWebhookBody(sigTestSecret, sigTestNow.Add(time.Second), sigTestBody) == got {
		t.Error("a different timestamp must change the signature")
	}
	if SignWebhookBody(sigTestSecret, sigTestNow, nil) == got {
		t.Error("a different body must change the signature")
	}
}

func TestWebhookSignatureConstants(t *testing.T) {
	// Pending operator confirmation: this pins the proposal's recommended
	// values so a changed decision is one constant plus this expectation.
	if WebhookSignatureHeader != "X-Datawatch-Signature" ||
		WebhookDeliveryHeader != "X-Datawatch-Delivery" ||
		WebhookEventHeader != "X-Datawatch-Event" {
		t.Errorf("header names changed: %q %q %q", WebhookSignatureHeader, WebhookDeliveryHeader, WebhookEventHeader)
	}
	if WebhookSignatureTolerance != 300*time.Second {
		t.Errorf("tolerance = %v, want 5m", WebhookSignatureTolerance)
	}
}

func TestWebhookSignatureEmptySecret(t *testing.T) {
	if got := SignWebhookBody(nil, sigTestNow, sigTestBody); got != "" {
		t.Errorf("an empty secret must not produce a signature, got %q", got)
	}
	if got := SignWebhookBody([]byte{}, sigTestNow, sigTestBody); got != "" {
		t.Errorf("an empty secret must not produce a signature, got %q", got)
	}
	header := SignWebhookBody(sigTestSecret, sigTestNow, sigTestBody)
	if err := VerifyWebhookSignature(nil, header, sigTestBody, sigTestNow, time.Minute); !errors.Is(err, ErrWebhookSignatureNoSecret) {
		t.Errorf("verify with no secret: got %v", err)
	}
}

func TestWebhookSignatureRoundTrip(t *testing.T) {
	for _, body := range [][]byte{sigTestBody, {}, nil, []byte("not json at all"), []byte(strings.Repeat("x", 70000))} {
		header := SignWebhookBody(sigTestSecret, sigTestNow, body)
		if err := VerifyWebhookSignature(sigTestSecret, header, body, sigTestNow, WebhookSignatureTolerance); err != nil {
			t.Errorf("round trip failed for %d-byte body: %v", len(body), err)
		}
	}
}

func TestWebhookSignatureVerifyCases(t *testing.T) {
	good := SignWebhookBody(sigTestSecret, sigTestNow, sigTestBody)
	mac := good[strings.Index(good, "v1=")+3:]
	flipped := mac[:len(mac)-1] + map[bool]string{true: "1", false: "0"}[mac[len(mac)-1] == '0']
	tol := 5 * time.Minute

	tests := []struct {
		name      string
		secret    []byte
		header    string
		body      []byte
		now       time.Time
		tolerance time.Duration
		want      error
	}{
		{"valid", sigTestSecret, good, sigTestBody, sigTestNow, tol, nil},
		{"valid at the old edge", sigTestSecret, good, sigTestBody, sigTestNow.Add(tol), tol, nil},
		{"valid at the future edge", sigTestSecret, good, sigTestBody, sigTestNow.Add(-tol), tol, nil},
		{"zero tolerance uses the default", sigTestSecret, good, sigTestBody, sigTestNow.Add(WebhookSignatureTolerance), 0, nil},
		{"upper-case hex accepted", sigTestSecret, "t=1800000000,v1=" + strings.ToUpper(mac), sigTestBody, sigTestNow, tol, nil},
		{"spaces after comma accepted", sigTestSecret, strings.Replace(good, ",", ", ", 1), sigTestBody, sigTestNow, tol, nil},
		{"unknown extra field ignored", sigTestSecret, good + ",v2=abcd", sigTestBody, sigTestNow, tol, nil},
		{"field order does not matter", sigTestSecret, "v1=" + mac + ",t=1800000000", sigTestBody, sigTestNow, tol, nil},
		{"tampered body", sigTestSecret, good, append(append([]byte{}, sigTestBody...), ' '), sigTestNow, tol, ErrWebhookSignatureMismatch},
		{"empty body vs signed body", sigTestSecret, good, nil, sigTestNow, tol, ErrWebhookSignatureMismatch},
		{"wrong secret", []byte("another-secret"), good, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMismatch},
		{"signature flipped in last digit", sigTestSecret, "t=1800000000,v1=" + flipped, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMismatch},
		{"timestamp edited, signature kept", sigTestSecret, "t=1800000001,v1=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMismatch},
		{"stale by one second over tolerance", sigTestSecret, good, sigTestBody, sigTestNow.Add(tol + time.Second), tol, ErrWebhookSignatureStale},
		{"stale by a day", sigTestSecret, good, sigTestBody, sigTestNow.Add(24 * time.Hour), tol, ErrWebhookSignatureStale},
		{"future by one second over tolerance", sigTestSecret, good, sigTestBody, sigTestNow.Add(-tol - time.Second), tol, ErrWebhookSignatureFuture},
		{"future by a day", sigTestSecret, good, sigTestBody, sigTestNow.Add(-24 * time.Hour), tol, ErrWebhookSignatureFuture},
		{"empty header", sigTestSecret, "", sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"garbage header", sigTestSecret, "hello", sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"missing timestamp", sigTestSecret, "v1=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"missing signature", sigTestSecret, "t=1800000000", sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"duplicate timestamp", sigTestSecret, good + ",t=1800000000", sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"duplicate signature", sigTestSecret, good + ",v1=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"empty timestamp", sigTestSecret, "t=,v1=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"non-numeric timestamp", sigTestSecret, "t=abc,v1=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"negative timestamp", sigTestSecret, "t=-5,v1=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"signed timestamp", sigTestSecret, "t=+1800000000,v1=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"oversized timestamp", sigTestSecret, "t=99999999999999999999,v1=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"non-hex signature", sigTestSecret, "t=1800000000,v1=" + strings.Repeat("z", 64), sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"short signature", sigTestSecret, "t=1800000000,v1=" + mac[:62], sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"odd-length signature", sigTestSecret, "t=1800000000,v1=" + mac[:63], sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"long signature", sigTestSecret, "t=1800000000,v1=" + mac + "00", sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"empty signature", sigTestSecret, "t=1800000000,v1=", sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
		{"github style header", sigTestSecret, "sha256=" + mac, sigTestBody, sigTestNow, tol, ErrWebhookSignatureMalformed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyWebhookSignature(tc.secret, tc.header, tc.body, tc.now, tc.tolerance)
			if !errors.Is(err, tc.want) && (err != nil || tc.want != nil) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWebhookSignatureErrorsNeverLeakSecretOrHeader(t *testing.T) {
	secret := []byte("super-secret-value-123")
	header := SignWebhookBody(secret, sigTestNow, sigTestBody)
	mac := header[strings.Index(header, "v1=")+3:]
	attempts := []struct {
		secret []byte
		header string
		now    time.Time
	}{
		{[]byte("a-different-secret"), header, sigTestNow},
		{secret, header, sigTestNow.Add(time.Hour)},
		{secret, header, sigTestNow.Add(-time.Hour)},
		{secret, "t=" + "zz" + ",v1=" + mac, sigTestNow},
		{secret, "garbage", sigTestNow},
		{nil, header, sigTestNow},
	}
	for _, a := range attempts {
		err := VerifyWebhookSignature(a.secret, a.header, sigTestBody, a.now, time.Minute)
		if err == nil {
			t.Fatalf("expected an error for %q", a.header)
		}
		msg := err.Error()
		for _, leak := range []string{string(secret), "a-different-secret", mac, a.header} {
			if leak != "" && strings.Contains(msg, leak) {
				t.Errorf("error %q leaks %q", msg, leak)
			}
		}
	}
}

// TestWebhookSignatureConstantTimeComparePath documents and pins the
// constant-time comparison: the MAC is compared with hmac.Equal over decoded
// bytes, so a wrong signature that shares a long prefix with the right one is
// rejected the same way as a completely different one, and the verifying code
// does not compare MACs with ==, bytes.Equal or string comparison.
func TestWebhookSignatureConstantTimeComparePath(t *testing.T) {
	good := SignWebhookBody(sigTestSecret, sigTestNow, sigTestBody)
	mac := good[strings.Index(good, "v1=")+3:]
	for i := 0; i < len(mac); i += 7 {
		b := []byte(mac)
		if b[i] == 'a' {
			b[i] = 'b'
		} else {
			b[i] = 'a'
		}
		err := VerifyWebhookSignature(sigTestSecret, "t=1800000000,v1="+string(b), sigTestBody, sigTestNow, time.Minute)
		if !errors.Is(err, ErrWebhookSignatureMismatch) {
			t.Errorf("signature differing at index %d: got %v, want mismatch", i, err)
		}
	}

	src, err := os.ReadFile("lifecycle_webhook_sign.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(src)
	if !strings.Contains(code, "hmac.Equal(") {
		t.Error("verification must compare with hmac.Equal")
	}
	for _, banned := range []string{"bytes.Equal(", "== macStr", "macStr ==", "string(got) ==", "== string(got)"} {
		if strings.Contains(code, banned) {
			t.Errorf("verification must not use %q", banned)
		}
	}
}
