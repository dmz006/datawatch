package autonomous

import (
	"strings"
	"testing"
	"time"
)

// A merge step record of text only a backfill recorded is a new merge step
// record: it needs a commit id like any other.
func TestShippedMergeStepAfterBackfillStillNeedsACommitID(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	r.pushed(p.Git.Branch)
	const text = "ported-by-hand"
	if _, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), text, ShippedSourceBackfill); err != nil {
		t.Fatal(err)
	}
	g := r.watchGit()
	_, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), text, ShippedSourceMergeStep)
	if err == nil || !strings.Contains(err.Error(), "not a commit id") {
		t.Errorf("err = %v, want it to say it is not a commit id", err)
	}
	if len(g.calls) != 0 {
		t.Errorf("git ran before the value was refused: %q", g.calls)
	}
	got, _ := r.m.store.GetPRD(p.ID)
	if got.MergeCommit != text || got.ShippedSource != ShippedSourceBackfill {
		t.Errorf("the stored record changed: %q %q", got.MergeCommit, got.ShippedSource)
	}
	// The backfill record sent again is still the same record.
	if _, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), text, ShippedSourceBackfill); err != nil {
		t.Errorf("backfill again: %v", err)
	}
}

// A merge step record that is already stored stays a no-op when sent again,
// whatever its text: here it was stored while no merge repository was set.
func TestShippedStoredMergeStepRecordSentAgainIsANoOp(t *testing.T) {
	r := newMERig(t)
	p, _ := r.automaton(PRDCompleted)
	r.pushed(p.Git.Branch)
	setRepo := func(dir string) {
		cfg := r.m.Config()
		cfg.WorktreeBaseRepo = dir
		r.m.SetConfig(cfg)
	}
	setRepo("")
	const text = "kept-as-given"
	first, err := r.m.RecordPRDShipped(p.ID, "v1.0.1", time.Now(), text, ShippedSourceMergeStep)
	if err != nil {
		t.Fatal(err)
	}
	at := first.UpdatedAt
	setRepo(r.repo)
	if r.m.MergeBaseRepo() == "" {
		t.Fatal("no merge repository, so this proves nothing")
	}
	g := r.watchGit()
	again, err := r.m.RecordPRDShipped(p.ID, "v9.9.9", time.Now(), text, ShippedSourceMergeStep)
	if err != nil || again.MergedInRelease != "v1.0.1" || !again.UpdatedAt.Equal(at) || len(g.calls) != 0 {
		t.Fatalf("sent again: err=%v record=%+v git=%q", err, again, g.calls)
	}
}
