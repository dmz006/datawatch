// Per-listener authentication posture for the startup warning.
//
// A listener with an empty credential is a legitimate operator choice
// (a loopback-only dev setup), but nothing in the config file tells the
// operator which listeners are open to non-loopback callers. The
// helpers here compute that posture from the config alone so the daemon
// can name each exposed-and-unauthenticated listener at startup, and so
// the operator can explicitly acknowledge that posture with
// server.acknowledge_unauthenticated instead of reading the warnings and
// choosing to ignore them.
package config

import (
	"fmt"
	"io"
	"net"
	"strings"
)

// ListenerAuthState describes one daemon listener's authentication
// posture as seen from the outside.
type ListenerAuthState struct {
	// Name is the listener's stable name: rest, rest_tls, mcp_sse,
	// proxy_sandbox, webhook, github_webhook or twilio_webhook.
	Name string
	// Address is the bind host:port as configured.
	Address string
	// Enabled reports whether the listener starts at all.
	Enabled bool
	// AuthRequired reports whether the listener enforces a credential.
	AuthRequired bool
	// Loopback reports whether the bind host is a loopback address.
	Loopback bool
	// Acknowledged mirrors server.acknowledge_unauthenticated for this
	// listener, for every listener.
	Acknowledged bool
}

// Exposed reports whether the listener is on, reachable from outside
// loopback, and enforces no credential.
func (s ListenerAuthState) Exposed() bool {
	return s.Enabled && !s.AuthRequired && !s.Loopback
}

// isLoopbackHost reports whether host is a loopback bind address:
// 127.0.0.1, ::1 or localhost, compared case-insensitively. An empty
// host, 0.0.0.0, ::, LAN addresses and other hostnames are not loopback.
func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// bindHost extracts the host part of an address that may or may not
// carry a port ("host:port", "[v6]:port", ":port", a bare hostname or
// an empty string).
func bindHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// ListenerAuthStates returns one auth-posture entry per listener, in a
// fixed order: rest, rest_tls, mcp_sse, proxy_sandbox, webhook,
// github_webhook, twilio_webhook.
func ListenerAuthStates(cfg *Config) []ListenerAuthState {
	acknowledged := cfg.Server.AcknowledgeUnauthenticated
	serverToken := cfg.Server.Token != ""
	return []ListenerAuthState{
		{
			Name:         "rest",
			Address:      net.JoinHostPort(cfg.Server.Host, fmt.Sprint(cfg.Server.Port)),
			Enabled:      cfg.Server.Enabled,
			AuthRequired: serverToken,
			Loopback:     isLoopbackHost(cfg.Server.Host),
			Acknowledged: acknowledged,
		},
		{
			Name:         "rest_tls",
			Address:      net.JoinHostPort(cfg.Server.Host, fmt.Sprint(cfg.Server.TLSPort)),
			Enabled:      cfg.Server.Enabled && cfg.Server.TLSEnabled,
			AuthRequired: serverToken,
			Loopback:     isLoopbackHost(cfg.Server.Host),
			Acknowledged: acknowledged,
		},
		{
			Name:    "mcp_sse",
			Address: net.JoinHostPort(cfg.MCP.SSEHost, fmt.Sprint(cfg.MCP.SSEPort)),
			Enabled: cfg.MCP.SSEEnabled,
			// The SSE transport falls back to the server token when
			// its own token is empty (see internal/mcp/server.go).
			AuthRequired: cfg.MCP.Token != "" || serverToken,
			Loopback:     isLoopbackHost(cfg.MCP.SSEHost),
			Acknowledged: acknowledged,
		},
		// proxy_sandbox starts together with the main server, bound on
		// the same host at ProxySandboxPort; it is gated by
		// ProxySandboxPort > 0 and its routes pass through the
		// federation auth middleware, so the credential it enforces is
		// the server token itself — the same gate as rest/rest_tls.
		{
			Name:         "proxy_sandbox",
			Address:      net.JoinHostPort(cfg.Server.Host, fmt.Sprint(cfg.Server.ProxySandboxPort)),
			Enabled:      cfg.Server.Enabled && cfg.Server.ProxySandboxPort > 0,
			AuthRequired: serverToken,
			Loopback:     isLoopbackHost(cfg.Server.Host),
			Acknowledged: acknowledged,
		},
		{
			Name:         "webhook",
			Address:      cfg.Webhook.Addr,
			Enabled:      cfg.Webhook.Enabled,
			AuthRequired: cfg.Webhook.Token != "",
			Loopback:     isLoopbackHost(bindHost(cfg.Webhook.Addr)),
			Acknowledged: acknowledged,
		},
		{
			Name:         "github_webhook",
			Address:      cfg.GitHubWebhook.Addr,
			Enabled:      cfg.GitHubWebhook.Enabled,
			AuthRequired: cfg.GitHubWebhook.Secret != "",
			Loopback:     isLoopbackHost(bindHost(cfg.GitHubWebhook.Addr)),
			Acknowledged: acknowledged,
		},
		{
			Name:         "twilio_webhook",
			Address:      cfg.Twilio.WebhookAddr,
			Enabled:      cfg.Twilio.Enabled,
			AuthRequired: cfg.Twilio.WebhookPublicURL != "",
			Loopback:     isLoopbackHost(bindHost(cfg.Twilio.WebhookAddr)),
			Acknowledged: acknowledged,
		},
	}
}

// LogUnauthenticatedListeners writes one startup warning line for each
// state that is exposed and not acknowledged.
func LogUnauthenticatedListeners(w io.Writer, states []ListenerAuthState) {
	for _, st := range states {
		if !st.Exposed() || st.Acknowledged {
			continue
		}
		fmt.Fprintf(w, "[warn] listener %s on %s accepts requests without authentication; set a token or bind to loopback, or set server.acknowledge_unauthenticated to acknowledge.\n",
			st.Name, st.Address)
	}
}
