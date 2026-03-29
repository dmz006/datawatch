package dns

import (
	"strings"
	"testing"
)

func TestEncodeDecodeQueryRoundTrip(t *testing.T) {
	secret := "test-secret-key-32chars-minimum!"
	domain := "ctl.example.com"
	commands := []string{
		"list",
		"status abc1",
		"new: fix the auth bug in login.go",
		"send abc1: yes",
		"kill abc1",
		"tail abc1 20",
		"alerts 10",
		"version",
	}

	for _, cmd := range commands {
		qname, err := EncodeQuery(cmd, secret, domain)
		if err != nil {
			t.Fatalf("EncodeQuery(%q): %v", cmd, err)
		}

		// Verify structure
		if !strings.HasSuffix(qname, "."+domain+".") {
			t.Errorf("qname %q doesn't end with .%s.", qname, domain)
		}
		if !strings.Contains(qname, ".cmd.") {
			t.Errorf("qname %q missing .cmd. literal", qname)
		}

		// Decode
		decoded, err := DecodeQuery(qname, domain, secret)
		if err != nil {
			t.Fatalf("DecodeQuery(%q): %v", qname, err)
		}
		if decoded != cmd {
			t.Errorf("round-trip mismatch: got %q, want %q", decoded, cmd)
		}
	}
}

func TestDecodeQueryBadHMAC(t *testing.T) {
	secret := "correct-secret"
	domain := "ctl.example.com"

	qname, _ := EncodeQuery("list", secret, domain)

	// Try decoding with wrong secret
	_, err := DecodeQuery(qname, domain, "wrong-secret")
	if err == nil {
		t.Error("expected HMAC verification error, got nil")
	}
	if !strings.Contains(err.Error(), "HMAC") {
		t.Errorf("expected HMAC error, got: %v", err)
	}
}

func TestDecodeQueryDomainMismatch(t *testing.T) {
	secret := "test-secret"
	qname, _ := EncodeQuery("list", secret, "ctl.example.com")

	_, err := DecodeQuery(qname, "other.domain.com", secret)
	if err == nil {
		t.Error("expected domain mismatch error")
	}
}

func TestEncodeDecodeResponseRoundTrip(t *testing.T) {
	responses := []string{
		"No sessions.",
		"[host] Sessions:\n  [a1b2] running 14:30 Fix auth bug\n  [c3d4] waiting_input 14:25 Add tests",
		strings.Repeat("A", 1000), // long response
		"",
	}

	for _, resp := range responses {
		records := EncodeResponse(resp, 512)
		if len(records) == 0 {
			t.Errorf("EncodeResponse(%q) returned empty", resp[:min(len(resp), 20)])
			continue
		}

		decoded, err := DecodeResponse(records)
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}

		expected := resp
		if len(expected) > 512 {
			expected = expected[:512]
		}
		if decoded != expected {
			t.Errorf("response round-trip mismatch for input len %d", len(resp))
		}
	}
}

func TestEncodeResponseFragmentation(t *testing.T) {
	// Long response should produce multiple fragments
	resp := strings.Repeat("Hello World ", 100) // 1200 chars
	records := EncodeResponse(resp, 1200)

	if len(records) < 2 {
		t.Errorf("expected multiple fragments, got %d", len(records))
	}

	// Each record should have idx/total: prefix
	for _, r := range records {
		if !strings.Contains(r, "/") || !strings.Contains(r, ":") {
			t.Errorf("record missing index prefix: %q", r[:min(len(r), 30)])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
