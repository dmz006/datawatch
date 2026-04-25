// BL171 (S9) — observer package unit tests.

package observer

import (
	"encoding/json"
	"testing"
)

func TestClassify_SessionWinsOverBackend(t *testing.T) {
	// Register a fake session root at pid 100.
	RegisterSessionRoot("sess-abcd", "Claude-dev", 100)
	defer UnregisterSessionRoot("sess-abcd")

	recs := []ProcRecord{
		{PID: 100, PPID: 1, Comm: "bash"},
		{PID: 200, PPID: 100, Comm: "claude", Cmdline: "claude"},
		{PID: 300, PPID: 200, Comm: "node", Cmdline: "node --foo"},
		{PID: 400, PPID: 1, Comm: "ollama", Cmdline: "ollama serve"},
	}
	cfg := EnvelopesCfg{
		SessionAttribution: true,
		BackendAttribution: true,
		BackendSignatures: map[string]BackendSig{
			"claude": {Exec: []string{"claude"}, Track: true},
			"ollama": {Exec: []string{"ollama"}, Track: true},
		},
	}
	envs, assign := classify(recs, cfg)
	if got := assign[200]; got != "session:sess-abcd" {
		t.Errorf("claude (pid 200) should fall under session envelope, got %q", got)
	}
	if got := assign[300]; got != "session:sess-abcd" {
		t.Errorf("node child (pid 300) should fall under session envelope, got %q", got)
	}
	if got := assign[400]; got != "backend:ollama" {
		t.Errorf("ollama (pid 400) should be backend:ollama, got %q", got)
	}
	// Sort puts session first since it has >1 pid vs ollama's 1.
	if len(envs) < 2 {
		t.Fatalf("expected 2+ envelopes, got %d", len(envs))
	}
}

func TestClassify_DockerContainerFallback(t *testing.T) {
	recs := []ProcRecord{
		{PID: 10, PPID: 1, Comm: "python", ContainerID: "abc123def456"},
		{PID: 11, PPID: 10, Comm: "python", ContainerID: "abc123def456"},
	}
	cfg := EnvelopesCfg{DockerDiscovery: true}
	envs, assign := classify(recs, cfg)
	if !startsWith(assign[10], "container:") {
		t.Errorf("containerised process should land in container envelope: got %q", assign[10])
	}
	if len(envs) != 1 {
		t.Fatalf("expected 1 envelope, got %d", len(envs))
	}
	if envs[0].Kind != EnvelopeContainer {
		t.Errorf("kind mismatch: %s", envs[0].Kind)
	}
}

func TestClassify_SystemFallback(t *testing.T) {
	recs := []ProcRecord{
		{PID: 1, PPID: 0, Comm: "init"},
		{PID: 2, PPID: 0, Comm: "kthread"},
	}
	envs, _ := classify(recs, EnvelopesCfg{})
	if len(envs) != 1 || envs[0].Kind != EnvelopeSystem {
		t.Fatalf("expected single system envelope, got %+v", envs)
	}
}

func TestExtractContainerID_DockerV2(t *testing.T) {
	id64 := "abc1234567890123456789012345678901234567890123456789012345678901"  // 64 chars
	id2  := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" // 64 chars
	cases := map[string]string{
		"0::/docker/" + id64:                                            id64,
		"0::/system.slice/docker-" + id2 + ".scope":                      id2,
		"0::/":                                                          "",
		"0::/system.slice/systemd.service":                               "",
	}
	for in, want := range cases {
		if got := extractContainerID(in); got != want {
			t.Errorf("extractContainerID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollector_LatestAndAliases(t *testing.T) {
	c := NewCollector(DefaultConfig())
	c.SetSessionCountsFn(func() (int, int, int, int, map[string]int) {
		return 3, 2, 1, 0, map[string]int{"claude-code": 2, "ollama": 1}
	})
	c.tick()
	snap := c.Latest()
	if snap == nil {
		t.Fatal("Latest() nil after tick")
	}
	if snap.V != 2 {
		t.Errorf("schema version = %d, want 2", snap.V)
	}
	if snap.Sessions.Total != 3 || snap.Sessions.Running != 2 {
		t.Errorf("sessions not populated: %+v", snap.Sessions)
	}
	if snap.SessionsTotal != snap.Sessions.Total {
		t.Errorf("v1 alias SessionsTotal=%d != Sessions.Total=%d",
			snap.SessionsTotal, snap.Sessions.Total)
	}
	if snap.UptimeSeconds != snap.Host.UptimeSeconds {
		t.Errorf("v1 alias UptimeSeconds=%d != Host.UptimeSeconds=%d",
			snap.UptimeSeconds, snap.Host.UptimeSeconds)
	}
}

// BL173 follow — verify SetClusterNodesFn folds output into snap.Cluster.
func TestCollector_ClusterNodesFn_PopulatesSnapshot(t *testing.T) {
	c := NewCollector(DefaultConfig())
	c.SetClusterNodesFn(func() []ClusterNode {
		return []ClusterNode{
			{Name: "n1", Ready: true, CPUPct: 12.5, MemPct: 33.0},
			{Name: "n2", Ready: true, CPUPct: 5.0, MemPct: 18.0, Pressure: []string{"memory"}},
		}
	})
	c.tick()
	snap := c.Latest()
	if snap == nil || snap.Cluster == nil {
		t.Fatalf("expected non-nil Cluster, got snap=%+v", snap)
	}
	if len(snap.Cluster.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(snap.Cluster.Nodes))
	}
	if snap.Cluster.Nodes[0].Name != "n1" || snap.Cluster.Nodes[1].Name != "n2" {
		t.Errorf("node names: %+v", snap.Cluster.Nodes)
	}
}

// Empty fn → snap.Cluster stays nil so the PWA card hides itself.
func TestCollector_ClusterNodesFn_EmptyKeepsNil(t *testing.T) {
	c := NewCollector(DefaultConfig())
	c.SetClusterNodesFn(func() []ClusterNode { return nil })
	c.tick()
	snap := c.Latest()
	if snap != nil && snap.Cluster != nil {
		t.Errorf("empty cluster fn should leave Cluster nil, got %+v", snap.Cluster)
	}
}

func TestAPI_SetConfigRoundTrip(t *testing.T) {
	c := NewCollector(DefaultConfig())
	a := NewAPI(c)
	raw := json.RawMessage(`{"tick_interval_ms":500,"process_tree":{"enabled":false}}`)
	if err := a.SetConfig(raw); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	if c.Config().TickIntervalMs != 500 {
		t.Errorf("tick_interval_ms not applied: %+v", c.Config())
	}
	if c.Config().ProcessTree.Enabled {
		t.Errorf("process_tree.enabled should be false")
	}
}

func startsWith(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
