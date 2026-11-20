package supervisorloop

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakePackWriter struct {
	calls []packCall
	link  string
	err   error
}

type packCall struct{ project, ruleText string }

func (w *fakePackWriter) CommitRule(project, ruleText string) (string, error) {
	w.calls = append(w.calls, packCall{project, ruleText})
	return w.link, w.err
}

type fakeFiler struct {
	calls []fileCall
	link  string
	err   error
}

type fileCall struct{ project, title, body string }

func (f *fakeFiler) FileAutomaton(project, title, body string) (string, error) {
	f.calls = append(f.calls, fileCall{project, title, body})
	return f.link, f.err
}

func newCorrections() (*Corrections, *fakeMemory, *fakePackWriter, *fakeFiler) {
	m := newFakeMemory()
	w := &fakePackWriter{link: "pack://commit/7"}
	f := &fakeFiler{link: "plan://automaton/9"}
	clock := &stepClock{t: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	recs := NewStateRecords(m, clock)
	return NewCorrections(recs, w, f, clock), m, w, f
}

func TestCorrectionRecordGetList(t *testing.T) {
	c, m, _, _ := newCorrections()
	got := []Correction{
		{ID: "b", Text: "use the dry run first", Class: ClassStandingRule},
		{ID: "a", Text: "the deploy window is open", Class: ClassState},
		{ID: "c", Text: "checkout panics on empty cart", Evidence: "panic at cart.go:41", Class: ClassProductDefect, Workaround: "add an item before checkout"},
	}
	for _, corr := range got {
		if err := c.Record("alpha", "sup", corr, "op", "operator gave it"); err != nil {
			t.Fatal(err)
		}
	}
	if m.saves != 3 {
		t.Fatalf("saves=%d", m.saves)
	}
	// Get each.
	for _, want := range got {
		corr, ok, err := c.Get("alpha", "sup", want.ID)
		if err != nil || !ok {
			t.Fatalf("get %s: ok=%v err=%v", want.ID, ok, err)
		}
		if corr.Text != want.Text || corr.Class != want.Class || corr.Status != StatusActive {
			t.Fatalf("%+v", corr)
		}
		if corr.Workaround != want.Workaround || corr.Evidence != want.Evidence {
			t.Fatalf("%+v", corr)
		}
	}
	if _, ok, err := c.Get("alpha", "sup", "zzz"); err != nil || ok {
		t.Fatalf("missing: ok=%v err=%v", ok, err)
	}
	// List is ordered by id and only holds the correction section.
	_ = c.recs.SetEntry("alpha", "sup", StateEntry{ID: "w1", Section: SectionWork, Text: "ship"}, "op", "w")
	list, err := c.List("alpha", "sup")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].ID != "a" || list[1].ID != "b" || list[2].ID != "c" {
		t.Fatalf("%+v", list)
	}
}

func TestCorrectionRecordIdempotent(t *testing.T) {
	m := newFakeMemory()
	w := &fakePackWriter{link: "pack://commit/7"}
	f := &fakeFiler{link: "plan://automaton/9"}
	fixed := hoClock{t: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	recs := NewStateRecords(m, &fixed)
	c := NewCorrections(recs, w, f, &fixed)
	corr := Correction{ID: "a", Text: "hold the merge", Class: ClassState}
	for i := 0; i < 2; i++ {
		if err := c.Record("alpha", "sup", corr, "op", "operator gave it"); err != nil {
			t.Fatal(err)
		}
	}
	if m.saves != 1 {
		t.Fatalf("saves=%d", m.saves)
	}
	rec, _ := recs.Get("alpha", "sup")
	if len(rec.Entries) != 1 || len(rec.Entries[0].Edits) != 1 {
		t.Fatalf("%+v", rec)
	}
}

func TestCorrectionRecordValidation(t *testing.T) {
	c, m, _, _ := newCorrections()
	bad := []Correction{
		{Text: "t", Class: ClassState},                              // no id
		{ID: "a", Class: ClassState},                                // no text
		{ID: "a", Text: "t", Class: "vibes"},                        // bad class
		{ID: "a", Text: "t", Class: ClassState, Status: "promoted"}, // store assigns status
	}
	for _, corr := range bad {
		if err := c.Record("alpha", "sup", corr, "op", "w"); !errors.Is(err, ErrCorrection) {
			t.Fatalf("%+v -> %v", corr, err)
		}
	}
	if err := c.Record("alpha", "sup", Correction{ID: "a", Text: "t", Class: ClassState}, "", "w"); !errors.Is(err, ErrCorrection) {
		t.Fatal(err)
	}
	if err := c.Record("alpha", "sup", Correction{ID: "a", Text: "t", Class: ClassState}, "op", ""); !errors.Is(err, ErrCorrection) {
		t.Fatal(err)
	}
	// A bad payload in the record surfaces on read.
	m.pinned[StateKey("alpha", "sup")] = `{"project":"alpha","role":"sup","entries":[{"id":"corr/a","section":"correction","text":"{bad","edits":[{"who":"x","why":"y"}]}]}`
	if _, _, err := c.Get("alpha", "sup", "a"); err == nil {
		t.Fatal("want decode error")
	}
	// Store errors propagate.
	m.err = errors.New("down")
	if err := c.Record("alpha", "sup", Correction{ID: "a", Text: "t", Class: ClassState}, "op", "w"); err == nil {
		t.Fatal("want store error")
	}
}

func TestCorrectionExpireState(t *testing.T) {
	c, _, w, f := newCorrections()
	_ = c.Record("alpha", "sup", Correction{ID: "s", Text: "deploy window open", Class: ClassState}, "op", "w")
	_ = c.Record("alpha", "sup", Correction{ID: "r", Text: "always dry run", Class: ClassStandingRule}, "op", "w")
	if len(w.calls) != 0 || len(f.calls) != 0 {
		t.Fatalf("writers called early: %+v %+v", w.calls, f.calls)
	}
	corr, err := c.Expire("alpha", "sup", "s", "op", "window closed")
	if err != nil {
		t.Fatal(err)
	}
	if corr.Status != StatusExpired {
		t.Fatalf("%+v", corr)
	}
	if corr.UpdatedAt.IsZero() || corr.CreatedAt.IsZero() {
		t.Fatalf("timestamps %+v", corr)
	}
	if list, _ := c.List("alpha", "sup"); len(list) != 1 || list[0].ID != "r" {
		t.Fatalf("%+v", list)
	}
	// Expiring a standing rule is refused.
	if _, err := c.Expire("alpha", "sup", "r", "op", "w"); !errors.Is(err, ErrCorrectionClass) {
		t.Fatal(err)
	}
	// Expiring a missing id is a not-found.
	if _, err := c.Expire("alpha", "sup", "zzz", "op", "w"); !errors.Is(err, ErrCorrectionNotFound) {
		t.Fatal(err)
	}
	// Expiring the already-expired one is a not-found (it left the record).
	if _, err := c.Expire("alpha", "sup", "s", "op", "w"); !errors.Is(err, ErrCorrectionNotFound) {
		t.Fatal(err)
	}
	if _, err := c.Expire("alpha", "sup", "r", "", "w"); !errors.Is(err, ErrCorrection) {
		t.Fatal(err)
	}
}

func TestCorrectionPromoteStandingRule(t *testing.T) {
	c, _, w, f := newCorrections()
	_ = c.Record("alpha", "sup", Correction{ID: "r", Text: "always dry run", Evidence: "deploy broke on 2026-01-30", Class: ClassStandingRule}, "op", "w")
	if len(f.calls) != 0 {
		t.Fatalf("filer called for a rule: %+v", f.calls)
	}
	corr, err := c.Promote("alpha", "sup", "r", "op", "make it durable")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.calls) != 1 {
		t.Fatalf("%+v", w.calls)
	}
	if w.calls[0].project != "alpha" || !strings.Contains(w.calls[0].ruleText, "always dry run") || !strings.Contains(w.calls[0].ruleText, "deploy broke on 2026-01-30") {
		t.Fatalf("%+v", w.calls[0])
	}
	if corr.Link != "pack://commit/7" || corr.Status != StatusPromoted {
		t.Fatalf("%+v", corr)
	}
	// The rule left the state record; a fresh session reads it from the pack.
	if list, _ := c.List("alpha", "sup"); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
	if lines, _ := c.HandoverLines("alpha", "sup"); len(lines) != 0 {
		t.Fatalf("%+v", lines)
	}
	// Class guards and not-found.
	_ = c.Record("alpha", "sup", Correction{ID: "d", Text: "defect", Class: ClassProductDefect}, "op", "w")
	if _, err := c.Promote("alpha", "sup", "d", "op", "w"); !errors.Is(err, ErrCorrectionClass) {
		t.Fatal(err)
	}
	if _, err := c.Promote("alpha", "sup", "zzz", "op", "w"); !errors.Is(err, ErrCorrectionNotFound) {
		t.Fatal(err)
	}
	if _, err := c.Promote("alpha", "sup", "d", "", "w"); !errors.Is(err, ErrCorrection) {
		t.Fatal(err)
	}
	// A pack writer failure leaves the correction active and untouched.
	_ = c.Record("alpha", "sup", Correction{ID: "r2", Text: "another rule", Class: ClassStandingRule}, "op", "w")
	w.err = errors.New("pack offline")
	before, _, _ := c.Get("alpha", "sup", "r2")
	if _, err := c.Promote("alpha", "sup", "r2", "op", "w"); err == nil || !strings.Contains(err.Error(), "pack offline") {
		t.Fatalf("err=%v", err)
	}
	after, ok, _ := c.Get("alpha", "sup", "r2")
	if !ok || after.Status != before.Status || after.Link != "" {
		t.Fatalf("%+v %+v", before, after)
	}
	// Without a writer configured, promotion is refused.
	w.err = nil
	if _, err := NewCorrections(c.recs, nil, nil, c.clock).Promote("alpha", "sup", "d", "op", "w"); err == nil {
		t.Fatal("want no-writer error")
	}
}

func TestCorrectionFileProductDefect(t *testing.T) {
	c, _, w, f := newCorrections()
	_ = c.Record("alpha", "sup", Correction{ID: "d", Text: "checkout panics on empty cart", Evidence: "panic at cart.go:41", Class: ClassProductDefect, Workaround: "add an item before checkout"}, "op", "w")
	_ = c.Record("alpha", "sup", Correction{ID: "s", Text: "deploy window open", Class: ClassState}, "op", "w")
	if len(w.calls) != 0 {
		t.Fatalf("pack writer called for a defect: %+v", w.calls)
	}
	corr, err := c.File("alpha", "sup", "d", "op", "file it")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("%+v", f.calls)
	}
	if f.calls[0].project != "alpha" || f.calls[0].title != "checkout panics on empty cart" || f.calls[0].body != "panic at cart.go:41" {
		t.Fatalf("%+v", f.calls[0])
	}
	if corr.Link != "plan://automaton/9" || corr.Status != StatusFiled {
		t.Fatalf("%+v", corr)
	}
	// The filed correction stays in the state record, with link and workaround.
	stored, ok, err := c.Get("alpha", "sup", "d")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if stored.Link != "plan://automaton/9" || stored.Status != StatusFiled || stored.Workaround != "add an item before checkout" {
		t.Fatalf("%+v", stored)
	}
	if list, _ := c.List("alpha", "sup"); len(list) != 2 {
		t.Fatalf("%+v", list)
	}
	// Class guard, status guard, not-found.
	if _, err := c.File("alpha", "sup", "s", "op", "w"); !errors.Is(err, ErrCorrectionClass) {
		t.Fatal(err)
	}
	if _, err := c.File("alpha", "sup", "d", "op", "again"); !errors.Is(err, ErrCorrectionClass) && !strings.Contains(err.Error(), "only active") {
		t.Fatal(err)
	}
	if _, err := c.File("alpha", "sup", "zzz", "op", "w"); !errors.Is(err, ErrCorrectionNotFound) {
		t.Fatal(err)
	}
	if _, err := c.File("alpha", "sup", "s", "", "w"); !errors.Is(err, ErrCorrection) {
		t.Fatal(err)
	}
	// A filer failure leaves the correction active with no link.
	f.err = errors.New("plans offline")
	_ = c.Record("alpha", "sup", Correction{ID: "d3", Text: "import loop", Class: ClassProductDefect}, "op", "w")
	if _, err := c.File("alpha", "sup", "d3", "op", "w"); err == nil || !strings.Contains(err.Error(), "plans offline") {
		t.Fatalf("err=%v", err)
	}
	if len(f.calls) != 2 { // the failed attempt was recorded by the fake
		t.Fatalf("%+v", f.calls)
	}
	stored, _, _ = c.Get("alpha", "sup", "d3")
	if stored.Status != StatusActive || stored.Link != "" {
		t.Fatalf("%+v", stored)
	}
	// Filing a defect with no evidence files the text as the body.
	f.err = nil
	if _, err := c.File("alpha", "sup", "d3", "op", "w"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 3 || f.calls[2].body != "import loop" {
		t.Fatalf("%+v", f.calls)
	}
}

func TestCorrectionHandoverLines(t *testing.T) {
	c, _, _, _ := newCorrections()
	_ = c.Record("alpha", "sup", Correction{ID: "s", Text: "deploy window open", Class: ClassState}, "op", "w")
	_ = c.Record("alpha", "sup", Correction{ID: "r", Text: "always dry run", Class: ClassStandingRule}, "op", "w")
	_ = c.Record("alpha", "sup", Correction{ID: "d", Text: "checkout panics", Evidence: "cart.go:41", Class: ClassProductDefect, Workaround: "add an item first"}, "op", "w")
	_, err := c.File("alpha", "sup", "d", "op", "file it")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := c.HandoverLines("alpha", "sup")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("%v", lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"s: deploy window open",
		"r: always dry run",
		"d: checkout panics (evidence: cart.go:41)",
		"workaround: add an item first",
		"[link: plan://automaton/9]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in\n%s", want, joined)
		}
	}
	// Promote the standing rule: it leaves the handover (the pack carries it).
	if _, err := c.Promote("alpha", "sup", "r", "op", "durable"); err != nil {
		t.Fatal(err)
	}
	lines, _ = c.HandoverLines("alpha", "sup")
	if len(lines) != 2 || strings.Contains(strings.Join(lines, "\n"), "always dry run") {
		t.Fatalf("%v", lines)
	}
	// Expire the state correction: it leaves the handover too.
	if _, err := c.Expire("alpha", "sup", "s", "op", "window closed"); err != nil {
		t.Fatal(err)
	}
	lines, _ = c.HandoverLines("alpha", "sup")
	if len(lines) != 1 || !strings.Contains(lines[0], "checkout panics") {
		t.Fatalf("%v", lines)
	}
	// An empty record yields no lines, not an error.
	if lines, err := c.HandoverLines("beta", "sup"); err != nil || len(lines) != 0 {
		t.Fatalf("%v %v", lines, err)
	}
}

func TestCorrectionTimesFromInjectedClock(t *testing.T) {
	m := newFakeMemory()
	w := &fakePackWriter{link: "pack://commit/7"}
	f := &fakeFiler{link: "plan://automaton/9"}
	t0 := time.Date(2026, 3, 5, 8, 0, 0, 0, time.UTC)
	clock := &stepClock{t: t0}
	recs := NewStateRecords(m, clock)
	c := NewCorrections(recs, w, f, clock)
	if err := c.Record("alpha", "sup", Correction{ID: "r", Text: "rule", Class: ClassStandingRule}, "op", "w"); err != nil {
		t.Fatal(err)
	}
	corr, ok, _ := c.Get("alpha", "sup", "r")
	if !ok {
		t.Fatal("missing")
	}
	// Times come from the injected clock, not the wall clock. The state
	// record shares the clock (it stamps its own edits), so Promote's
	// update lands three ticks in: Record, record-edit, Promote.
	if !corr.CreatedAt.Equal(t0.Add(time.Minute)) || !corr.UpdatedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("%+v", corr)
	}
	corr, err := c.Promote("alpha", "sup", "r", "op", "durable")
	if err != nil {
		t.Fatal(err)
	}
	if !corr.UpdatedAt.Equal(t0.Add(3 * time.Minute)) {
		t.Fatalf("%+v", corr)
	}
}
