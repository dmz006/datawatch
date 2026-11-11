package autonomous

import (
	"strings"
	"sync"
	"testing"
)

func TestNodeUnresponsiveAlertsAtThreshold(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	c := newNodeUnresponsiveCounter(3, func(level, title, body string) string {
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		return ""
	})
	for i := 1; i <= 2; i++ {
		if n := c.record("gpu1", "qwen"); n != i {
			t.Fatalf("count=%d want %d", n, i)
		}
	}
	if len(bodies) != 0 {
		t.Fatal("alert before threshold")
	}
	c.record("gpu1", "qwen")
	if len(bodies) != 1 {
		t.Fatalf("alerts=%d want 1", len(bodies))
	}
	for _, w := range []string{"gpu1", "qwen"} {
		if !strings.Contains(bodies[0], w) {
			t.Fatalf("alert missing %q: %s", w, bodies[0])
		}
	}
	c.record("gpu1", "qwen")
	if len(bodies) != 1 {
		t.Fatal("alert repeated past threshold")
	}
}

func TestNodeUnresponsiveResetAndIsolation(t *testing.T) {
	alerts := 0
	c := newNodeUnresponsiveCounter(0, func(_, _, _ string) string { alerts++; return "" })
	c.record("a", "m")
	c.record("a", "m")
	c.reset("a")
	c.record("a", "m")
	c.record("b", "m")
	if c.count("a") != 1 || c.count("b") != 1 || alerts != 0 {
		t.Fatalf("a=%d b=%d alerts=%d", c.count("a"), c.count("b"), alerts)
	}
}

func TestManagerRecordNodeUnresponsiveUsesSink(t *testing.T) {
	m := &Manager{}
	var got []string
	m.SetAlertSink(func(_, title, body string) string { got = append(got, body); return "" })
	for i := 0; i < DefaultNodeUnresponsiveThreshold; i++ {
		m.RecordNodeUnresponsive("n1", "mod")
	}
	if len(got) != 1 {
		t.Fatalf("alerts=%d", len(got))
	}
	m.resetNodeUnresponsive("n1")
	if m.RecordNodeUnresponsive("n1", "mod") != 1 {
		t.Fatal("reset did not clear")
	}
}
