// BL1 — IPv6-safe listen-address composition.
//
// Bare `fmt.Sprintf("%s:%d", host, port)` breaks for IPv6 literals
// because "::1:8080" is ambiguous (could be parsed as the IPv6 group
// "::1" with port 8080, or as the IPv6 address "::1:8080" with no
// port). RFC 3986 §3.2.2 mandates bracketing IPv6 literals when used
// in network addresses: "[::1]:8080".
//
// `net.JoinHostPort` does this correctly for any host (IPv4, IPv6,
// hostname). All listener bind sites and Host-header rewrites should
// route through `joinHostPort` rather than concatenating the parts.
//
// To bind dual-stack (IPv4 + IPv6 on the same port), set the host to
// "::" — Go's net.Listen on Linux/Darwin enables IPV6_V6ONLY=false by
// default, so a single `[::]:8080` listener accepts both families.

package server

import (
	"net"
	"strconv"
)

// joinHostPort returns "host:port" for IPv4/hostname, and "[host]:port"
// for IPv6 literals.
func joinHostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
