package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// BL316 S2 — daemonAPIURL/daemonHTTPClient used to resolve a --server name
// by scanning this CLI invocation's own YAML-loaded cfg.Servers, which
// can't see peers added to the running daemon's live server-store at
// runtime, and which held the remote's plaintext token in this process
// for no reason. They now always target the LOCAL daemon and let its
// /api/proxy/<name>/... passthrough resolve the live peer and inject its
// stored token server-side. These tests pin that routing.
func TestDaemonAPIURL_ServerFlagRoutesThroughLocalProxy(t *testing.T) {
	origName, origURL := serverName, serverURL
	t.Cleanup(func() { serverName, serverURL = origName, origURL })

	cfg := &config.Config{
		Servers: []config.RemoteServerConfig{
			{Name: "stale-yaml-only", URL: "http://stale.example:1", Enabled: true},
		},
	}
	cfg.Server.Port = 8443

	serverURL = ""
	serverName = "runtime-peer" // NOT present in cfg.Servers -- only in the live store
	got := daemonAPIURL(cfg)
	want := "http://localhost:8443/api/proxy/runtime-peer/api/command"
	if got != want {
		t.Errorf("daemonAPIURL(%q) = %q, want %q", serverName, got, want)
	}
}

func TestDaemonAPIURL_NoServerFlagTargetsLocalCommand(t *testing.T) {
	origName, origURL := serverName, serverURL
	t.Cleanup(func() { serverName, serverURL = origName, origURL })

	cfg := &config.Config{}
	cfg.Server.Port = 9000

	serverURL = ""
	serverName = ""
	got := daemonAPIURL(cfg)
	want := "http://localhost:9000/api/command"
	if got != want {
		t.Errorf("daemonAPIURL() = %q, want %q", got, want)
	}

	serverName = "local"
	got = daemonAPIURL(cfg)
	if got != want {
		t.Errorf("daemonAPIURL() with serverName=local = %q, want %q", got, want)
	}
}

func TestDaemonAPIURL_ExplicitURLFlagWins(t *testing.T) {
	origName, origURL := serverName, serverURL
	t.Cleanup(func() { serverName, serverURL = origName, origURL })

	cfg := &config.Config{}
	cfg.Server.Port = 9000

	serverURL = "http://explicit.example:1234/"
	serverName = "ignored"
	got := daemonAPIURL(cfg)
	want := "http://explicit.example:1234/api/command"
	if got != want {
		t.Errorf("daemonAPIURL() = %q, want %q", got, want)
	}
}

func TestDaemonHTTPClient_ServerFlagUsesLocalToken(t *testing.T) {
	origName, origURL := serverName, serverURL
	t.Cleanup(func() { serverName, serverURL = origName, origURL })

	cfg := &config.Config{
		Servers: []config.RemoteServerConfig{
			{Name: "runtime-peer", URL: "http://peer.example", Token: "peer-secret-token", Enabled: true},
		},
	}
	cfg.Server.Token = "local-admin-token"

	serverURL = ""
	serverName = "runtime-peer"
	_, token := daemonHTTPClient(cfg)
	if token != "local-admin-token" {
		t.Errorf("daemonHTTPClient token = %q, want the local admin token (%q) -- the remote's own token must never leave this process; the local proxy injects it server-side", token, "local-admin-token")
	}
}
