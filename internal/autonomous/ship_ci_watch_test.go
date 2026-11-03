package autonomous

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type stubLister struct {
	byRef map[string][]CIWatchRun
	err   error
	calls []CIRef
}

func (s *stubLister) ListRuns(_ context.Context, _ string, ref CIRef) ([]CIWatchRun, error) {
	s.calls = append(s.calls, ref)
	if s.err != nil {
		return nil, s.err
	}
	return s.byRef[ref.String()], nil
}

func run(name, status, concl string) CIWatchRun {
	return CIWatchRun{ID: 1, Name: name, Status: status, Conclusion: concl}
}

func opts() ShipCIWatchOptions {
	return ShipCIWatchOptions{OwnerRepo: "o/r", Commit: "abc", Tag: "v1", Poll: time.Millisecond}
}

func TestWatchShipCI_SuccessBothRefs(t *testing.T) {
	l := &stubLister{byRef: map[string][]CIWatchRun{
		"commit abc": {run("CI", "completed", "success")},
		"tag v1":     {run("Release", "completed", "success")},
	}}
	res := WatchShipCI(context.Background(), l, opts())
	if len(res) != 2 || res[0].Outcome != CIOutcomeSuccess || res[1].Outcome != CIOutcomeSuccess {
		t.Fatalf("got %+v", res)
	}
}

func TestWatchShipCI_Failure(t *testing.T) {
	l := &stubLister{byRef: map[string][]CIWatchRun{
		"commit abc": {run("CI", "completed", "failure")},
		"tag v1":     {run("Release", "completed", "success")},
	}}
	res := WatchShipCI(context.Background(), l, opts())
	if res[0].Outcome != CIOutcomeFailure || len(res[0].Failed) != 1 {
		t.Fatalf("commit: %+v", res[0])
	}
	if res[1].Outcome != CIOutcomeSuccess {
		t.Fatalf("tag: %+v", res[1])
	}
}

func TestWatchShipCI_StillRunningWhenNoTimeout(t *testing.T) {
	l := &stubLister{byRef: map[string][]CIWatchRun{"commit abc": {run("CI", "in_progress", "")}}}
	o := opts()
	o.Tag = ""
	res := WatchShipCI(context.Background(), l, o)
	if len(res) != 1 || res[0].Outcome != CIOutcomeRunning {
		t.Fatalf("got %+v", res)
	}
}

func TestWatchShipCI_Timeout(t *testing.T) {
	l := &stubLister{byRef: map[string][]CIWatchRun{"commit abc": {run("CI", "queued", "")}}}
	o := opts()
	o.Tag = ""
	o.Timeout = 20 * time.Millisecond
	res := WatchShipCI(context.Background(), l, o)
	if res[0].Outcome != CIOutcomeTimeout {
		t.Fatalf("got %+v", res[0])
	}
}

func TestWatchShipCI_WorkflowFilter(t *testing.T) {
	runs := []CIWatchRun{run("CI", "completed", "success"), run("Docs", "completed", "failure")}
	l := &stubLister{byRef: map[string][]CIWatchRun{"commit abc": runs}}
	o := opts()
	o.Tag = ""
	o.Workflows = []string{"ci"}
	if res := WatchShipCI(context.Background(), l, o); res[0].Outcome != CIOutcomeSuccess {
		t.Fatalf("filtered: %+v", res[0])
	}
	o.Workflows = nil
	if res := WatchShipCI(context.Background(), l, o); res[0].Outcome != CIOutcomeFailure {
		t.Fatalf("unfiltered: %+v", res[0])
	}
}

func TestWatchShipCI_NoTagSkipsTagWatch(t *testing.T) {
	l := &stubLister{byRef: map[string][]CIWatchRun{"commit abc": {run("CI", "completed", "success")}}}
	o := opts()
	o.Tag = ""
	WatchShipCI(context.Background(), l, o)
	if len(l.calls) != 1 || l.calls[0].Tag != "" {
		t.Fatalf("calls %+v", l.calls)
	}
}

func TestWatchShipCI_ListErrorTimesOutNotPanics(t *testing.T) {
	l := &stubLister{err: errors.New("boom")}
	o := opts()
	o.Tag = ""
	o.Timeout = 10 * time.Millisecond
	if res := WatchShipCI(context.Background(), l, o); res[0].Outcome != CIOutcomeTimeout {
		t.Fatalf("got %+v", res[0])
	}
}

func TestGhRunListArgs(t *testing.T) {
	got := ghRunListArgs("o/r", CIRef{Commit: "abc"})
	want := []string{"run", "list", "--repo", "o/r", "--commit", "abc", "--json", "databaseId,name,status,conclusion,url", "--limit", "50"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commit args %v", got)
	}
	got = ghRunListArgs("o/r", CIRef{Tag: "v1"})
	if got[4] != "--branch" || got[5] != "v1" {
		t.Fatalf("tag args %v", got)
	}
}

func TestRecordShipCIWatch_ModeBehaviour(t *testing.T) {
	failing := []RefCIResult{{Ref: CIRef{Commit: "abc"}, Outcome: CIOutcomeFailure,
		Failed: []CIWatchRun{run("CI", "completed", "failure")}}}
	for _, tc := range []struct {
		mode    string
		noted   bool
		alerted bool
	}{{"on", true, true}, {"shadow", true, false}} {
		m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
		if err != nil {
			t.Fatal(err)
		}
		prd, err := m.CreatePRD("spec", t.TempDir(), "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		m.recordShipCIWatch(prd, ShipPolicy{CIWatch: tc.mode}, failing)
		snap, ok := m.store.SnapshotPRD(prd.ID)
		if !ok || snap.Triage == nil {
			t.Fatalf("%s: triage missing", tc.mode)
		}
		noted := false
		for _, n := range snap.Triage.Notes {
			if n.Source == shipCIWatchTriageSource {
				noted = true
			}
		}
		if noted != tc.noted || (snap.Triage.AlertedAt != nil) != tc.alerted {
			t.Errorf("%s: noted=%v alerted=%v", tc.mode, noted, snap.Triage.AlertedAt != nil)
		}
	}
}

func TestRunShipCIWatch_OffSkips(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	l := &stubLister{}
	prd, _ := m.CreatePRD("spec", t.TempDir(), "", "", "")
	if got := m.runShipCIWatch(prd, ShipPolicy{Push: "on", Tag: "on", CIWatch: "off"}, &ShipResult{Tag: "v1"}, l); got != nil || len(l.calls) != 0 {
		t.Fatalf("off must skip, got %+v calls %v", got, l.calls)
	}
}
