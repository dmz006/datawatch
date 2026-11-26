package autonomous

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// (a) A pi failure line in the output log fails the attempt at once.
func TestGuardPiFailureLineFailsAtOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(p, []byte("start\nDATAWATCH_PI_FAILED: pi exited with status 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ev, ok := PiFailedLineEvidence(p, "gpu-a", "qwen-x")
	if !ok || ev.ExitStatus == nil || *ev.ExitStatus != 5 {
		t.Fatalf("pi failure line not detected: %+v ok=%v", ev, ok)
	}
}

// (b) A pi request timeout is model-unresponsive and requeues without
// consuming an attempt.
func TestGuardRequestTimeoutRequeuesWithoutAttempt(t *testing.T) {
	old := infraSleep
	infraSleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { infraSleep = old })

	if class, ok := ClassifyInfraText(modelUnresponsiveStall); !ok || class != InfraClassModelUnresponsive {
		t.Fatalf("class=%q ok=%v", class, ok)
	}
	m, prd, task := infraRetryFixture(t, nil)
	task.ComputeNode = "gpu-a"
	action, err := m.handleInfraFailure(context.Background(), prd, task,
		attemptRun{Backend: "pi-prompt", Model: "qwen-x"}, errors.New(modelUnresponsiveStall))
	if err != nil {
		t.Fatal(err)
	}
	if action != infraFailureRetry {
		t.Fatalf("action=%v want retry", action)
	}
	if task.InfraRetryCount != 0 || task.ModelUnresponsiveRetries != 1 {
		t.Fatalf("InfraRetryCount=%d ModelUnresponsiveRetries=%d", task.InfraRetryCount, task.ModelUnresponsiveRetries)
	}
}

// (c) The per-node unresponsive counter raises an alert at the threshold,
// naming the node and model.
func TestGuardNodeUnresponsiveCounterAlerts(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	c := newNodeUnresponsiveCounter(2, func(_, _, body string) string {
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		return ""
	})
	c.record("gpu-a", "qwen-x")
	if len(bodies) != 0 {
		t.Fatal("alert before threshold")
	}
	c.record("gpu-a", "qwen-x")
	if len(bodies) != 1 || !strings.Contains(bodies[0], "gpu-a") || !strings.Contains(bodies[0], "qwen-x") {
		t.Fatalf("alerts=%v", bodies)
	}
}

// (d) The stall evidence from the output log names node, model and last output.
// The cmd/datawatch helper is in package main and not reachable from here, so
// the package's log evidence builder is tested.
func TestGuardStallEvidenceNamesNodeModelOutput(t *testing.T) {
	p := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(p, []byte("working\nlast line before stall\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ev := RunnerEvidenceFromLog(p, "gpu-a", "qwen-x")
	if ev.Node != "gpu-a" || ev.Model != "qwen-x" {
		t.Fatalf("evidence=%+v", ev)
	}
	if len(ev.LastOutput) == 0 || ev.LastOutput[len(ev.LastOutput)-1] != "last line before stall" {
		t.Fatalf("last output=%v", ev.LastOutput)
	}
}
