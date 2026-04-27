// v5.26.17 — operator-reported: hardcoding 127.0.0.1 breaks when the
// daemon binds to a specific non-loopback interface. loopbackBaseURL
// resolves the right base URL from the bind config; tests cover the
// matrix.

package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestLoopbackBaseURL_Default(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = ""
	cfg.Server.Port = 8080
	got := loopbackBaseURL(cfg)
	want := "http://127.0.0.1:8080"
	if got != want {
		t.Errorf("default empty host → %q, want %q", got, want)
	}
}

func TestLoopbackBaseURL_BindAll(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 9000
	got := loopbackBaseURL(cfg)
	want := "http://127.0.0.1:9000"
	if got != want {
		t.Errorf("0.0.0.0 → %q, want %q", got, want)
	}
}

func TestLoopbackBaseURL_BindSpecificIPv4(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = "192.168.1.5"
	cfg.Server.Port = 8080
	got := loopbackBaseURL(cfg)
	want := "http://192.168.1.5:8080"
	if got != want {
		// This is the operator's case — daemon bound to a specific
		// interface means loopback (127.0.0.1) is NOT what should be
		// used; we use the bound IP instead.
		t.Errorf("specific IPv4 → %q, want %q", got, want)
	}
}

func TestLoopbackBaseURL_BindIPv6All(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = "::"
	cfg.Server.Port = 8443
	got := loopbackBaseURL(cfg)
	want := "http://[::1]:8443"
	if got != want {
		t.Errorf(":: → %q, want %q", got, want)
	}
}

func TestLoopbackBaseURL_BindSpecificIPv6(t *testing.T) {
	cfg := &config.Config{}
	cfg.Server.Host = "fe80::1"
	cfg.Server.Port = 8080
	got := loopbackBaseURL(cfg)
	want := "http://[fe80::1]:8080"
	if got != want {
		t.Errorf("specific IPv6 → %q, want %q", got, want)
	}
}

func TestLoopbackBaseURL_DefaultPort(t *testing.T) {
	cfg := &config.Config{}
	got := loopbackBaseURL(cfg)
	want := "http://127.0.0.1:8080"
	if got != want {
		t.Errorf("zero port → %q, want %q", got, want)
	}
}

func TestLoopbackBaseURL_NilCfg(t *testing.T) {
	got := loopbackBaseURL(nil)
	want := "http://127.0.0.1:8080"
	if got != want {
		t.Errorf("nil cfg → %q, want %q (must not panic)", got, want)
	}
}
