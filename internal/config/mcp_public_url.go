// mcp_public_url.go — validation for mcp.public_url, the base URL the MCP
// SSE server advertises to clients in its "endpoint" event. The value is
// only ever echoed to clients as text, but a malformed one (wrong scheme,
// no host, embedded credentials, a query string) would misdirect them, so
// every writer checks it here and the SSE server ignores an invalid one.
package config

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizePublicURL trims surrounding whitespace and trailing slashes from
// a raw mcp.public_url value. Empty (or whitespace-only) input returns the
// empty string and no error, meaning "advertise the bind address". Any
// other value must be an absolute http or https URL (scheme compared
// case-insensitively) with a host and no user-info, query or fragment;
// otherwise an error is returned and callers must not store or use it.
func NormalizePublicURL(raw string) (string, error) {
	v := strings.TrimRight(strings.TrimSpace(raw), "/")
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", fmt.Errorf("invalid mcp.public_url %q: %v", raw, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("invalid mcp.public_url %q: scheme must be http or https", raw)
	}
	if u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("invalid mcp.public_url %q: host is required", raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("invalid mcp.public_url %q: user-info is not allowed", raw)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(v, "?#") {
		return "", fmt.Errorf("invalid mcp.public_url %q: query and fragment are not allowed", raw)
	}
	return v, nil
}
