// BL394 — SSRF fix for POST /api/push/register.
//
// WHAT WAS WRONG: handlePushRegister accepted any `endpoint` URL with zero
// validation, and publishToEndpoint later did a direct http.Post to it.
// Anyone able to register a push endpoint (CapCommWrite) could make the
// daemon POST to an arbitrary address it can reach — internal services,
// cloud metadata endpoints, etc. Found + triaged in
// docs/plans/2026-10-03-bl394-security-findings-review.md.
//
// WHY IT'S NOT A SIMPLE "BLOCK ALL PRIVATE IPs" FIX: coordinated with the
// datawatch-app agent (same node, discussion_id "push-endpoint-ssrf-review"
// in the memory/discussion WAL) before writing this, because a blanket
// private-IP block would have broken a real, currently-working feature.
// Two different registration shapes share this one endpoint:
//
//  1. SSE self-registration: body has `client_id` set, and `endpoint` is
//     the datawatch SERVER'S OWN base URL (the client just GETs the SSE
//     stream itself; the daemon never dials this). This is stored for
//     bookkeeping only — see isSSEMarkerRegistration below.
//  2. WebPush/UnifiedPush distributor registration: body has NO
//     `client_id`, just a user-typed `endpoint` the daemon actually POSTs
//     to (publishToEndpoint). THIS is the real SSRF surface, and its
//     legitimate values routinely include self-hosted ntfy/Gotify on a
//     LAN or Tailscale address (100.64.0.0/10) — sometimes over plain
//     http. Blocking private ranges by default would break that.
//
// THE FIX, split accordingly:
//   - client_id present  -> never passed to publishToEndpoint at all
//     (see the skip in publishToTopic/handlePushNotify). No SSRF exposure
//     because nothing ever dials it.
//   - client_id absent   -> validatePushEndpoint runs at registration
//     time (reject obviously-bad values fast, with a clear error), AND
//     the actual dial (pushHTTPClient) re-checks the resolved IP via a
//     net.Dialer.Control hook — this re-check-at-dial-time step is what
//     stops DNS rebinding (a hostname that resolves to a public IP at
//     registration time but to 169.254.169.254 by the time the daemon
//     actually connects).
//
// Loopback / link-local / cloud-metadata addresses are rejected ALWAYS,
// unconditionally. RFC1918 / Tailscale CGNAT / IPv6 ULA are rejected only
// when the operator opts into push.block_private_endpoints (default
// false, since that's where real self-hosted distributors live). Plain
// http:// is rejected unless push.allow_insecure_endpoints (default
// false).
//
// IF YOU HIT A PROBLEM WITH THIS LATER: the two things operators most
// likely report are (a) "my self-hosted ntfy/Gotify push stopped working"
// — check push.block_private_endpoints / push.allow_insecure_endpoints
// are set correctly for their setup, and (b) "SSE push stopped
// delivering" — check isSSEMarkerRegistration isn't misclassifying a real
// WebPush registration as an SSE marker (it's purely "does client_id look
// non-empty", nothing fancier).
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// isSSEMarkerRegistration reports whether a registration is the
// "deliver to me over SSE" bookkeeping entry rather than a real outbound
// push endpoint. Per datawatch-app (the actual producer of these
// registrations): client_id is only ever set for the SSE self-registration
// shape. Nothing about the Endpoint value itself is checked here — the
// client_id field alone is the signal.
func isSSEMarkerRegistration(r pushRegistration) bool {
	return r.ClientID != ""
}

// blockedIPReason returns a non-empty reason if ip must never be dialed,
// regardless of configuration. These categories have no legitimate use as
// a push endpoint under any operator setup.
func blockedIPReason(ip net.IP) string {
	switch {
	case ip.IsLoopback():
		return "loopback address"
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return "link-local address"
	case ip.IsUnspecified():
		return "unspecified address (0.0.0.0 / ::)"
	case ip.Equal(net.ParseIP("169.254.169.254")): // AWS/GCP/Azure metadata
		return "cloud metadata address"
	case ip.Equal(net.ParseIP("fd00:ec2::254")): // AWS IMDSv2 IPv6
		return "cloud metadata address (IPv6)"
	}
	return ""
}

// privateIPReason returns a non-empty reason if ip is in a private range
// that's blocked ONLY when the operator opts into
// push.block_private_endpoints. These are exactly the ranges real
// self-hosted push distributors (ntfy, Gotify) commonly live in, so they
// stay allowed by default.
func privateIPReason(ip net.IP) string {
	switch {
	case ip.IsPrivate(): // RFC1918 (10/8, 172.16/12, 192.168/16) + IPv6 ULA (fc00::/7)
		return "private-range address (RFC1918/ULA)"
	case isTailscaleCGNAT(ip):
		return "Tailscale CGNAT address (100.64.0.0/10)"
	}
	return ""
}

func isTailscaleCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	// 100.64.0.0/10
	return v4[0] == 100 && v4[1]&0xC0 == 64
}

// validatePushEndpoint checks a registration's Endpoint at registration
// time. It's a fast-fail for obviously-bad values with a clear error
// message back to the caller; the dial-time check in pushHTTPClient is
// the actual security boundary (this one can be bypassed by DNS
// rebinding between registration and first delivery).
func validatePushEndpoint(rawURL string, cfg config.PushConfig) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !cfg.AllowInsecureEndpoints) {
		if cfg.AllowInsecureEndpoints {
			return fmt.Errorf("endpoint must be http:// or https://, got %q", u.Scheme)
		}
		return fmt.Errorf("endpoint must be https:// (set push.allow_insecure_endpoints to allow http://), got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("endpoint has no host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// Can't resolve right now -- don't hard-fail registration over a
		// transient DNS blip. The dial-time check still applies to every
		// actual delivery attempt.
		return nil
	}
	for _, ip := range ips {
		if reason := blockedIPReason(ip); reason != "" {
			return fmt.Errorf("endpoint resolves to a %s (%s) -- not allowed", reason, ip)
		}
		if cfg.BlockPrivateEndpoints {
			if reason := privateIPReason(ip); reason != "" {
				return fmt.Errorf("endpoint resolves to a %s (%s) -- blocked by push.block_private_endpoints", reason, ip)
			}
		}
	}
	return nil
}

// newPushHTTPClient is a package var (not a plain func) so tests can swap
// in a plain, unguarded client for an httptest.NewServer loopback target
// -- production code always goes through the real SSRF-guarded one
// below. See push_ssrf_test.go / push_topics_test.go for the save-restore
// pattern already used for globalPushHub.registered.
var newPushHTTPClient = pushHTTPClient

// pushHTTPClient returns an http.Client whose dialer re-validates the
// resolved IP right before connecting -- this is the real SSRF boundary,
// not validatePushEndpoint above. A net.Dialer.Control hook runs after DNS
// resolution and before the socket connects, so a hostname that resolved
// safely at registration time but now resolves to a blocked address (DNS
// rebinding, or just a changed DNS record) is still caught on every send.
func pushHTTPClient(cfg config.PushConfig) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("push: could not parse resolved address %q", host)
		}
		if reason := blockedIPReason(ip); reason != "" {
			return fmt.Errorf("push: refusing to dial %s (%s)", reason, ip)
		}
		if cfg.BlockPrivateEndpoints {
			if reason := privateIPReason(ip); reason != "" {
				return fmt.Errorf("push: refusing to dial %s (%s), blocked by push.block_private_endpoints", reason, ip)
			}
		}
		return nil
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: transport}
}
