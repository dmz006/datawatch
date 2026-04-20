// BL1 — IPv6-safe listen-address composition.

package server

import "testing"

func TestBL1_JoinHostPort_IPv4(t *testing.T) {
	got := joinHostPort("127.0.0.1", 8080)
	if got != "127.0.0.1:8080" {
		t.Errorf("got %q want 127.0.0.1:8080", got)
	}
}

func TestBL1_JoinHostPort_IPv6Literal(t *testing.T) {
	got := joinHostPort("::1", 8080)
	if got != "[::1]:8080" {
		t.Errorf("got %q want [::1]:8080 (IPv6 literals must be bracketed)", got)
	}
}

func TestBL1_JoinHostPort_DualStack(t *testing.T) {
	got := joinHostPort("::", 443)
	if got != "[::]:443" {
		t.Errorf("got %q want [::]:443", got)
	}
}

func TestBL1_JoinHostPort_Hostname(t *testing.T) {
	got := joinHostPort("api.example.com", 80)
	if got != "api.example.com:80" {
		t.Errorf("got %q want api.example.com:80", got)
	}
}

func TestBL1_JoinHostPort_AnyV4(t *testing.T) {
	got := joinHostPort("0.0.0.0", 8443)
	if got != "0.0.0.0:8443" {
		t.Errorf("got %q want 0.0.0.0:8443", got)
	}
}
