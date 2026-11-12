// A daemon restart can resume a session on a different backend than its task
// was configured for (a pi-prompt session under a task set to claude-code).
// A lease seeded under the configured backend is stale: adoptCapacity must
// release it, lease from the backend the session really runs, and raise one
// alert per task and session. No daemon, tmux or hosting CLI is involved.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run 'TestStaleLeaseRestart'
package autonomous

import (
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
)

type staleAlert struct{ level, title, body string }

type staleLeaseRig struct {
	*adoptRestartRig
	mu     sync.Mutex
	alerts []staleAlert
}

func (r *staleLeaseRig) alertList() []staleAlert {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]staleAlert(nil), r.alerts...)
}

// newStaleLeaseRig builds n in-progress tasks configured for claude-code. The
// session lookup reports pi-prompt for every session listed in piSessions.
func newStaleLeaseRig(t *testing.T, n int, piSessions map[string]bool) *staleLeaseRig {
	t.Helper()
	base := newAdoptRestartRig(t, make([]string, n)...)
	r := &staleLeaseRig{adoptRestartRig: base}
	led := r.led
	led.SetLimit("llm:claude-code", 1)
	led.SetLimit("llm:pi-prompt", 2)
	r.m.SetCapacity(led, func(backend, model string) ([]string, string) {
		switch backend {
		case "claude-code":
			return []string{"host", "llm:claude-code"}, ""
		case "pi-prompt":
			return []string{"host", "llm:pi-prompt"}, ""
		}
		return nil, ""
	})
	r.m.SetCapacityChoices(nil)
	r.m.SetEntryNodes(nil)
	r.m.SetSessionBackend(func(id string) (string, string) {
		if piSessions[id] {
			return "pi-prompt", "qwen3.8:27b"
		}
		return "", ""
	})
	r.m.SetAlertSink(func(level, title, body string) string {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.alerts = append(r.alerts, staleAlert{level, title, body})
		return ""
	})
	for _, task := range r.tasks {
		task.Backend = "claude-code"
	}
	return r
}

func (r *staleLeaseRig) seedClaudeLease(task *Task) {
	r.led.Adopt(capacity.Request{
		Holder: task.ID, PRDID: r.prd.ID, Pools: []string{"host", "llm:claude-code"}, Consumer: consumerSessionSpawn,
	}, task.SessionID, VerifyHolder(task.ID))
}

func TestStaleLeaseRestart_PiSessionReleasesClaudeLease(t *testing.T) {
	r := newStaleLeaseRig(t, 1, map[string]bool{"sess-a": true})
	task := r.tasks[0]
	r.seedClaudeLease(task)
	if got := holdersOf(t, r.led.Snapshot(), "llm:claude-code"); len(got) != 1 {
		t.Fatalf("fixture: claude-code holders = %v, want the stale lease", got)
	}

	r.m.adoptCapacity(r.prd, task)

	if got := holdersOf(t, r.led.Snapshot(), "llm:claude-code"); len(got) != 0 {
		t.Errorf("claude-code holders = %v, want none", got)
	}
	if n := r.leaseCountFor(task.ID); n != 1 {
		t.Errorf("task holds %d leases, want 1", n)
	}
	ls := leaseOrFail(t, r.led, task.ID)
	inPi := false
	for _, p := range ls.Pools {
		if p == "llm:claude-code" {
			t.Errorf("lease still names llm:claude-code: %v", ls.Pools)
		}
		if p == "llm:pi-prompt" {
			inPi = true
		}
	}
	if !inPi {
		t.Errorf("lease pools %v lack llm:pi-prompt", ls.Pools)
	}
	if h := holdersOf(t, r.led.Snapshot(), "llm:pi-prompt"); len(h) != 1 || h[0] != task.ID {
		t.Errorf("pi-prompt holders = %v, want only %s", h, task.ID)
	}
	al := r.alertList()
	if len(al) != 1 {
		t.Fatalf("got %d alerts, want 1: %v", len(al), al)
	}
	if al[0].level != "warn" || !strings.Contains(al[0].body, task.ID) {
		t.Errorf("alert = %+v, want a warn naming task %s", al[0], task.ID)
	}
}

func TestStaleLeaseRestart_ThreePiWorkersStayWithinLimit(t *testing.T) {
	r := newStaleLeaseRig(t, 3, map[string]bool{"sess-a": true, "sess-b": true, "sess-c": true})
	// Two tasks already lease from pi-prompt; the third carries a stale claude lease.
	r.tasks[0].Backend, r.tasks[1].Backend = "pi-prompt", "pi-prompt"
	r.seedClaudeLease(r.tasks[2])

	for _, task := range r.tasks {
		r.m.adoptCapacity(r.prd, task)
	}

	st := r.led.Snapshot()
	pi, ok := poolOf(st, "llm:pi-prompt")
	if !ok {
		t.Fatal("pool llm:pi-prompt missing")
	}
	if pi.Held+pi.Overflow != 3 {
		t.Errorf("pi-prompt reads held=%d overflow=%d, want its true occupancy of 3", pi.Held, pi.Overflow)
	}
	cl, ok := poolOf(st, "llm:claude-code")
	if !ok {
		t.Fatal("pool llm:claude-code missing")
	}
	if cl.Held != 0 || cl.Overflow != 0 || len(cl.Holders) != 0 {
		t.Errorf("claude-code lane reads held=%d overflow=%d holders=%v, want free", cl.Held, cl.Overflow, cl.Holders)
	}
	for _, task := range r.tasks {
		if n := r.leaseCountFor(task.ID); n != 1 {
			t.Errorf("task %s holds %d leases, want 1", task.ID, n)
		}
	}
}

func TestStaleLeaseRestart_AlertOncePerSession(t *testing.T) {
	r := newStaleLeaseRig(t, 1, map[string]bool{"sess-a": true})
	task := r.tasks[0]
	r.seedClaudeLease(task)

	r.m.adoptCapacity(r.prd, task)
	r.m.adoptCapacity(r.prd, task)

	if al := r.alertList(); len(al) != 1 {
		t.Errorf("got %d alerts after two adoptions, want 1: %v", len(al), al)
	}
	if n := r.leaseCountFor(task.ID); n != 1 {
		t.Errorf("task holds %d leases, want 1", n)
	}
}

func TestStaleLeaseRestart_NoMismatchNoAlert(t *testing.T) {
	// The session runs claude-code, the same backend the task is configured for.
	r := newStaleLeaseRig(t, 1, nil)
	r.m.SetSessionBackend(func(string) (string, string) { return "claude-code", "" })
	task := r.tasks[0]
	r.seedClaudeLease(task)

	r.m.adoptCapacity(r.prd, task)

	if al := r.alertList(); len(al) != 0 {
		t.Errorf("got alerts %v, want none", al)
	}
	if h := holdersOf(t, r.led.Snapshot(), "llm:claude-code"); len(h) != 1 || h[0] != task.ID {
		t.Errorf("claude-code holders = %v, want %s to keep its lease", h, task.ID)
	}
	if h := holdersOf(t, r.led.Snapshot(), "llm:pi-prompt"); len(h) != 0 {
		t.Errorf("pi-prompt holders = %v, want none", h)
	}
}
