package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// modelUnresponsiveStall is a pi failure line whose output shows a model
// request timeout.
const modelUnresponsiveStall = "DATAWATCH_PI_FAILED: pi exited\nError: Request timed out."

func TestModelUnresponsiveClassification(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"request timed out", modelUnresponsiveStall, true},
		{"mixed case", "datawatch_pi_failed: Request Timed Out", true},
		{"timeout without pi marker", "Error: Request timed out.", false},
		{"pi failure other reason", "DATAWATCH_PI_FAILED: pi exited\nError: connection refused", false},
		{"no output stall", "no output for 10 minutes", false},
		{"verifier failure", "verification failed: tests did not pass", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			class, ok := ClassifyInfraText(c.text)
			got := ok && class == InfraClassModelUnresponsive
			if got != c.want {
				t.Fatalf("ClassifyInfraText(%q) = %q,%v; model-unresponsive want %v", c.text, class, ok, c.want)
			}
		})
	}
	if class, ok := ClassifyInfraFailure(errors.New(modelUnresponsiveStall)); !ok || class != InfraClassModelUnresponsive {
		t.Fatalf("ClassifyInfraFailure = %q,%v", class, ok)
	}
	if _, ok := ClassifyInfraFailure(errors.New("session stalled with no output")); ok {
		t.Fatal("other stall classified as infrastructure failure")
	}
}

func TestModelUnresponsiveRequeueKeepsAttemptCount(t *testing.T) {
	old := infraSleep
	infraSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { infraSleep = old })

	m, prd, task := infraRetryFixture(t, nil)
	task.ComputeNode = "gpu-a"
	failuresBefore := len(task.Attempts)

	action, err := m.handleInfraFailure(context.Background(), prd, task, attemptRun{Backend: "pi-prompt", Model: "qwen-x"},
		errors.New(modelUnresponsiveStall))
	if err != nil {
		t.Fatalf("handleInfraFailure: %v", err)
	}
	if action != infraFailureRetry {
		t.Fatalf("action=%v want retry (requeue)", action)
	}
	if task.InfraRetryCount != 0 {
		t.Fatalf("InfraRetryCount=%d: failure consumed an attempt", task.InfraRetryCount)
	}
	if task.ModelUnresponsiveRetries != 1 {
		t.Fatalf("ModelUnresponsiveRetries=%d want 1", task.ModelUnresponsiveRetries)
	}
	if len(task.InfraRetries) != 1 || task.InfraRetries[0].Class != InfraClassModelUnresponsive {
		t.Fatalf("InfraRetries=%+v", task.InfraRetries)
	}
	for _, w := range []string{"gpu-a", "qwen-x"} {
		if !strings.Contains(task.Error, w) {
			t.Fatalf("task error %q lacks %q", task.Error, w)
		}
	}
	for _, a := range task.Attempts[failuresBefore:] {
		if a.Outcome == "" {
			t.Fatalf("attempt without outcome: %+v", a)
		}
	}
}

func TestModelUnresponsiveRequeueStopsAtBound(t *testing.T) {
	m, _, task := infraRetryFixture(t, nil)
	task.ModelUnresponsiveRetries = maxModelUnresponsiveRetries
	if _, ok := m.planInfraRetry(nil, task, errors.New(modelUnresponsiveStall)); ok {
		t.Fatal("requeue planned past the bound")
	}
}

func TestModelUnresponsivePerNodeCountAndAlert(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	c := newNodeUnresponsiveCounter(3, func(level, title, body string) string {
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		return "alert-1"
	})

	for i := 1; i <= 2; i++ {
		if n := c.record("gpu-a", "qwen-x"); n != i {
			t.Fatalf("gpu-a count=%d want %d", n, i)
		}
	}
	if n := c.record("gpu-b", "llama-y"); n != 1 {
		t.Fatalf("gpu-b count=%d want 1", n)
	}
	if c.count("gpu-a") != 2 || c.count("gpu-b") != 1 {
		t.Fatalf("counts a=%d b=%d", c.count("gpu-a"), c.count("gpu-b"))
	}
	if len(bodies) != 0 {
		t.Fatalf("alert fired before threshold: %v", bodies)
	}

	c.record("gpu-a", "qwen-x")
	if len(bodies) != 1 {
		t.Fatalf("alerts=%d want 1 at threshold", len(bodies))
	}
	for _, w := range []string{"gpu-a", "qwen-x"} {
		if !strings.Contains(bodies[0], w) {
			t.Fatalf("alert %q lacks %q", bodies[0], w)
		}
	}
	if strings.Contains(bodies[0], "gpu-b") {
		t.Fatalf("alert names the wrong node: %q", bodies[0])
	}
	c.record("gpu-a", "qwen-x")
	if len(bodies) != 1 {
		t.Fatalf("alert repeated past threshold: %d", len(bodies))
	}
}

func TestModelUnresponsiveManagerCountsPerNode(t *testing.T) {
	m := &Manager{}
	var got []string
	m.SetAlertSink(func(_, _, body string) string { got = append(got, body); return "" })
	for i := 0; i < DefaultNodeUnresponsiveThreshold; i++ {
		m.RecordNodeUnresponsive("n1", "mod-1")
	}
	m.RecordNodeUnresponsive("n2", "mod-2")
	if len(got) != 1 || !strings.Contains(got[0], "n1") || !strings.Contains(got[0], "mod-1") {
		t.Fatalf("alerts=%v", got)
	}
}
