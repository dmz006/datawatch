// Tests for the worktree cleanup pass resolving what it skips: a work
// tree whose uncommitted work is only generated harness files is
// cleaned (or would be, in report mode) and the pass says exactly which
// files it resolved; a work tree with one real uncommitted file is held
// and the report lists that file by its exact path with the one action
// that releases the hold; and a safety-report hold entry surfaces its
// hold reason and release action.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// TestWorktreeCleanupWorktreeGeneratedOnlyUncommittedIsRemoved verifies
// that a work tree whose uncommitted work is only harness files the
// daemon and its launchers generate (.mcp.json, .aider.conf.yml,
// .goose/) does not stay an opaque "uncommitted work" skip: on mode
// removes it and names the resolved files in the audit record and in
// the removal entry, and report mode would-remove it the same way.
func TestWorktreeCleanupWorktreeGeneratedOnlyUncommittedIsRemoved(t *testing.T) {
	// Runs against a REAL git repository (NewMergeGitRunner): the strict
	// `git worktree remove` refuses when the tree holds uncommitted files,
	// so a pass that did not pass --force for the generated-only case would
	// land in the failed bucket and the assertions below would fail — this
	// is exactly the fake-success case the scripted version could not catch.
	probe := &fakeProbe{finished: true, base: "trunk", age: 48 * time.Hour, ageOK: true, size: 4096}
	runCase := func(mode string) (WorktreeCleanupResult, string) {
		gBase, worktree := fixtureRepo(t)
		// Two deterministic top-level generated harness files, each reported
		// individually by `git status --porcelain`.
		_ = os.WriteFile(filepath.Join(worktree, ".mcp.json"), []byte("{}\n"), 0o644)
		_ = os.WriteFile(filepath.Join(worktree, ".aider.conf.yml"), []byte("model: x\n"), 0o644)
		res := CleanupWorktrees(WorktreeCleanupOptions{
			Ctx:         context.Background(),
			Repo:        gBase,
			Report:      SafeDeleteReport{Safe: []SafeDeleteEntry{{Branch: "automaton/fixture", Worktree: worktree, Reason: "all commits on main"}}},
			Mode:        mode,
			Retention:   24 * time.Hour,
			MaxRemovals: 10,
			Git:         NewMergeGitRunner(),
			Probe:       probe,
			Audit:       func(string, map[string]any) error { return nil },
			Record:      nil,
		})
		return res, worktree
	}

	// on mode: the work tree is actually removed and the pass names the
	// exact generated files it resolved.
	res, worktree := runCase(config.CleanupModeOn)
	if len(res.Failed) != 0 {
		t.Fatalf("no failed entries expected (real git removed the tree), got %+v", res.Failed)
	}
	if len(res.Removed) != 1 {
		t.Fatalf("generated-only work tree must be cleaned, got removed=%+v skipped=%+v",
			res.Removed, res.Skipped)
	}
	got := append([]string(nil), res.Removed[0].ResolvedFiles...)
	sort.Strings(got)
	want := []string{".aider.conf.yml", ".mcp.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("removal must name the resolved generated files, got %q want %q", got, want)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("the real work tree %s must be gone after on-mode removal", worktree)
	}

	// report mode: would-remove the same tree with the same exact file list,
	// and touch nothing on disk.
	rep, rworktree := runCase(config.CleanupModeReport)
	if len(rep.Removed) != 0 || len(rep.WouldRemove) != 1 {
		t.Fatalf("report mode must would-remove the tree, got removed=%+v wouldRemove=%+v", rep.Removed, rep.WouldRemove)
	}
	g2 := append([]string(nil), rep.WouldRemove[0].ResolvedFiles...)
	sort.Strings(g2)
	if !reflect.DeepEqual(g2, want) {
		t.Fatalf("report-mode entry must name the exact generated files, got %q want %q", g2, want)
	}
	if _, err := os.Stat(rworktree); err != nil {
		t.Fatalf("report mode must not remove the work tree, got %v", err)
	}
}

// TestWorktreeCleanupWorktreeRealUncommittedWorkIsHeld verifies that a
// work tree with one non-generated uncommitted file is never touched:
// no removal in either mode, the skip entry carries the reason, the
// exact file list the hold is keyed on (the real file, not the
// generated ones), and the release action that lifts the hold.
func TestWorktreeCleanupWorktreeRealUncommittedWorkIsHeld(t *testing.T) {
	status := " M app.js\n?? .mcp.json\n?? notes.txt\n"
	mkResult := func(mode string) WorktreeCleanupResult {
		g := newScriptedGit()
		scriptStandardRemoval(t, g)
		g.out[keyOf(testWorktree, "status", "--porcelain")] = status
		return runCoreWith(g, eligibleProbe(), coreWorktrees(mode), mode,
			func(string, map[string]any) error { return nil }, nil)
	}

	for _, mode := range []string{config.CleanupModeOn, config.CleanupModeReport} {
		res := mkResult(mode)
		if len(res.Removed) != 0 || len(res.WouldRemove) != 0 {
			t.Fatalf("mode %s: a real uncommitted file must hold the work tree: removed=%+v wouldRemove=%+v",
				mode, res.Removed, res.WouldRemove)
		}
		if len(res.Skipped) != 1 || res.Skipped[0].Reason != cleanupSkipDirty {
			t.Fatalf("mode %s: the hold must be an uncommitted-work skip: %+v", mode, res.Skipped)
		}
		s := res.Skipped[0]
		want := []string{"app.js", "notes.txt"}
		if !reflect.DeepEqual(s.Files, want) {
			t.Fatalf("mode %s: the hold must list the exact real files it keys on, got %q", mode, s.Files)
		}
		if s.ReleaseAction == "" || strings.Contains(s.ReleaseAction, "uncommitted work") {
			t.Fatalf("mode %s: the hold must carry the release action, got %q", mode, s.ReleaseAction)
		}
	}
}

// TestWorktreeCleanupWorktreeSafetyReportHoldListsReleaseAction
// verifies that a hold from the safe-to-delete report itself surfaces
// the hold reason and the one concrete action that releases it —
// including the dirty-work hold whose release is the commit/discard
// action.
func TestWorktreeCleanupWorktreeSafetyReportHoldListsReleaseAction(t *testing.T) {
	report := SafeDeleteReport{
		Safe: []SafeDeleteEntry{{Branch: testBranch, Worktree: testWorktree, Reason: "all commits on main"}},
		Held: []SafeDeleteHeld{
			{Branch: "automaton/ahead", Worktree: "/repo/wt-ahead", State: MergeStateNeedsLook, Note: "commits ahead of main"},
			{Branch: "automaton/dirty", Worktree: "/repo/wt-dirty", Dirty: true, Note: "uncommitted edits"},
		},
	}
	g := newScriptedGit()
	scriptStandardRemoval(t, g)
	res := runCoreWith(g, eligibleProbe(), report, config.CleanupModeReport,
		func(string, map[string]any) error { return nil }, nil)

	if len(res.Skipped) != 2 {
		t.Fatalf("both held entries must surface as skips, got %+v", res.Skipped)
	}
	var ahead, dirty WorktreeCleanupSkipped
	for _, s := range res.Skipped {
		switch s.Branch {
		case "automaton/ahead":
			ahead = s
		case "automaton/dirty":
			dirty = s
		}
	}
	if ahead.Reason != cleanupSkipHeld || !strings.Contains(ahead.Detail, "commits ahead of main") {
		t.Fatalf("the held entry must carry its hold reason: %+v", ahead)
	}
	if !strings.Contains(ahead.ReleaseAction, "merging") {
		t.Fatalf("the held entry must carry the release action that matches what it is keyed on: %+v", ahead)
	}
	if dirty.Reason != cleanupSkipDirty {
		t.Fatalf("a dirty-hold entry must key on uncommitted work: %+v", dirty)
	}
	if dirty.ReleaseAction != worktreeSkipHoldReleaseAction {
		t.Fatalf("the dirty hold must carry the commit/discard release action, got %q", dirty.ReleaseAction)
	}
}

// TestWorktreeCleanupHoldTextNamesFilesAndReleaseAction verifies the
// read surface (CLI, MCP result and comm channel render it): the hold
// render names the held files exactly and the single action that
// releases the hold.
func TestWorktreeCleanupHoldTextNamesFilesAndReleaseAction(t *testing.T) {
	now := time.Now()
	st := CleanupStatus{
		DiskAlertPercent: 92,
		Worktrees: CleanupPassStatus{
			LastRun: &now, Mode: config.CleanupModeReport, Removed: 0, Skipped: 1,
		},
		LastWorktreeSkips: []WorktreeCleanupSkipped{{
			Branch: testBranch, Worktree: testWorktree, Reason: cleanupSkipDirty,
			Detail:        "the work tree has uncommitted work the cleanup cannot classify: notes.txt",
			Files:         []string{"app.js", "notes.txt"},
			ReleaseAction: worktreeSkipHoldReleaseAction,
		}},
	}
	text := RenderCleanupStatus(st)
	for _, want := range []string{
		"worktree hold at", testWorktree,
		"held files: app.js, notes.txt",
		"released by: commit the work or discard the held files",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the hold render must contain %q\ngot:\n%s", want, text)
		}
	}
}

// TestClassifyWorktreeUncommittedWorktreeGeneratedVsReal pins the
// classification itself: generated harness files are recognized by
// name and directory, tracked changes and unrecognized untracked files
// are not, and a tracked .mcp.json (a project that tracks it itself)
// stays real work.
func TestClassifyWorktreeUncommittedWorktreeGeneratedVsReal(t *testing.T) {
	generated, real := classifyWorktreeUncommitted(
		"?? .mcp.json\n" +
			"?? .aider.conf.yml\n" +
			"?? .goose/config.yaml\n" +
			"?? .gemini/settings.json\n" +
			" M app.js\n" +
			"?? notes.txt\n" +
			" M .mcp.json\n")
	if !reflect.DeepEqual(generated, []string{".mcp.json", ".aider.conf.yml", ".goose/config.yaml", ".gemini/settings.json"}) {
		t.Fatalf("generated list wrong: %q", generated)
	}
	if !reflect.DeepEqual(real, []string{"app.js", "notes.txt", ".mcp.json"}) {
		t.Fatalf("real list wrong: %q", real)
	}
}
