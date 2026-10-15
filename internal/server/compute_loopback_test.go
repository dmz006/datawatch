package server

import "testing"

// 2026-10-15 — operator-reported: a ComputeNode whose Address is a
// loopback (e.g. a CPU-only logical partition of the local host) never
// matched the self-peer monitoring fallback because its NAME didn't
// equal the daemon's hostname. isLoopbackAddress fixes this by checking
// the actual address instead.
func TestIsLoopbackAddress(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"http://127.0.0.1:11436", true},
		{"https://127.0.0.1:8443", true},
		{"127.0.0.1:11434", true},
		{"127.0.0.1", true},
		{"http://localhost:11434", true},
		{"localhost", true},
		{"http://[::1]:11434", true},
		{"::1", true},
		{"http://johnnyjohnny:11434", false},
		{"http://datawatch:3000", false},
		{"http://10.0.0.5:11434", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLoopbackAddress(c.addr); got != c.want {
			t.Errorf("isLoopbackAddress(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}
