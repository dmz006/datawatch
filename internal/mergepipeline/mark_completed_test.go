package mergepipeline

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// markCompletedSpy is a Completer that records its calls. cancels counts the
// cancel calls it would receive; the Completer interface has no such method,
// so the count must stay zero.
type markCompletedSpy struct {
	mu      sync.Mutex
	calls   []markCall
	done    map[string]bool
	err     error
	cancels int
}

type markCall struct {
	id, release, commit string
	at                  time.Time
	noop                bool
}

func (m *markCompletedSpy) MarkCompleted(_ context.Context, id, release, commit string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	noop := m.done[id]
	if m.done == nil {
		m.done = map[string]bool{}
	}
	m.done[id] = true
	m.calls = append(m.calls, markCall{id, release, commit, at, noop})
	return nil
}

func markCompletedRun(t *testing.T, spy *markCompletedSpy, ps PostShipSettings, extra ...PostShipHook) RunResult {
	t.Helper()
	ps.Hooks = append(ps.Hooks, extra...)
	ship := &smokeShip{}
	fixed := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	r := &Runner{
		VersionFiles: testVersionFiles, Dir: smokeRepo(t), Project: "smoke",
		Resolver: smokePipeline(t), Ship: ship, Completer: spy, PostShip: ps,
		Now: func() time.Time { return fixed },
	}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}, AutomatonID: "prd-1"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res
}

func stepNamed(res RunResult, name string) (string, string, bool) {
	for _, s := range res.Run.Steps {
		if s.Step == name {
			return s.Result, s.Detail, true
		}
	}
	return "", "", false
}

func TestPostShipMarkCompletedRunsWhenBlockAndHookEnabled(t *testing.T) {
	spy := &markCompletedSpy{}
	res := markCompletedRun(t, spy, PostShipSettings{Enabled: true},
		PostShipHook{Name: "done", Builtin: builtinMarkCompleted, Enabled: true})
	if r, _, ok := stepNamed(res, "done"); !ok || r != "ok" {
		t.Fatalf("mark_completed step: ok=%v result=%q steps=%+v", ok, r, res.Run.Steps)
	}
	if len(spy.calls) != 1 {
		t.Fatalf("want one call, got %v", spy.calls)
	}
	c := spy.calls[0]
	if c.id != "prd-1" || c.release != "1.2.3" || c.commit == "" || c.at.IsZero() {
		t.Errorf("call %+v: want the Automaton, the run's release version, the merge commit and time", c)
	}
	if spy.cancels != 0 {
		t.Errorf("cancel calls: %d", spy.cancels)
	}
}

func TestPostShipMarkCompletedDoesNotRunWhenBlockOffOrHookDisabled(t *testing.T) {
	spy := &markCompletedSpy{}
	res := markCompletedRun(t, spy, PostShipSettings{Enabled: false},
		PostShipHook{Name: "done", Builtin: builtinMarkCompleted, Enabled: true})
	if _, _, ok := stepNamed(res, "done"); ok || len(spy.calls) != 0 {
		t.Errorf("block off: the hook must not run (step=%v calls=%v)", ok, spy.calls)
	}
	res = markCompletedRun(t, spy, PostShipSettings{Enabled: true},
		PostShipHook{Name: "done", Builtin: builtinMarkCompleted, Enabled: false})
	if _, _, ok := stepNamed(res, "done"); ok || len(spy.calls) != 0 {
		t.Errorf("hook disabled: the hook must not run (step=%v calls=%v)", ok, spy.calls)
	}
}

func TestMarkCompletedNoOpWhenAlreadyCompleted(t *testing.T) {
	spy := &markCompletedSpy{done: map[string]bool{"prd-1": true}}
	res := markCompletedRun(t, spy, PostShipSettings{Enabled: true},
		PostShipHook{Name: "done", Builtin: builtinMarkCompleted, Enabled: true})
	if r, _, _ := stepNamed(res, "done"); r != "ok" {
		t.Errorf("already completed must be ok, got %q", r)
	}
	if len(spy.calls) != 1 || !spy.calls[0].noop {
		t.Errorf("calls %v: want a single no-op call", spy.calls)
	}
}

func TestMarkCompletedFailureIsRecordedWithoutFailingTheRun(t *testing.T) {
	spy := &markCompletedSpy{err: errors.New("store down token=ghp_abcdefghijklmnopqrstuvwxyz0123456789")}
	ship := &smokeShip{}
	nf := &smokeNotifier{}
	ps := PostShipSettings{Enabled: true, NotifyTemplate: "{project} live", Hooks: []PostShipHook{
		{Name: "done", Builtin: builtinMarkCompleted, Enabled: true},
		{Name: "say", Builtin: builtinNotify, Enabled: true},
	}}
	r := &Runner{VersionFiles: testVersionFiles, Dir: smokeRepo(t), Project: "smoke",
		Resolver: smokePipeline(t), Ship: ship, Completer: spy, Notifier: nf, PostShip: ps}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}, AutomatonID: "prd-1"})
	if err != nil || res.Run.Result != "ok" {
		t.Fatalf("the merge result must stay ok: %q err %v", res.Run.Result, err)
	}
	result, detail, _ := stepNamed(res, "done")
	if result != "failed" || !strings.Contains(detail, "store down") || strings.Contains(detail, "ghp_abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Errorf("step result %q detail %q: want failed, with the detail redacted", result, detail)
	}
	if len(nf.delivered()) != 1 {
		t.Errorf("a later hook must still run")
	}
}

func TestMarkCompletedWithoutCompleterOrAutomatonFails(t *testing.T) {
	if _, err := RunMarkCompleted(context.Background(), nil, "prd-1", "1.0.0", "abc", time.Now()); err == nil {
		t.Error("nil completer must fail")
	}
	if _, err := RunMarkCompleted(context.Background(), &markCompletedSpy{}, " ", "1.0.0", "abc", time.Now()); err == nil {
		t.Error("an empty Automaton id must fail")
	}
}

func TestMarkCompletedNeverCancels(t *testing.T) {
	spy := &markCompletedSpy{}
	markCompletedRun(t, spy, PostShipSettings{Enabled: true},
		PostShipHook{Name: "done", Builtin: builtinMarkCompleted, Enabled: true})
	if spy.cancels != 0 {
		t.Errorf("cancel calls: %d", spy.cancels)
	}
	// The wiring type exposes exactly one method.
	var _ Completer = (*markCompletedSpy)(nil)
}
