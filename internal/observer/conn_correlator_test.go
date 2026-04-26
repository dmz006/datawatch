// BL180 Phase 2 — unit tests for the userspace conn correlator. Q6
// answer mandates these gate the merge; the Thor smoke test (which we
// can't run from CI without operator-provisioned host) gates the
// closure.

package observer

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHexEndpoint_IPv4(t *testing.T) {
	ip, port, ok := parseHexEndpoint("0100007F:1F90") // 127.0.0.1:8080
	if !ok {
		t.Fatal("expected ok")
	}
	if !ip.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("ip = %v, want 127.0.0.1", ip)
	}
	if port != 8080 {
		t.Fatalf("port = %d, want 8080", port)
	}
}

func TestParseHexEndpoint_IPv6Loopback(t *testing.T) {
	// ::1:80 in /proc/net/tcp6 little-endian-per-word format.
	ip, port, ok := parseHexEndpoint("00000000000000000000000001000000:0050")
	if !ok {
		t.Fatal("expected ok")
	}
	if !ip.Equal(net.ParseIP("::1")) {
		t.Fatalf("ip = %v, want ::1", ip)
	}
	if port != 80 {
		t.Fatalf("port = %d, want 80", port)
	}
}

func TestParseHexEndpoint_BadInput(t *testing.T) {
	cases := []string{"", "no-colon", "00000000:GGGG", "ZZZ:0050"}
	for _, c := range cases {
		if _, _, ok := parseHexEndpoint(c); ok {
			t.Errorf("expected !ok for %q", c)
		}
	}
}

func TestParseTCPLine_Established(t *testing.T) {
	// One real /proc/net/tcp line: established conn from 127.0.0.1:51234
	// to 127.0.0.1:11434 (ollama default port). State 01 = ESTABLISHED.
	line := "  0: 0100007F:C822 0100007F:2CAA 01 00000000:00000000 02:00000050 00000000  1000        0 8439021 1 0000000000000000 100 0 0 10 0"
	r, ok := parseTCPLine(line)
	if !ok {
		t.Fatal("expected ok")
	}
	if r.LocalPort != 0xC822 {
		t.Errorf("LocalPort = %d, want 0xC822", r.LocalPort)
	}
	if r.RemotePort != 0x2CAA {
		t.Errorf("RemotePort = %d, want 0x2CAA", r.RemotePort)
	}
	if r.State != 0x01 {
		t.Errorf("State = %d, want 0x01", r.State)
	}
	if r.Inode != 8439021 {
		t.Errorf("Inode = %d, want 8439021", r.Inode)
	}
}

func TestIsLocalhostScope(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":     true,
		"::1":           true,
		"172.17.0.5":    true, // docker bridge
		"10.42.0.5":     true, // k8s default
		"192.168.1.10":  true, // home LAN / Thor scope per Q5 answer
		"8.8.8.8":       false,
		"203.0.113.10":  false,
	}
	for ipStr, want := range cases {
		ip := net.ParseIP(ipStr)
		got := isLocalhostScope(ip)
		if got != want {
			t.Errorf("isLocalhostScope(%s) = %v, want %v", ipStr, got, want)
		}
	}
}

// TestCorrelateCallers_EndToEnd builds a fake /proc tree with a client
// envelope (PID 100) talking to a server envelope (PID 200 listening on
// :11434) and confirms the resulting Callers[] surfaces the join.
func TestCorrelateCallers_EndToEnd(t *testing.T) {
	root := t.TempDir()

	// PID 100 = opencode session client. One ESTABLISHED outbound to
	// 127.0.0.1:11434.
	writeProcTCP(t, root, 100, []string{
		// header
		"  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode",
		"  0: 0100007F:C822 0100007F:2CAA 01 00000000:00000000 02:00000050 00000000  1000  0 8439021 1 0000000000000000 100 0 0 10 0",
	})
	// PID 200 = ollama backend server. One LISTEN on 0.0.0.0:11434 + the
	// inbound side of the same conn.
	writeProcTCP(t, root, 200, []string{
		"  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode",
		"  0: 00000000:2CAA 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000  0 9000001 1 0000000000000000 100 0 0 10 0",
		"  1: 0100007F:2CAA 0100007F:C822 01 00000000:00000000 02:00000050 00000000  1000  0 9000002 1 0000000000000000 100 0 0 10 0",
	})

	envelopes := []Envelope{
		{ID: "session:opencode-x1y2", Kind: EnvelopeSession, PIDs: []int{100}},
		{ID: "backend:ollama", Kind: EnvelopeBackend, PIDs: []int{200}},
	}

	out := CorrelateCallers(envelopes, root)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	server := out[1]
	if len(server.Callers) != 1 {
		t.Fatalf("server.Callers len = %d, want 1; %+v", len(server.Callers), server.Callers)
	}
	c := server.Callers[0]
	if c.Caller != "session:opencode-x1y2" {
		t.Errorf("Caller = %q, want session:opencode-x1y2", c.Caller)
	}
	if c.CallerKind != "session" {
		t.Errorf("CallerKind = %q, want session", c.CallerKind)
	}
	if c.Conns != 1 {
		t.Errorf("Conns = %d, want 1", c.Conns)
	}
	// Derived loudest-caller alias should fall through (Phase 1 didn't
	// pre-fill Caller on this fixture envelope).
	if server.Caller != "session:opencode-x1y2" || server.CallerKind != "session" {
		t.Errorf("derived Caller/Kind = %q/%q, want session:opencode-x1y2/session", server.Caller, server.CallerKind)
	}
}

// TestCorrelateCallers_PreservesPhase1Caller — when the ollama tap (Phase
// 1) has already set Caller="ollama_model:llama3", the procfs derivation
// must not overwrite it; the per-client breakdown still appears in
// Callers[].
func TestCorrelateCallers_PreservesPhase1Caller(t *testing.T) {
	root := t.TempDir()
	writeProcTCP(t, root, 100, []string{
		"  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode",
		"  0: 0100007F:C822 0100007F:2CAA 01 00000000:00000000 02:00000050 00000000  1000  0 8439021 1 0000000000000000 100 0 0 10 0",
	})
	writeProcTCP(t, root, 200, []string{
		"  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode",
		"  0: 00000000:2CAA 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000  0 9000001 1 0000000000000000 100 0 0 10 0",
	})

	envelopes := []Envelope{
		{ID: "session:opencode-x1y2", Kind: EnvelopeSession, PIDs: []int{100}},
		{
			ID:         "backend:ollama:llama3",
			Kind:       EnvelopeBackend,
			PIDs:       []int{200},
			Caller:     "ollama_model:llama3",
			CallerKind: "ollama_model",
		},
	}

	out := CorrelateCallers(envelopes, root)
	server := out[1]
	if server.Caller != "ollama_model:llama3" || server.CallerKind != "ollama_model" {
		t.Errorf("Phase 1 Caller overwritten: %q/%q", server.Caller, server.CallerKind)
	}
	if len(server.Callers) != 1 {
		t.Fatalf("Callers len = %d, want 1", len(server.Callers))
	}
	if server.Callers[0].Caller != "session:opencode-x1y2" {
		t.Errorf("Callers[0] = %q, want session:opencode-x1y2", server.Callers[0].Caller)
	}
}

// TestCorrelateCallers_RejectsCrossHost confirms Q5 scoping: a remote
// non-private IP must not produce an attribution row.
func TestCorrelateCallers_RejectsCrossHost(t *testing.T) {
	root := t.TempDir()
	// 8.8.8.8:11434 — public IP, must be filtered out.
	writeProcTCP(t, root, 100, []string{
		"  sl  local_address rem_address   st tx_queue:rx_queue tr:tm->when retrnsmt   uid  timeout inode",
		"  0: 0100007F:C822 08080808:2CAA 01 00000000:00000000 02:00000050 00000000  1000  0 8439021 1 0000000000000000 100 0 0 10 0",
	})
	envelopes := []Envelope{
		{ID: "session:opencode-x1y2", Kind: EnvelopeSession, PIDs: []int{100}},
	}
	out := CorrelateCallers(envelopes, root)
	if len(out[0].Callers) != 0 {
		t.Fatalf("expected no callers (cross-host filtered); got %+v", out[0].Callers)
	}
}

// TestFormatCallerSummary checks the helper used by log/MCP surfaces.
func TestFormatCallerSummary(t *testing.T) {
	in := []CallerAttribution{
		{Caller: "session:opencode-x1y2", Conns: 6},
		{Caller: "backend:openwebui", Conns: 4},
	}
	got := FormatCallerSummary(in, 0)
	want := "60% session:opencode-x1y2, 40% backend:openwebui"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// writeProcTCP writes a fake /proc/<pid>/net/tcp file under root.
func writeProcTCP(t *testing.T, root string, pid int, lines []string) {
	t.Helper()
	dir := filepath.Join(root, "100", "net")
	_ = dir
	// pid is variable, recompute path.
	p := filepath.Join(root, intToStr(pid), "net")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(p, "tcp"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Also create empty tcp6 so readProcTCP doesn't fall through to the
	// real /proc when running from a sandboxed test.
	if err := os.WriteFile(filepath.Join(p, "tcp6"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
}

func intToStr(n int) string {
	// avoid pulling strconv into the helper just for tests
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
