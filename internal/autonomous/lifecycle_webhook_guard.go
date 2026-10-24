// BL427 (phase 5) — target guard for the lifecycle outbound webhook sink.
// Design: docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md
// (section 3: URL scheme, allow-list match, SSRF guard, DNS rebinding,
// redirects).
//
// PURPOSE: a webhook subscription makes the daemon POST lifecycle events
// to an address the operator typed. Without a guard that is a server-side
// request forgery primitive: anyone able to create a subscription could
// aim the daemon at an internal service or a cloud credential endpoint.
// This file is the guard. It has three layers, each usable on its own:
//
//  1. WebhookHostAllowed — the operator's allow-list. Exact host or exact
//     host:port, case-insensitive, IDNA and trailing-dot normalised, no
//     wildcards, no suffix match. An empty list allows nothing.
//  2. ValidateWebhookURL — creation-time and per-delivery URL check:
//     https (http only for an allow-listed host), no userinfo, a real
//     host, no obfuscated IP spelling, host on the allow-list. Errors are
//     operator-readable and never echo the raw URL (it can carry a
//     secret in userinfo or the query string).
//  3. NewWebhookHTTPClient — the actual security boundary. The dialer's
//     Control hook re-checks the resolved IP at connect time on every
//     attempt (this stops DNS rebinding: a name that resolved safely at
//     creation but points at a blocked address now), redirects are never
//     followed, no proxy is used, and the response body is read-capped.
//
// IP classification: loopback, link-local, unspecified, multicast, private
// (RFC 1918 / ULA) and Tailscale CGNAT addresses are refused unless the
// host is explicitly allow-listed (name or literal IP). Cloud metadata
// addresses stay refused even when allow-listed — see
// webhookMetadataAlwaysRefused. Trade-off to keep in mind: allow-listing a
// NAME trusts that name's DNS; if the operator wants a name that may only
// ever resolve to public addresses they must not allow-list a private one.
//
// The classification follows internal/server/push_ssrf.go (BL394) but is
// written here on purpose: this package must not import internal/server or
// internal/session (import cycle).

package autonomous

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/idna"
)

// webhookMetadataAlwaysRefused keeps the cloud metadata addresses
// (169.254.169.254, fd00:ec2::254) refused even when their host is on the
// allow-list. Pending operator confirmation: the proposal recommends this
// deliberate narrowing of the "unless explicitly allow-listed" rule because
// no legitimate event receiver lives there and an allow-list typo must not
// turn the daemon into a credential fetcher. Set to false to honour the
// allow-list for every address.
const webhookMetadataAlwaysRefused = true

// webhookDefaultAttemptTimeout is used by NewWebhookHTTPClient when the
// caller passes a non-positive timeout. Pending operator confirmation (the
// proposal's per-attempt timeout, 10s).
const webhookDefaultAttemptTimeout = 10 * time.Second

// webhookMaxResponseBytes caps how much of a receiver's response body the
// client will ever hand back to the caller. The body is only used for
// diagnostics; a hostile receiver must not make the daemon read an
// unbounded stream.
const webhookMaxResponseBytes = 64 * 1024

// ErrWebhookGuard is wrapped by every refusal this file produces, at
// validation time and at dial time, so a delivery loop can classify the
// failure (error_class "guard") with errors.Is and not retry it.
var ErrWebhookGuard = errors.New("webhook target refused")

func webhookGuardErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrWebhookGuard, fmt.Sprintf(format, args...))
}

// webhookResolver is the lookup the guarded dialer uses. net.DefaultResolver
// satisfies it; tests inject a fake to simulate DNS rebinding.
type webhookResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// ---------------------------------------------------------------------------
// Host normalisation
// ---------------------------------------------------------------------------

// normaliseWebhookHost returns the canonical spelling of a bare host (no
// port): lower-case, IDNA (punycode) form, one trailing dot removed, IPv6
// compressed, IPv4 in dotted-quad. obfuscated reports an IP literal that was
// written in a non-canonical way — decimal / octal / hex / short IPv4 forms
// ("2130706433", "0177.0.0.1", "0x7f.1", "127.1") or an IPv4-mapped IPv6
// address ("::ffff:127.0.0.1"). Those are the classic ways to slip a
// loopback or metadata address past a string comparison, so callers refuse
// them rather than trust the normalised form.
func normaliseWebhookHost(h string) (host string, obfuscated bool, err error) {
	if strings.TrimSpace(h) != h || h == "" {
		return "", false, errors.New("host is empty or has surrounding whitespace")
	}
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	if strings.ContainsAny(h, "%/\\@?#*") {
		return "", false, errors.New("host contains a character that is not allowed")
	}
	for _, r := range h {
		if r < 0x20 || r == 0x7f {
			return "", false, errors.New("host contains a control character")
		}
	}
	h = strings.ToLower(h)
	h = strings.TrimSuffix(h, ".")
	if h == "" || strings.HasSuffix(h, ".") || strings.HasPrefix(h, ".") || strings.Contains(h, "..") {
		return "", false, errors.New("host is empty or malformed")
	}

	// IPv6 literal.
	if strings.Contains(h, ":") {
		ip := net.ParseIP(h)
		if ip == nil {
			return "", false, errors.New("host is not a valid IPv6 address")
		}
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), true, nil // IPv4-mapped
		}
		return ip.String(), false, nil
	}

	// A host whose last label is a number is an IPv4 spelling (inet_aton
	// semantics, which the system resolver honours even though Go's parser
	// does not).
	labels := strings.Split(h, ".")
	if isNumericLabel(labels[len(labels)-1]) {
		ip, ok := parseLegacyIPv4(labels)
		if !ok {
			return "", false, errors.New("host looks like a numeric address but is not a valid one")
		}
		canonical := ip.String()
		return canonical, canonical != h, nil
	}

	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil || ascii == "" {
		return "", false, errors.New("host is not a valid domain name")
	}
	return ascii, false, nil
}

// isNumericLabel reports whether a host's last label is all decimal digits
// or a 0x-prefixed hex number.
func isNumericLabel(l string) bool {
	if l == "" {
		return false
	}
	if strings.HasPrefix(l, "0x") {
		for _, c := range l[2:] {
			if !isHexDigit(c) {
				return false
			}
		}
		return true
	}
	for _, c := range l {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isHexDigit(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

// parseLegacyIPv4 parses the 1-4 part inet_aton forms: each part decimal,
// 0-prefixed octal or 0x-prefixed hex; the last part fills the remaining
// bytes.
func parseLegacyIPv4(parts []string) (net.IP, bool) {
	if len(parts) == 0 || len(parts) > 4 {
		return nil, false
	}
	vals := make([]uint64, len(parts))
	for i, p := range parts {
		base := 10
		s := p
		switch {
		case strings.HasPrefix(p, "0x"):
			base, s = 16, p[2:]
			if s == "" {
				s = "0"
			}
		case len(p) > 1 && p[0] == '0':
			base, s = 8, p[1:]
		}
		v, err := strconv.ParseUint(s, base, 32)
		if err != nil {
			return nil, false
		}
		vals[i] = v
	}
	last := vals[len(vals)-1]
	if last >= uint64(1)<<(8*uint(5-len(vals))) {
		return nil, false
	}
	var addr uint64
	for i := 0; i < len(vals)-1; i++ {
		if vals[i] > 255 {
			return nil, false
		}
		addr |= vals[i] << (8 * uint(3-i))
	}
	addr |= last
	return net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr)).To4(), true
}

// parseWebhookHostPort splits "host", "host:port", "[v6]" or "[v6]:port"
// (and a bare IPv6 literal) into a canonical host and an optional port.
// ok is false for anything invalid, wildcard or obfuscated.
func parseWebhookHostPort(s string) (host, port string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t*/") {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(s, "["):
		if i := strings.LastIndex(s, "]"); i > 0 && i < len(s)-1 {
			if s[i+1] != ':' {
				return "", "", false
			}
			host, port = s[1:i], s[i+2:]
			if port == "" {
				return "", "", false
			}
		} else {
			host = s
		}
	case strings.Count(s, ":") == 1:
		i := strings.Index(s, ":")
		host, port = s[:i], s[i+1:]
		if port == "" {
			return "", "", false
		}
	default:
		host = s // plain name, IPv4, or a bare IPv6 literal
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return "", "", false
		}
	}
	canonical, obfuscated, err := normaliseWebhookHost(host)
	if err != nil || obfuscated {
		return "", "", false
	}
	return canonical, port, true
}

// ---------------------------------------------------------------------------
// Allow-list and URL validation
// ---------------------------------------------------------------------------

// WebhookHostAllowed reports whether host is on the operator's allow-list.
// host is "name", "name:port", an IP literal, or "[v6]:port". An allow entry
// is an exact host (matches that host on any port) or an exact host:port
// (matches only that port). Matching is case-insensitive and compared after
// IDNA and trailing-dot normalisation; there are no wildcards and no suffix
// matching. An empty (or all-invalid) list allows nothing. A host with no
// port never matches a port-qualified entry. Obfuscated IP spellings never
// match anything.
func WebhookHostAllowed(host string, allow []string) bool {
	h, p, ok := parseWebhookHostPort(host)
	if !ok {
		return false
	}
	for _, entry := range allow {
		eh, ep, ok := parseWebhookHostPort(entry)
		if !ok || eh != h {
			continue
		}
		if ep == "" || ep == p {
			return true
		}
	}
	return false
}

// ValidateWebhookURL parses raw and applies the URL policy: https only (http
// only when the host is explicitly allow-listed), no userinfo, a non-empty
// host in a canonical spelling, and the host (with its effective port) on
// the allow-list. The returned URL has its host in canonical form. Errors
// never echo raw (it may carry credentials in userinfo or the query).
func ValidateWebhookURL(raw string, allow []string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, webhookGuardErr("the webhook URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, webhookGuardErr("the webhook URL could not be parsed")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return nil, webhookGuardErr("the URL scheme must be https (http only for an allow-listed host)")
	}
	if u.User != nil {
		return nil, webhookGuardErr("the URL must not contain a user name or password")
	}
	if u.Opaque != "" {
		return nil, webhookGuardErr("the URL must have the form %s://host/path", scheme)
	}
	if u.Hostname() == "" {
		return nil, webhookGuardErr("the URL has no host")
	}
	host, obfuscated, err := normaliseWebhookHost(u.Hostname())
	if err != nil {
		return nil, webhookGuardErr("the URL host is not valid: %v", err)
	}
	if obfuscated {
		return nil, webhookGuardErr("the URL host %q is a non-canonical IP spelling; write the address as %q", u.Hostname(), host)
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if scheme == "http" {
			port = "80"
		}
	} else if n, perr := strconv.Atoi(port); perr != nil || n < 1 || n > 65535 {
		return nil, webhookGuardErr("the URL port is not valid")
	}
	hostPort := net.JoinHostPort(host, port)
	if len(allow) == 0 {
		return nil, webhookGuardErr("the webhook host allow-list is empty, so no host can receive events")
	}
	if !WebhookHostAllowed(hostPort, allow) {
		if scheme == "http" {
			return nil, webhookGuardErr("host %s is not on the allow-list; http is accepted only for an allow-listed host", hostPort)
		}
		return nil, webhookGuardErr("host %s is not on the webhook allow-list", hostPort)
	}
	if ip := net.ParseIP(host); ip != nil {
		if reason := webhookBlockedIPReason(ip, true); reason != "" {
			return nil, webhookGuardErr("%s is a %s", host, reason)
		}
	}
	out := *u
	out.Scheme = scheme
	if u.Port() != "" {
		out.Host = hostPort
	} else if strings.Contains(host, ":") {
		out.Host = "[" + host + "]"
	} else {
		out.Host = host
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// IP classification
// ---------------------------------------------------------------------------

// webhookBlockedIPReason returns a non-empty reason if ip must not be dialed.
// Cloud metadata addresses are refused when webhookMetadataAlwaysRefused is
// set, whatever explicitlyAllowed says; every other blocked category (
// loopback, link-local, unspecified, multicast, private RFC 1918 / ULA,
// Tailscale CGNAT) is refused unless explicitlyAllowed.
func webhookBlockedIPReason(ip net.IP, explicitlyAllowed bool) string {
	if ip == nil {
		return "invalid address"
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	} else if len(ip) == net.IPv6len && isNAT64(ip) {
		// 64:ff9b::/96 embeds an IPv4 address; classify what it points at.
		ip = net.IP(ip[12:16])
	}
	if reason := webhookMetadataReason(ip); reason != "" && (webhookMetadataAlwaysRefused || !explicitlyAllowed) {
		return reason
	}
	if explicitlyAllowed {
		return ""
	}
	switch {
	case ip.IsLoopback():
		return "loopback address"
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast():
		return "link-local address"
	case ip.IsUnspecified():
		return "unspecified address (0.0.0.0 / ::)"
	case ip.IsMulticast():
		return "multicast address"
	case ip.IsPrivate():
		return "private-range address (RFC1918/ULA)"
	case isWebhookCGNAT(ip):
		return "Tailscale CGNAT address (100.64.0.0/10)"
	}
	return ""
}

func webhookMetadataReason(ip net.IP) string {
	switch {
	case ip.Equal(net.ParseIP("169.254.169.254")):
		return "cloud metadata address"
	case ip.Equal(net.ParseIP("fd00:ec2::254")):
		return "cloud metadata address (IPv6)"
	}
	return ""
}

func isWebhookCGNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xC0 == 64 // 100.64.0.0/10
}

// isNAT64 reports whether ip is in the well-known NAT64 prefix 64:ff9b::/96.
func isNAT64(ip net.IP) bool {
	if len(ip) != net.IPv6len || ip[0] != 0x00 || ip[1] != 0x64 || ip[2] != 0xff || ip[3] != 0x9b {
		return false
	}
	for _, b := range ip[4:12] {
		if b != 0 {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Guarded HTTP client
// ---------------------------------------------------------------------------

// NewWebhookHTTPClient returns the client the webhook sink must use. Every
// connection is made by a dialer that (1) refuses a host that is not on the
// allow-list, (2) resolves the name itself, and (3) re-checks each resolved
// IP in the dialer's Control hook right before the socket connects, so a
// name that was safe when the subscription was created but now points at a
// blocked address (DNS rebinding) is refused on every attempt. Keep-alives
// are off so no attempt can reuse a connection that skipped that check.
// Redirects are never followed (the 3xx response is returned as is), no
// proxy is used, and the response body is capped at webhookMaxResponseBytes.
// A non-positive timeout falls back to webhookDefaultAttemptTimeout.
func NewWebhookHTTPClient(allow []string, timeout time.Duration) *http.Client {
	return newWebhookHTTPClient(allow, timeout, net.DefaultResolver)
}

func newWebhookHTTPClient(allow []string, timeout time.Duration, resolver webhookResolver) *http.Client {
	if timeout <= 0 {
		timeout = webhookDefaultAttemptTimeout
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	allowCopy := append([]string(nil), allow...)

	dialOne := func(ctx context.Context, network string, ip net.IP, port string, explicit bool) (net.Conn, error) {
		d := &net.Dialer{
			Timeout: timeout,
			Control: func(_, address string, _ syscall.RawConn) error {
				h, _, err := net.SplitHostPort(address)
				if err != nil {
					return webhookGuardErr("could not read the address being dialed")
				}
				resolved := net.ParseIP(h)
				if resolved == nil {
					return webhookGuardErr("could not parse the resolved address")
				}
				if reason := webhookBlockedIPReason(resolved, explicit); reason != "" {
					return webhookGuardErr("refusing to connect to a %s (%s)", reason, resolved)
				}
				return nil
			},
		}
		return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}

	transport := &http.Transport{
		Proxy:             nil,
		DisableKeepAlives: true,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, webhookGuardErr("could not read the address being dialed")
			}
			if !WebhookHostAllowed(addr, allowCopy) {
				return nil, webhookGuardErr("host %s is not on the webhook allow-list", addr)
			}
			// Allowed by name or literal IP: private ranges are permitted,
			// the metadata addresses are still checked in Control.
			const explicit = true
			if ip := net.ParseIP(host); ip != nil {
				return dialOne(ctx, network, ip, port, explicit)
			}
			canonical, obfuscated, err := normaliseWebhookHost(host)
			if err != nil || obfuscated {
				return nil, webhookGuardErr("the host is not in a canonical spelling")
			}
			addrs, err := resolver.LookupIPAddr(ctx, canonical)
			if err != nil {
				return nil, err
			}
			if len(addrs) == 0 {
				return nil, webhookGuardErr("host %s did not resolve to any address", canonical)
			}
			var lastErr error
			for _, a := range addrs {
				conn, derr := dialOne(ctx, network, a.IP, port, explicit)
				if derr == nil {
					return conn, nil
				}
				lastErr = derr
			}
			return nil, lastErr
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: cappedBodyTransport{next: transport, limit: webhookMaxResponseBytes},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// cappedBodyTransport wraps a RoundTripper and limits how much of the
// response body can be read.
type cappedBodyTransport struct {
	next  http.RoundTripper
	limit int64
}

func (t cappedBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	resp.Body = &cappedBody{r: io.LimitReader(resp.Body, t.limit), c: resp.Body}
	return resp, nil
}

type cappedBody struct {
	r io.Reader
	c io.Closer
}

func (b *cappedBody) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *cappedBody) Close() error               { return b.c.Close() }
