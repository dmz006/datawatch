// v5.18.0 — verify the loopback detection used by the HTTP→HTTPS
// redirect bypass for the MCP channel bridge.

package server

import "testing"

func TestIsLoopbackRemote(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"127.0.0.1:35727", true},
		{"127.0.0.1", true},  // no port (some test paths)
		{"[::1]:8080", true}, // IPv6 loopback with port
		{"::1", true},
		{"10.0.0.42:8080", false},
		{"192.168.1.5:443", false},
		{"[2001:db8::1]:443", false},
		{"", false},
		{"garbage", false},
		{"127.1.2.3:9999", true}, // 127/8 still loopback
	}
	for _, tc := range cases {
		got := isLoopbackRemote(tc.in)
		if got != tc.want {
			t.Errorf("isLoopbackRemote(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}
