package acceptance

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type fakeRunner struct {
	out   map[string]RunOutcome // by commit
	calls []string
	hook  func()
}

func (f *fakeRunner) Run(_ context.Context, _ CheckSpec, sub Subject, _ time.Duration) RunOutcome {
	f.calls = append(f.calls, sub.Commit)
	if f.hook != nil {
		f.hook()
	}
	if o, ok := f.out[sub.Commit]; ok {
		return o
	}
	return RunOutcome{ExitCode: -1, Reason: "no fake outcome"}
}

type fakeRepo map[string]map[string]string // commit -> path -> content

func (f fakeRepo) ObjectID(_ context.Context, sub Subject, p string) (string, bool, error) {
	v, ok := f[sub.Commit][p]
	return hashOf(v), ok, nil
}

func (f fakeRepo) ReadFile(_ context.Context, sub Subject, p string) ([]byte, bool, error) {
	v, ok := f[sub.Commit][p]
	return []byte(v), ok, nil
}

type fakeResolver struct{ at map[string]string } // effect -> commit; "" is the work

func (f *fakeResolver) Resolve(_ context.Context, _, effect string) (Subject, error) {
	c, ok := f.at[effect]
	if !ok {
		return Subject{}, errors.New("unknown effect " + effect)
	}
	return Subject{Repo: "fake", Commit: c}, nil
}

type fakeProbe struct {
	read  func() (Reading, error)
	calls int
}

func (f *fakeProbe) Read(context.Context, ProbeRequest) (Reading, error) {
	f.calls++
	return f.read()
}

type fakeReviewer struct {
	settings string
	value    ResultValue
	noQuote  bool
	err      error
	held     time.Time
	calls    int
}

func (f *fakeReviewer) Settings() string { return f.settings }
func (f *fakeReviewer) Held() (string, time.Time, bool) {
	return "usage", f.held, !f.held.IsZero()
}
func (f *fakeReviewer) Review(_ context.Context, r ReviewRequest) (ReviewReply, error) {
	f.calls++
	if f.err != nil {
		return ReviewReply{}, f.err
	}
	out := ReviewReply{ItemID: r.Item.ID, Value: f.value, Evidence: []Evidence{{Origin: OriginReviewer, What: "file.go:1"}}}
	if f.noQuote {
		out.Evidence = nil
	}
	return out, nil
}

var (
	operator = Caller{Claimed: "operator", Observed: "admin-token"}
	worker   = Caller{Claimed: "operator", Observed: "session:w1", SessionID: "w1"}
	shadow   = Caller{Claimed: "operator", Observed: "session:s9", SessionID: "s9"}
)

const (
	passLine = "--- PASS: TestX"
	failLine = "--- FAIL: TestX"
)

var (
	startFails = RunOutcome{Finished: true, ExitCode: 1, Output: failLine + " (0.00s)\nFAIL"}
	afterPass  = RunOutcome{Finished: true, Output: "=== RUN TestX\n" + passLine + " (0.00s)\nok"}
)

type world struct {
	t        *testing.T
	now      time.Time
	mode     string
	set      Settings
	runner   *fakeRunner
	repo     fakeRepo
	res      *fakeResolver
	probes   map[string]Probe
	reviewer *fakeReviewer
	path     string
	svc      *Service
}

func newWorld(t *testing.T, opts ...func(*world)) *world {
	t.Helper()
	w := &world{
		t: t, now: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC), mode: ModeReport,
		set: Settings{ProjectChecks: map[string]ProjectCheck{"unit": {
			Command: "make test", Inputs: []string{"Makefile"}, Expect: []Expect{{Pass: passLine, Fail: failLine}},
		}}},
		runner: &fakeRunner{out: map[string]RunOutcome{"c0": startFails, "c1": afterPass}},
		repo:   fakeRepo{"c0": {"Makefile": "test"}, "c1": {"Makefile": "test"}, "c2": {"Makefile": "test"}},
		res:    &fakeResolver{at: map[string]string{"": "c0"}},
		probes: map[string]Probe{},
		path:   filepath.Join(t.TempDir(), "acceptance.json"),
	}
	for _, o := range opts {
		o(w)
	}
	w.open()
	return w
}

func (w *world) open() {
	w.t.Helper()
	o := Options{
		Path: w.path, Mode: func() string { return w.mode }, Settings: func() Settings { return w.set },
		Runner: w.runner, Repo: w.repo, Resolver: w.res, Probes: w.probes, Now: func() time.Time { return w.now },
	}
	if w.reviewer != nil {
		o.Reviewer = w.reviewer
	}
	s, err := Open(o)
	if err != nil {
		w.t.Fatalf("Open: %v", err)
	}
	w.svc = s
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

const oneAsk = "Make the export work"

// fix is a change item proven by the saved project check.
func fix() Item {
	return Item{ID: "fix", Statement: "the export test passes", From: oneAsk, Method: "project:unit", Change: true, Before: BeforeFailFirst, Effect: "work"}
}

// frozen makes a fixed list with the given items.
func (w *world) frozen(id, source, ask string, items ...Item) *List {
	w.t.Helper()
	l, err := w.svc.Edit(context.Background(), EditInput{RequestID: id, Source: source, Ask: ask, Upsert: items}, operator)
	if err != nil {
		w.t.Fatalf("Edit: %v", err)
	}
	var drop []string
	if source == "bug" || source == "code" {
		drop = []string{"d-live-state", "d-connected"}
	}
	if len(drop) > 0 {
		l = must[*List](w.t)(w.svc.Edit(context.Background(), EditInput{RequestID: id, Remove: drop}, operator))
	}
	return must[*List](w.t)(w.svc.Freeze(context.Background(), id, l.Hash(), operator, "start"))
}

func (w *world) cover(id string) *List {
	w.t.Helper()
	l := must[*List](w.t)(w.svc.Get(id))
	return must[*List](w.t)(w.svc.Confirm(id, coverageItem, true, "the list covers the ask", "", l.Hash(), operator))
}

// work moves the subject to a new version and evaluates.
func (w *world) work(id, commit string) *List {
	w.t.Helper()
	w.res.at[""] = commit
	return must[*List](w.t)(w.svc.Evaluate(context.Background(), id))
}

func read(l *List, id string) ItemRead {
	for _, r := range Read(l).Items {
		if r.ID == id {
			return r
		}
	}
	return ItemRead{}
}

func wantStatus(t *testing.T, l *List, want Status) {
	t.Helper()
	if got := Read(l); got.Status != want {
		t.Fatalf("status = %s %v, want %s; items %+v", got.Status, got.Reasons, want, got.Items)
	}
}

func wantNotMet(t *testing.T, l *List, id string, contains string) {
	t.Helper()
	r := read(l, id)
	if r.Result.Value == ResultMet || Read(l).Done {
		t.Fatalf("%s reads %q done=%v, must not be met", id, r.Result.Value, Read(l).Done)
	}
	if contains != "" && !containsStr(r.Result.Reason, contains) {
		t.Fatalf("%s reason = %q, want it to contain %q", id, r.Result.Reason, contains)
	}
}
