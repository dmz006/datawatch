package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// draftPathFor names the tracked changelog draft file of automaton id, from
// the generated-artifact templates — the same path the runner reads.
func draftPathFor(t *testing.T, id string) string {
	t.Helper()
	p := genartifacts.TrackedPath(genartifacts.ChangelogDraft,
		&changerecord.ChangeRecord{AutomatonID: id}, genartifacts.Options{})
	if p != "docs/plans/drafts/"+id+"-changelog.md" {
		t.Fatalf("draft path %q, want docs/plans/drafts/%s-changelog.md", p, id)
	}
	return p
}

// TestChangelogDraftFileIsUsedWhenPresent pins the fix: when the Automaton's
// changelog draft file exists, the plan's heading and body come from that
// file and the recorded source names the file, not the entry task.
func TestChangelogDraftFileIsUsedWhenPresent(t *testing.T) {
	repo := runnerRepo(t)
	draftPath := draftPathFor(t, "a1")
	if err := os.MkdirAll(filepath.Join(repo, filepath.Dir(filepath.FromSlash(draftPath))), 0o750); err != nil {
		t.Fatal(err)
	}
	// The draft and the entry task carry different text; the plan must hold
	// the draft's, so the source is what makes the difference visible.
	write(t, repo, filepath.FromSlash(draftPath),
		"## [v1.3.0] - 2026-11-09\n\n### Added\n\n- from the draft file\n")

	in := runnerInput(t, repo, "work")
	in.ChangelogTasks = []ChangelogTask{{
		ID:     "t9",
		Kind:   ChangelogTaskKind,
		Output: "## [v1.3.0] - 2026-11-08\n\n- from the task output\n",
	}}

	plan, err := Runner{Gates: fakeGates(t, GatePass, new(int)), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if plan.ChangelogSource != draftPath {
		t.Errorf("changelog source %q, want the draft file %q", plan.ChangelogSource, draftPath)
	}
	if plan.ChangelogHeading != "## [v1.3.0] - 2026-11-09" {
		t.Errorf("heading %q, want the draft's heading", plan.ChangelogHeading)
	}
	if !strings.Contains(plan.ChangelogBody, "from the draft file") || strings.Contains(plan.ChangelogBody, "from the task output") {
		t.Errorf("body %q, want the draft's text and not the task's", plan.ChangelogBody)
	}
	if len(plan.Findings) != 0 {
		t.Errorf("findings %+v, want none: the entry names 1.3.0, which the plan names", plan.Findings)
	}

	stored, err := Load(in.DataDir, "a1")
	if err != nil {
		t.Fatalf("plan not read back through the store: %v", err)
	}
	if stored.ChangelogSource != draftPath {
		t.Errorf("stored changelog source %q, want the draft file %q", stored.ChangelogSource, draftPath)
	}
	if stored.ChangelogHeading != "## [v1.3.0] - 2026-11-09" || !strings.Contains(stored.ChangelogBody, "from the draft file") {
		t.Errorf("stored heading %q body %q, want the draft file's text", stored.ChangelogHeading, stored.ChangelogBody)
	}
	if strings.Contains(stored.ChangelogBody, "from the task output") {
		t.Errorf("stored body %q holds the task's text; the draft file must win", stored.ChangelogBody)
	}
}

// TestChangelogFallsBackToEntryTaskWithoutDraft pins the fix: with no draft
// file the plan takes the entry from the changelog task and names that task
// as the source.
func TestChangelogFallsBackToEntryTaskWithoutDraft(t *testing.T) {
	repo := runnerRepo(t)
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(draftPathFor(t, "a1")))); !os.IsNotExist(err) {
		t.Fatalf("the draft file should not exist: %v", err)
	}

	in := runnerInput(t, repo, "work") // its entry task carries the well-formed v1.3.0 text
	plan, err := Runner{Gates: fakeGates(t, GatePass, new(int)), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if plan.ChangelogSource != "t9" {
		t.Errorf("changelog source %q, want the entry task t9", plan.ChangelogSource)
	}
	if plan.ChangelogHeading != "## [v1.3.0] - 2026-11-09" || !strings.Contains(plan.ChangelogBody, "- thing") {
		t.Errorf("heading %q body %q, want the task's text", plan.ChangelogHeading, plan.ChangelogBody)
	}
	if len(plan.Findings) != 0 {
		t.Errorf("findings %+v, want none: the entry names 1.3.0, which the plan names", plan.Findings)
	}

	stored, err := Load(in.DataDir, "a1")
	if err != nil {
		t.Fatalf("plan not read back through the store: %v", err)
	}
	if stored.ChangelogSource != "t9" {
		t.Errorf("stored changelog source %q, want the entry task t9", stored.ChangelogSource)
	}
	if !strings.Contains(stored.ChangelogBody, "- thing") {
		t.Errorf("stored body %q, want the task's text", stored.ChangelogBody)
	}
}

// TestChangelogStaysMissingWithoutDraftOrTask pins the existing state: with
// neither a draft file nor a usable entry task the plan's changelog keeps
// its missing state — nothing is invented — and, because a version is
// planned, that is a blocking finding.
func TestChangelogStaysMissingWithoutDraftOrTask(t *testing.T) {
	repo := runnerRepo(t)
	if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(draftPathFor(t, "a1")))); !os.IsNotExist(err) {
		t.Fatalf("the draft file should not exist: %v", err)
	}

	in := runnerInput(t, repo, "work")
	in.ChangelogTasks = nil // no entry task, no output

	plan, err := Runner{Gates: fakeGates(t, GatePass, new(int)), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if plan.ChangelogSource != ChangelogSourceMissing {
		t.Errorf("changelog source %q, want the missing marker %q", plan.ChangelogSource, ChangelogSourceMissing)
	}
	if plan.ChangelogHeading != "" || plan.ChangelogBody != "" {
		t.Errorf("heading %q body %q, want both empty: nothing is invented", plan.ChangelogHeading, plan.ChangelogBody)
	}
	if got := blockingFinding(plan.Findings, "changelog_missing"); got == "" {
		t.Errorf("findings %+v, want a blocking changelog_missing: a version is planned and names no entry", plan.Findings)
	}

	stored, err := Load(in.DataDir, "a1")
	if err != nil {
		t.Fatalf("plan not read back through the store: %v", err)
	}
	if stored.ChangelogSource != ChangelogSourceMissing || stored.ChangelogHeading != "" || stored.ChangelogBody != "" {
		t.Errorf("stored changelog source %q heading %q body %q, want the missing state",
			stored.ChangelogSource, stored.ChangelogHeading, stored.ChangelogBody)
	}
	if got := blockingFinding(stored.Findings, "changelog_missing"); got == "" {
		t.Errorf("stored findings %+v, want a blocking changelog_missing", stored.Findings)
	}
}

// blockingFinding returns the message of the first blocking finding with
// code, or "" when none holds.
func blockingFinding(findings []Finding, code string) string {
	for _, f := range findings {
		if f.Code == code && f.Blocking {
			return f.Message
		}
	}
	return ""
}
