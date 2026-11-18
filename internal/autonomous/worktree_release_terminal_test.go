package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// residueSink collects the audit and record calls of a pass (fakes).
type residueSink struct {
	mu      sync.Mutex
	audits  []string
	records []string
}

func (s *residueSink) audit(action string, _ map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, action)
	return nil
}

func (s *residueSink) record(_ string, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, text)
	return nil
}

func (s *residueSink) recordedAll() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.records, "\n")
}

func residueOpts(baseRepo, branch, worktree string, sink *residueSink) ResidualReleaseOptions {
	return ResidualReleaseOptions{
		Repo: baseRepo, Base: "trunk", Trigger: ResidueTriggerMerged,
		Branch: branch, Worktree: worktree, Git: NewMergeGitRunner(),
		Audit: sink.audit, Record: sink.record,
	}
}

func writeResidueFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func heldReasons(r ResidualReleaseResult) []string {
	var out []string
	for _, h := range r.Held {
		out = append(out, h.Kind+":"+h.Reason)
	}
	return out
}

func hasReason(r ResidualReleaseResult, kind, reason string) bool {
	for _, h := range r.Held {
		if h.Kind == kind && h.Reason == reason {
			return true
		}
	}
	return false
}

func TestReleaseAtTerminalTransition(t *testing.T) {
	const branch = "automaton/fixture"

	t.Run("merged work tree, task work tree and branch are removed and recorded", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		taskWT := filepath.Join(filepath.Dir(wt), "task-one")
		gitCmd(t, baseRepo, "worktree", "add", "-q", "-b", "automaton/fixture-task1", taskWT, branch)
		sink := &residueSink{}
		opts := residueOpts(baseRepo, branch, wt, sink)
		opts.TaskTrees = []ResidualTaskTree{{Path: taskWT, Branch: "automaton/fixture-task1"}}

		res := ResidualReleasePass(opts)

		if len(res.Held) != 0 {
			t.Fatalf("nothing should be held: %v", heldReasons(res))
		}
		if len(res.Removed) != 3 {
			t.Fatalf("want task work tree, work tree and branch removed, got %+v", res.Removed)
		}
		for _, p := range []string{wt, taskWT} {
			if _, err := os.Stat(p); err == nil {
				t.Fatalf("%s must be gone", p)
			}
		}
		if branchExists(t, baseRepo, branch) {
			t.Fatalf("the local branch must be gone")
		}
		if got := sink.recordedAll(); !strings.Contains(got, "removed") || !strings.Contains(got, "merged") {
			t.Fatalf("removals must be recorded with the trigger, got %q", got)
		}
		if len(sink.audits) == 0 {
			t.Fatalf("removals must be audited")
		}
	})

	t.Run("held for unmerged work", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		writeResidueFile(t, wt, "feature.txt", "unique work\n")
		gitCmd(t, wt, "add", "feature.txt")
		gitCmd(t, wt, "commit", "-q", "-m", "work not on trunk")
		sink := &residueSink{}

		res := ResidualReleasePass(residueOpts(baseRepo, branch, wt, sink))

		if len(res.Removed) != 0 || !hasReason(res, ResidueKindBranch, ResidueHeldUnmerged) {
			t.Fatalf("want held_unmerged on the branch and nothing removed, got removed=%v held=%v", res.Removed, heldReasons(res))
		}
		if !branchExists(t, baseRepo, branch) {
			t.Fatalf("an unmerged branch must stay")
		}
		if _, err := os.Stat(wt); err != nil {
			t.Fatalf("an unmerged work tree must stay: %v", err)
		}
		if !strings.Contains(sink.recordedAll(), ResidueHeldUnmerged) {
			t.Fatalf("the hold reason must be recorded, got %q", sink.recordedAll())
		}
	})

	t.Run("held for hand-written uncommitted files", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		writeResidueFile(t, wt, "notes.txt", "written by hand\n")
		sink := &residueSink{}

		res := ResidualReleasePass(residueOpts(baseRepo, branch, wt, sink))

		if len(res.Removed) != 0 || !hasReason(res, ResidueKindWorktree, ResidueHeldHandWritten) {
			t.Fatalf("want held_hand_written, got removed=%v held=%v", res.Removed, heldReasons(res))
		}
		if _, err := os.Stat(filepath.Join(wt, "notes.txt")); err != nil {
			t.Fatalf("the hand-written file must survive: %v", err)
		}
		if !branchExists(t, baseRepo, branch) {
			t.Fatalf("the branch is checked out in the held work tree and must stay")
		}
		if !strings.Contains(sink.recordedAll(), ResidueHeldHandWritten) {
			t.Fatalf("the hold reason must be recorded, got %q", sink.recordedAll())
		}
	})

	t.Run("held for a live user and for an unmerged task branch", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		taskWT := filepath.Join(filepath.Dir(wt), "task-two")
		gitCmd(t, baseRepo, "worktree", "add", "-q", "-b", "automaton/fixture-task2", taskWT, branch)
		writeResidueFile(t, taskWT, "t.txt", "x\n")
		gitCmd(t, taskWT, "add", "t.txt")
		gitCmd(t, taskWT, "commit", "-q", "-m", "task work")
		opts := residueOpts(baseRepo, branch, wt, &residueSink{})
		opts.TaskTrees = []ResidualTaskTree{{Path: taskWT, Branch: "automaton/fixture-task2"}}
		opts.InUse = func(p string) bool { return p == wt }

		res := ResidualReleasePass(opts)

		if !hasReason(res, ResidueKindTaskWorktree, ResidueHeldUnmerged) || !hasReason(res, ResidueKindWorktree, ResidueHeldInUse) {
			t.Fatalf("want task held_unmerged and work tree held_in_use, got %v", heldReasons(res))
		}
		if _, err := os.Stat(taskWT); err != nil {
			t.Fatalf("the unmerged task work tree must stay")
		}
	})

	t.Run("dry run removes nothing", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		opts := residueOpts(baseRepo, branch, wt, &residueSink{})
		opts.DryRun = true
		res := ResidualReleasePass(opts)
		if len(res.Removed) != 2 || !branchExists(t, baseRepo, branch) {
			t.Fatalf("dry run lists the would-be removals only, got %+v", res.Removed)
		}
		if _, err := os.Stat(wt); err != nil {
			t.Fatalf("dry run must keep the work tree")
		}
	})

	t.Run("cancel archive and the periodic pass release through the manager", func(t *testing.T) {
		old := time.Now().Add(-72 * time.Hour)

		// cancelled: release at the cancel transition, with a generated report in the way.
		baseRepo, wt := fixtureRepo(t)
		m, audit := buildManager(t, baseRepo, config.CleanupModeOn, 0)
		prd := saveFixturePRD(t, m, branch, PRDCancelled, "", time.Now())
		writeResidueFile(t, wt, defaultGateReportPath, "gate output\n")
		m.ReleaseAutomatonWorktree(prd)
		if branchExists(t, baseRepo, branch) {
			t.Fatalf("a cancelled Automaton's branch must be released")
		}
		if _, err := os.Stat(wt); err == nil {
			t.Fatalf("a cancelled Automaton's work tree must be released")
		}
		if len(audit.events(worktreeCleanupAuditAction)) != 1 {
			t.Fatalf("want one removal audit event")
		}

		// archived: release at the archive transition.
		baseRepo, wt = fixtureRepo(t)
		m, _ = buildManager(t, baseRepo, config.CleanupModeOn, 0)
		prd = saveFixturePRD(t, m, branch, PRDArchived, "", time.Now())
		res := m.ReleaseResidueAtTerminal(prd, ResidueTriggerArchived)
		if len(res.Held) != 0 || branchExists(t, baseRepo, branch) {
			t.Fatalf("an archived Automaton's residue must be released, held=%v", heldReasons(res))
		}

		// periodic pass: a dirty-held entry (generated files only) is released, a hand-written one is held.
		baseRepo, wt = fixtureRepo(t)
		m, _ = buildManager(t, baseRepo, config.CleanupModeOn, 0)
		saveFixturePRD(t, m, branch, PRDCompleted, "", old)
		writeResidueFile(t, wt, defaultGateReportPath+".sha256", "abc\n")
		writeResidueFile(t, wt, "docs/plans/drafts/changelog.md", "draft\n")
		out := m.CleanupWorktreesPass()
		if len(out.Removed) == 0 || branchExists(t, baseRepo, branch) {
			t.Fatalf("the periodic pass must release a generated-only dirty entry, got %+v", out)
		}

		baseRepo, wt = fixtureRepo(t)
		m, _ = buildManager(t, baseRepo, config.CleanupModeOn, 0)
		saveFixturePRD(t, m, branch, PRDCompleted, "", old)
		writeResidueFile(t, wt, "keep-me.txt", "by hand\n")
		m.CleanupWorktreesPass()
		if !branchExists(t, baseRepo, branch) {
			t.Fatalf("the periodic pass must hold hand-written work")
		}
		if _, err := os.Stat(filepath.Join(wt, "keep-me.txt")); err != nil {
			t.Fatalf("the hand-written file must survive the periodic pass")
		}
	})

	t.Run("off mode does nothing", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		m, _ := buildManager(t, baseRepo, config.CleanupModeOff, 0)
		prd := saveFixturePRD(t, m, branch, PRDCancelled, "", time.Now())
		res := m.ReleaseResidueAtTerminal(prd, ResidueTriggerCancelled)
		if len(res.Removed)+len(res.Held) != 0 || !branchExists(t, baseRepo, branch) {
			t.Fatalf("off mode must not touch anything")
		}
		if _, err := os.Stat(wt); err != nil {
			t.Fatalf("off mode must keep the work tree")
		}
	})
}

func TestGeneratedArtifactsDoNotHold(t *testing.T) {
	const branch = "automaton/fixture"

	t.Run("classification", func(t *testing.T) {
		porcelain := strings.Join([]string{
			"?? .datawatch/reports/final-gate.txt",
			"?? .datawatch/reports/final-gate.txt.sha256",
			"?? docs/plans/parity-issues/thing-parity-draft.md",
			"?? docs/plans/drafts/",
			"?? docs/plans/drafts/changelog.md",
			"?? docs/plans/parity-issues/hand-written.md",
			"?? main.go",
			" M .datawatch/reports/final-gate.txt",
			"A  docs/plans/drafts/staged.md",
			"?? \"quoted name.txt\"",
		}, "\n")
		gen, rest := SplitGeneratedArtefacts(porcelain, nil)
		if len(gen) != 5 {
			t.Fatalf("want 5 generated artefacts, got %v", gen)
		}
		wantRest := []string{
			"?? docs/plans/parity-issues/hand-written.md", "?? main.go",
			" M .datawatch/reports/final-gate.txt", "A  docs/plans/drafts/staged.md", "?? \"quoted name.txt\"",
		}
		if strings.Join(rest, "|") != strings.Join(wantRest, "|") {
			t.Fatalf("remaining = %v", rest)
		}
		if g, _ := SplitGeneratedArtefacts("?? reports/custom.txt\n?? reports/custom.txt.sha256", []string{"reports/custom.txt"}); len(g) != 2 {
			t.Fatalf("a task's own report path must be recognised, got %v", g)
		}
		if isGeneratedArtefact("../.datawatch/reports/final-gate.txt", nil) {
			t.Fatalf("a path outside the work tree is never an artefact")
		}
	})

	t.Run("untracked generated files are deleted with the work tree", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		for _, rel := range []string{
			defaultGateReportPath, defaultGateReportPath + ".sha256",
			"docs/plans/parity-issues/x-parity-draft.md", "docs/plans/drafts/a/b.md",
		} {
			writeResidueFile(t, wt, rel, "generated\n")
		}
		res := ResidualReleasePass(residueOpts(baseRepo, branch, wt, &residueSink{}))
		if len(res.Held) != 0 {
			t.Fatalf("generated artefacts must not hold, held=%v", heldReasons(res))
		}
		if _, err := os.Stat(wt); err == nil {
			t.Fatalf("the work tree must be deleted with its generated files")
		}
		if branchExists(t, baseRepo, branch) {
			t.Fatalf("the branch must be released")
		}
		var arte int
		for _, e := range res.Removed {
			arte += len(e.Artefacts)
		}
		if arte != 4 {
			t.Fatalf("want 4 artefacts reported as deleted with the work tree, got %d", arte)
		}
	})

	t.Run("any other untracked file still holds, and no artefact is deleted", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		writeResidueFile(t, wt, defaultGateReportPath, "generated\n")
		writeResidueFile(t, wt, "docs/plans/parity-issues/hand-written.md", "mine\n")
		res := ResidualReleasePass(residueOpts(baseRepo, branch, wt, &residueSink{}))
		if !hasReason(res, ResidueKindWorktree, ResidueHeldHandWritten) || len(res.Removed) != 0 {
			t.Fatalf("a non-generated untracked file must hold, got removed=%v held=%v", res.Removed, heldReasons(res))
		}
		if _, err := os.Stat(filepath.Join(wt, defaultGateReportPath)); err != nil {
			t.Fatalf("a held work tree keeps all its files: %v", err)
		}
	})

	t.Run("a modified tracked file holds even inside a generated folder", func(t *testing.T) {
		baseRepo, wt := fixtureRepo(t)
		writeResidueFile(t, wt, "docs/plans/drafts/tracked.md", "v1\n")
		gitCmd(t, wt, "add", "-f", "docs/plans/drafts/tracked.md")
		gitCmd(t, wt, "commit", "-q", "-m", "track a draft")
		gitCmd(t, baseRepo, "merge", "-q", "--ff-only", branch)
		writeResidueFile(t, wt, "docs/plans/drafts/tracked.md", "v2 edited by hand\n")
		res := ResidualReleasePass(residueOpts(baseRepo, branch, wt, &residueSink{}))
		if !hasReason(res, ResidueKindWorktree, ResidueHeldHandWritten) {
			t.Fatalf("a modified tracked file must hold, got held=%v removed=%v", heldReasons(res), res.Removed)
		}
	})

	t.Run("RemoveGeneratedArtefacts never leaves the work tree", func(t *testing.T) {
		root := t.TempDir()
		wt := filepath.Join(root, "wt")
		outside := filepath.Join(root, "outside.txt")
		writeResidueFile(t, root, "outside.txt", "x\n")
		writeResidueFile(t, wt, "inside.txt", "y\n")
		got := RemoveGeneratedArtefacts(wt, []string{"../outside.txt", "inside.txt"})
		if len(got) != 1 || got[0] != "inside.txt" {
			t.Fatalf("removed = %v", got)
		}
		if _, err := os.Stat(outside); err != nil {
			t.Fatalf("a path outside the work tree must survive")
		}
	})
}
