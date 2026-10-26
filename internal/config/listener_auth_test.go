package config

import (
	"bytes"
	"strings"
	"testing"
)

func byName(t *testing.T, states []ListenerAuthState, name string) ListenerAuthState {
	t.Helper()
	for _, st := range states {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("listener %q not found in %v", name, namesOf(states))
	return ListenerAuthState{}
}

func namesOf(states []ListenerAuthState) []string {
	out := make([]string, 0, len(states))
	for _, st := range states {
		out = append(out, st.Name)
	}
	return out
}

// listenerExposedConfig returns a config where the target listener is
// enabled, bound non-loopback, and has no credential set, so it is
// exposed under every rule.
func listenerExposedConfig(name string) *Config {
	cfg := &Config{}
	switch name {
	case "rest", "rest_tls", "proxy_sandbox":
		cfg.Server.Enabled = true
		cfg.Server.Host = "0.0.0.0"
		cfg.Server.Port = 8080
		cfg.Server.TLSEnabled = name == "rest_tls"
		cfg.Server.TLSPort = 8443
		cfg.Server.ProxySandboxPort = 8444
	case "mcp_sse":
		cfg.MCP.SSEEnabled = true
		cfg.MCP.SSEHost = "0.0.0.0"
		cfg.MCP.SSEPort = 8081
	case "webhook":
		cfg.Webhook.Enabled = true
		cfg.Webhook.Addr = "0.0.0.0:9001"
	case "github_webhook":
		cfg.GitHubWebhook.Enabled = true
		cfg.GitHubWebhook.Addr = "0.0.0.0:9002"
	case "twilio_webhook":
		cfg.Twilio.Enabled = true
		cfg.Twilio.WebhookAddr = "0.0.0.0:9003"
	default:
		panic("unknown listener " + name)
	}
	return cfg
}

func TestListenerAuthOrderAndShape(t *testing.T) {
	states := ListenerAuthStates(&Config{})
	want := []string{"rest", "rest_tls", "mcp_sse", "proxy_sandbox", "webhook", "github_webhook", "twilio_webhook"}
	if len(states) != len(want) {
		t.Fatalf("got %d entries %v, want %d", len(states), namesOf(states), len(want))
	}
	for i, n := range want {
		if states[i].Name != n {
			t.Fatalf("entry %d = %q, want %q (full order %v)", i, states[i].Name, n, namesOf(states))
		}
		if states[i].Acknowledged {
			t.Fatalf("%s: Acknowledged = true, want false (acknowledgement unset)", n)
		}
	}
}

func TestListenerAuthExposedNonLoopbackUnauthenticated(t *testing.T) {
	for _, name := range []string{"rest", "rest_tls", "mcp_sse", "proxy_sandbox", "webhook", "github_webhook", "twilio_webhook"} {
		cfg := listenerExposedConfig(name)
		st := byName(t, ListenerAuthStates(cfg), name)
		if !st.Enabled {
			t.Errorf("%s: Enabled = false, want true", name)
			continue
		}
		if st.Loopback {
			t.Errorf("%s: Loopback = true on non-loopback bind, want false", name)
		}
		if st.AuthRequired {
			t.Errorf("%s: AuthRequired = true with no credential configured", name)
		}
		if !st.Exposed() {
			t.Errorf("%s: Exposed() = false, want true (enabled, non-loopback, unauthenticated)", name)
		}
	}
}

func TestListenerAuthLoopbackNotExposed(t *testing.T) {
	cases := map[string]func(*Config){
		"rest": func(c *Config) {
			c.Server.Host = "127.0.0.1"
		},
		"rest_tls": func(c *Config) {
			c.Server.TLSEnabled = true
			c.Server.Host = "localhost"
		},
		"mcp_sse": func(c *Config) {
			c.MCP.SSEHost = "::1"
		},
		"proxy_sandbox": func(c *Config) {
			c.Server.Host = "LOCALHOST"
		},
		"webhook": func(c *Config) {
			c.Webhook.Addr = "127.0.0.1:9001"
		},
		"github_webhook": func(c *Config) {
			c.GitHubWebhook.Addr = "localhost:9002"
		},
		"twilio_webhook": func(c *Config) {
			c.Twilio.WebhookAddr = "[::1]:9003"
		},
	}
	for name, mutate := range cases {
		cfg := listenerExposedConfig(name)
		mutate(cfg)
		st := byName(t, ListenerAuthStates(cfg), name)
		if !st.Loopback {
			t.Errorf("%s: Loopback = false on a loopback bind, want true", name)
			continue
		}
		if st.Exposed() {
			t.Errorf("%s: Exposed() = true while bound to loopback, want false", name)
		}
	}
}

func TestListenerAuthTokenProtectedNotExposed(t *testing.T) {
	cases := map[string]func(*Config){
		"rest": func(c *Config) {
			c.Server.Token = "rest-token"
		},
		"rest_tls": func(c *Config) {
			c.Server.TLSEnabled = true
			c.Server.Token = "tls-token"
		},
		"mcp_sse": func(c *Config) {
			c.MCP.Token = "sse-token"
		},
		"proxy_sandbox": func(c *Config) {
			c.Server.Token = "sandbox-token"
		},
		"webhook": func(c *Config) {
			c.Webhook.Token = "wh-token"
		},
		"github_webhook": func(c *Config) {
			c.GitHubWebhook.Secret = "gh-secret"
		},
		"twilio_webhook": func(c *Config) {
			c.Twilio.WebhookPublicURL = "https://sms.example/sms"
		},
	}
	for name, mutate := range cases {
		cfg := listenerExposedConfig(name)
		mutate(cfg)
		st := byName(t, ListenerAuthStates(cfg), name)
		if !st.AuthRequired {
			t.Errorf("%s: AuthRequired = false with its credential set", name)
			continue
		}
		if st.Exposed() {
			t.Errorf("%s: Exposed() = true while a credential is enforced, want false", name)
		}
	}
}

func TestListenerAuthMCPFallsBackToServerToken(t *testing.T) {
	cfg := listenerExposedConfig("mcp_sse")
	cfg.MCP.Token = ""
	cfg.Server.Token = "server-token"
	st := byName(t, ListenerAuthStates(cfg), "mcp_sse")
	if !st.AuthRequired {
		t.Fatalf("mcp_sse: AuthRequired = false with only server.token set, want true (SSE falls back to the server token)")
	}
	if st.Exposed() {
		t.Fatalf("mcp_sse: Exposed() = true while the server token protects it, want false")
	}
}

func TestListenerAuthDisabledNeverExposed(t *testing.T) {
	// The empty config enables nothing: every listener disabled, none exposed.
	states := ListenerAuthStates(&Config{})
	for _, st := range states {
		if st.Enabled {
			t.Errorf("%s: Enabled = true with its enable flag unset, want false", st.Name)
		}
		if st.Exposed() {
			t.Errorf("%s: Exposed() = true while disabled, want false", st.Name)
		}
	}

	// Explicitly: enabling a sibling must not leak into another listener.
	cfg := &Config{}
	cfg.Server.Enabled = true
	cfg.Server.Host = "0.0.0.0"
	cfg.MCP.SSEEnabled = true // rest on, mcp_sse on — but rest_tls / proxy_sandbox still off
	states = ListenerAuthStates(cfg)
	if byName(t, states, "rest_tls").Enabled {
		t.Errorf("rest_tls: Enabled = true with tls_enabled unset, want false")
	}
	if byName(t, states, "proxy_sandbox").Enabled {
		t.Errorf("proxy_sandbox: Enabled = true with proxy_sandbox_port unset, want false")
	}
	// proxy_sandbox gated on the port:
	cfg.Server.Enabled = true
	cfg.Server.Host = "0.0.0.0"
	states = ListenerAuthStates(cfg)
	if byName(t, states, "proxy_sandbox").Enabled || byName(t, states, "proxy_sandbox").Exposed() {
		t.Errorf("proxy_sandbox: enabled/exposed with proxy_sandbox_port = 0, want disabled")
	}
}

func TestListenerAuthWriterListsOnlyExposedUnacknowledged(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Enabled: true, Host: "0.0.0.0", Port: 8080},
		MCP:    MCPConfig{SSEEnabled: true, SSEHost: "0.0.0.0", SSEPort: 8081},
		Webhook: WebhookConfig{
			Enabled: true, Addr: "0.0.0.0:9001", // exposed
		},
		GitHubWebhook: GitHubWebhookConfig{
			Enabled: true, Addr: "127.0.0.1:9002", // loopback, not exposed
		},
		Twilio: TwilioConfig{
			Enabled:     true,
			WebhookAddr: "0.0.0.0:9003", // no signature URL set -> would be...
		},
	}
	cfg.Twilio.WebhookPublicURL = "https://sms.example/sms" // protected

	var buf bytes.Buffer
	LogUnauthenticatedListeners(&buf, ListenerAuthStates(cfg))
	got := buf.String()

	for _, want := range []string{"listener rest on 0.0.0.0:8080", "listener mcp_sse on 0.0.0.0:8081", "listener webhook on 0.0.0.0:9001"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\noutput:\n%s", want, got)
		}
	}
	for _, absent := range []string{"listener rest_tls", "listener proxy_sandbox", "listener github_webhook", "listener twilio_webhook"} {
		if strings.Contains(got, absent) {
			t.Errorf("output unexpectedly contains %q\noutput:\n%s", absent, got)
		}
	}
}

func TestListenerAuthAcknowledgedSuppressesWriter(t *testing.T) {
	cfg := listenerExposedConfig("rest")
	cfg.MCP.SSEEnabled = true
	cfg.MCP.SSEHost = "0.0.0.0"

	unacked := bytes.Buffer{}
	LogUnauthenticatedListeners(&unacked, ListenerAuthStates(cfg))
	if !strings.Contains(unacked.String(), "listener rest on 0.0.0.0:8080") {
		t.Fatalf("without acknowledgement the warning is missing:\n%s", unacked.String())
	}

	cfg.Server.AcknowledgeUnauthenticated = true
	states := ListenerAuthStates(cfg)
	for _, st := range states {
		if !st.Acknowledged {
			t.Fatalf("%s: Acknowledged = false after server.acknowledge_unauthenticated is set", st.Name)
		}
	}
	acked := bytes.Buffer{}
	LogUnauthenticatedListeners(&acked, states)
	if acked.Len() != 0 {
		t.Fatalf("acknowledgement did not silence the warnings:\n%s", acked.String())
	}
}

func TestListenerAuthDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("default Server.Host = %q, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.MCP.SSEHost != "127.0.0.1" {
		t.Fatalf("default MCP.SSEHost = %q, want 127.0.0.1", cfg.MCP.SSEHost)
	}
	if cfg.Server.Token != "" {
		t.Fatalf("default Server.Token = %q, want empty", cfg.Server.Token)
	}
	if cfg.MCP.Token != "" {
		t.Fatalf("default MCP.Token = %q, want empty", cfg.MCP.Token)
	}
	if cfg.Server.AcknowledgeUnauthenticated {
		t.Fatalf("default AcknowledgeUnauthenticated = true, want false")
	}
}
