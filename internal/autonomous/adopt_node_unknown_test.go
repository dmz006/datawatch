// Adopted workers whose compute node cannot be determined: the lease holds no
// node, is flagged, and the flag shows in the per-task view and the status text.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run 'NodeUnknown|UnknownNode'
package autonomous

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
)

// capacityRequest is an adopted worker's request, with the node it is known on
// (empty when not) and whether it is flagged as unknown.
func capacityRequest(holder, prdID string, pools []string, node string, unknown bool) capacity.Request {
	return capacity.Request{Holder: holder, PRDID: prdID, Pools: pools, Node: node, NodeUnknown: unknown}
}

func TestAdoptCapacityFlagsUnknownNodeOnMultiNodeEntry(t *testing.T) {
	m, led, prd, task := adoptNodeRig(t)
	m.SetEntryNodes(func(string, string) int { return 2 })
	m.SetSessionEndpoint(func(id string) string {
		if id == "live-sess" {
			return "http://10.9.9.9:11434"
		}
		return ""
	})
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(log.Writer())

	m.adoptCapacity(prd, task)
	m.adoptCapacity(prd, task)

	ls := leaseOrFail(t, led, task.ID)
	if ls.Node != "" || !ls.NodeUnknown {
		t.Fatalf("lease = %+v, want no node and node_unknown", ls)
	}
	out := buf.String()
	for _, want := range []string{task.ID, "live-sess", "http://10.9.9.9:11434"} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q should name %q", out, want)
		}
	}
	if n := strings.Count(out, "node_unknown"); n != 1 {
		t.Errorf("the line should be logged once per lease, got %d:\n%s", n, out)
	}

	// The node turns up later (session reports it): the flag clears.
	m.SetSessionNode(func(string) string { return "second" })
	m.adoptCapacity(prd, task)
	if ls := leaseOrFail(t, led, task.ID); ls.Node != "second" || ls.NodeUnknown {
		t.Fatalf("lease = %+v, want node second and no flag", ls)
	}
}

func TestAdoptCapacityLogsWithoutEndpointReader(t *testing.T) {
	m, led, prd, task := adoptNodeRig(t)
	m.SetEntryNodes(func(string, string) int { return 2 })
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(log.Writer())

	m.adoptCapacity(prd, task)

	if !leaseOrFail(t, led, task.ID).NodeUnknown || !strings.Contains(buf.String(), `endpoint=""`) {
		t.Fatalf("flag and an empty-endpoint line expected, log: %s", buf.String())
	}
}

func TestAdoptCapacityDoesNotFlagBackendsThatTakeNoNode(t *testing.T) {
	for name, count := range map[string]EntryNodesFn{
		"hosted backend":         func(string, string) int { return 0 },
		"single-node entry":      func(string, string) int { return 1 },
		"count not wired, no ch": nil,
	} {
		t.Run(name, func(t *testing.T) {
			m, led, prd, task := adoptNodeRig(t)
			m.SetEntryNodes(count)
			m.SetCapacityChoices(nil)
			var buf bytes.Buffer
			log.SetOutput(&buf)
			defer log.SetOutput(log.Writer())

			m.adoptCapacity(prd, task)

			if ls := leaseOrFail(t, led, task.ID); ls.NodeUnknown {
				t.Fatalf("lease = %+v, must not be flagged", ls)
			}
			if strings.Contains(buf.String(), "node_unknown") {
				t.Fatalf("nothing to log for %s: %s", name, buf.String())
			}
		})
	}
}

func TestAdoptCapacityKeepsResolverNodeWhenEntryCountNotWired(t *testing.T) {
	m, led, prd, task := adoptNodeRig(t)
	m.SetCapacityChoices(func(string, string) []capacity.NodeChoice {
		return []capacity.NodeChoice{{Name: "first"}, {Name: "second"}}
	})
	m.SetSessionNode(func(string) string { return "" })
	// Count not wired: the resolver's node stands, so the lease is placed, not flagged.
	m.adoptCapacity(prd, task)
	if ls := leaseOrFail(t, led, task.ID); ls.NodeUnknown || ls.Node != "first" {
		t.Fatalf("lease = %+v, want the resolver's node and no flag", ls)
	}
}

func TestNodeExpected(t *testing.T) {
	two := func(string, string) int { return 2 }
	one := func(string, string) int { return 1 }
	cases := []struct {
		name       string
		count      EntryNodesFn
		candidates int
		want       bool
	}{
		{"entry lists two", two, 0, true},
		{"entry lists one", one, 5, false},
		{"no count, two candidates", nil, 2, true},
		{"no count, one candidate", nil, 1, false},
		{"no count, none", nil, 0, false},
	}
	for _, c := range cases {
		if got := nodeExpected(c.count, c.candidates, "b", "m"); got != c.want {
			t.Errorf("%s: nodeExpected = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTaskVisibilityFlagsUnknownNode(t *testing.T) {
	m, prd, led := visFixture(t, 3)
	ids := visTaskIDs(t, m, prd.ID)
	for _, id := range ids {
		visSetTask(t, m, id, func(c *Task) { c.Status = TaskInProgress })
	}
	pools := []string{"host", "llm:qwen"}
	led.Adopt(capacityRequest(ids[0], prd.ID, pools, "", true), "sess-a")
	led.Adopt(capacityRequest(ids[1], prd.ID, append(append([]string{}, pools...), "node:n1"), "n1", false), "sess-b")
	led.Adopt(capacityRequest(ids[2], prd.ID, pools, "", false), "sess-c")

	_, holdings := m.TaskVisibility()
	if h := holdViewFor(holdings, ids[0]); h == nil || !h.NodeUnknown || h.Node != "" {
		t.Fatalf("flagged task view = %+v, want node_unknown and no node", h)
	}
	for _, id := range ids[1:] {
		if h := holdViewFor(holdings, id); h == nil || h.NodeUnknown {
			t.Fatalf("task %s view = %+v, must not be flagged", id, h)
		}
	}
	raw, err := json.Marshal(holdViewFor(holdings, ids[0]))
	if err != nil || !strings.Contains(string(raw), `"node_unknown":true`) {
		t.Fatalf("JSON should carry node_unknown: %s (%v)", raw, err)
	}
	raw, _ = json.Marshal(holdViewFor(holdings, ids[1]))
	if strings.Contains(string(raw), "node_unknown") {
		t.Fatalf("an unflagged view must omit node_unknown: %s", raw)
	}
}

func TestFormatCapacityTextMarksUnknownNode(t *testing.T) {
	body := []byte(`{
	  "pools": [{"name":"llm:pi-prompt","limit":2,"held":2,"external":0,"holders":["t1","t2"]}],
	  "leases": [{"holder":"t1","node_unknown":true},{"holder":"t2","node":"gpu-a"}],
	  "tasks": {"waiting": [], "running": [
	    {"task_id":"t1","prd_id":"p1","pools":["host","llm:pi-prompt"],"node_unknown":true,"session_id":"s1","since":"2026-10-27T00:00:00Z"},
	    {"task_id":"t2","prd_id":"p1","pools":["host","llm:pi-prompt","node:gpu-a"],"node":"gpu-a","session_id":"s2","since":"2026-10-27T00:00:00Z"}
	  ]}
	}`)
	text, err := FormatCapacityText(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Leases with unknown node: 1") {
		t.Errorf("pools text should name how many leases have an unknown node:\n%s", text)
	}
	var t1, t2 string
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.Contains(line, "t1"):
			t1 = line
		case strings.Contains(line, "t2") && strings.Contains(line, "holds"):
			t2 = line
		}
	}
	if !strings.Contains(t1, "node unknown") || strings.Contains(t1, ", node gpu") {
		t.Errorf("flagged running task line should say node unknown: %q", t1)
	}
	if !strings.Contains(t2, ", node gpu-a") || strings.Contains(t2, "node unknown") {
		t.Errorf("placed running task line should name its node only: %q", t2)
	}

	clean, err := FormatCapacityText([]byte(`{"pools":[],"leases":[{"holder":"t2","node":"gpu-a"}],"tasks":{"waiting":[],"running":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(clean, "unknown") {
		t.Errorf("nothing flagged, nothing said:\n%s", clean)
	}
}
