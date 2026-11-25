package autonomous

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// dsmLauncherFixture is like dsmFixture but the base commit also carries a
// tracked worker-mcp.json that belongs to the operator (the "launcher-named
// file that is part of the target" case).
func dsmLauncherFixture(t *testing.T) *dsm {
	t.Helper()
	repo := newTestRepo(t)
	wtDir := t.TempDir()
	prd := &PRD{ID: "abcd1234", Title: "DSM"}
	p0, b0, err := EnsureWorktree(prd, repo, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	prd.ProjectDir, prd.Git.Branch = p0, b0
	const operatorMCP = `{"mcpServers":{"mine":{"command":"mine"}}}` + "\n"
	dsmWrite(t, p0, DefaultRulesFile, dsmBaseRules)
	dsmWrite(t, p0, launcherMCPConfigFileName, operatorMCP)
	gitRun(t, p0, "add", DefaultRulesFile, launcherMCPConfigFileName)
	gitRun(t, p0, "commit", "-q", "-m", "base rules + operator mcp config")
	m := newTaskWorktreeManager(t, repo, wtDir, true)
	m.SetDaemonRulesTextFn(standInDaemonText(dsmStandIns))
	task := Task{ID: "t1", PRDID: prd.ID, Title: "launcher cleanup", Status: TaskCompleted}
	p1, b1, err := EnsureTaskWorktree(prd, &task, repo, wtDir)
	if err != nil {
		t.Fatal(err)
	}
	task.WorktreePath, task.WorktreeBranch, task.BaseSHA = p1, b1, gitOut(t, p1, "rev-parse", "HEAD")
	if len(prd.Story) == 0 {
		prd.Story = []Story{{ID: "s1"}}
	}
	prd.Story[0].Tasks = append(prd.Story[0].Tasks, task)
	if err := m.store.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return &dsm{t: t, m: m, prd: prd, task: &prd.Story[0].Tasks[0], dir: p0, path: p1, branch: b1, base: task.BaseSHA}
}

// headMissing reports whether the path is absent from the branch tip.
func (f *dsm) headMissing(name string) bool {
	f.t.Helper()
	_, err := exec.Command("git", "-C", f.path, "show", "HEAD:"+name).Output()
	return err != nil
}

// prdMissing reports whether the path is absent from the PRD branch head.
func (f *dsm) prdMissing(name string) bool {
	f.t.Helper()
	_, err := exec.Command("git", "-C", f.dir, "show", "HEAD:"+name).Output()
	return err != nil
}

// TestDaemonSectionsMergeLauncherFilesDroppedWhenAbsentFromTarget: a branch
// that committed the two root launcher files (and a same-named file in a
// subfolder) plus a daemon-sections-only AGENT.md commit comes back with the
// launcher files removed, the subfolder file kept, and AGENT.md cleaned to
// the base blob — in one cleanup commit; nothing launcher-like survives the
// merge.
func TestDaemonSectionsMergeLauncherFilesDroppedWhenAbsentFromTarget(t *testing.T) {
	f := dsmFixture(t, dsmBaseRules)
	const launcherMCP = `{"mcpServers":{"daemon":{"command":"daemon"}}}` + "\n"
	const launcherCtx = "# Worker starting context\n\nWork from the task spec only.\n"
	f.commit(launcherMCPConfigFileName, launcherMCP)
	f.commit(launcherStartingContextFileName, launcherCtx)
	if err := os.MkdirAll(filepath.Join(f.path, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	dsmWrite(f.t, filepath.Join(f.path, "sub"), launcherMCPConfigFileName, "subfolder, not the launcher's\n")
	gitRun(f.t, f.path, "add", "sub/"+launcherMCPConfigFileName)
	gitRun(f.t, f.path, "commit", "-q", "-m", "work: sub/worker-mcp.json")
	f.commit(DefaultRulesFile, dsmBaseRules+dsmDaemon)

	before := f.headCount()
	if err := f.m.cleanDaemonSectionsBeforeMerge(context.Background(), f.path, f.base); err != nil {
		t.Fatal(err)
	}
	if got := f.headCount(); got != before+1 {
		t.Fatalf("cleanup commits = %d, want exactly 1", got-before)
	}
	if !f.headMissing(launcherMCPConfigFileName) {
		t.Fatalf("committed %s still present at the branch tip", launcherMCPConfigFileName)
	}
	if !f.headMissing(launcherStartingContextFileName) {
		t.Fatalf("committed %s still present at the branch tip", launcherStartingContextFileName)
	}
	if got := f.branchFile("sub/" + launcherMCPConfigFileName); got != "subfolder, not the launcher's\n" {
		t.Fatalf("subfolder %s was touched: %q", launcherMCPConfigFileName, got)
	}
	if got := f.branchFile(DefaultRulesFile); got != dsmBaseRules {
		t.Fatalf("branch rules file after cleanup = %q, want the base blob %q", got, dsmBaseRules)
	}

	f.merge(MergeOutcomeMerged)
	if !f.prdMissing(launcherMCPConfigFileName) {
		t.Fatalf("launcher %s leaked into the PRD branch", launcherMCPConfigFileName)
	}
	if !f.prdMissing(launcherStartingContextFileName) {
		t.Fatalf("launcher %s leaked into the PRD branch", launcherStartingContextFileName)
	}
	if got := f.prdFile("sub/" + launcherMCPConfigFileName); got != "subfolder, not the launcher's\n" {
		t.Fatalf("subfolder %s not merged: %q", launcherMCPConfigFileName, got)
	}
	if got := f.prdFile(DefaultRulesFile); got != dsmBaseRules {
		t.Fatalf("PRD rules file after merge = %q, want unchanged %q", got, dsmBaseRules)
	}
}

// TestDaemonSectionsMergeLauncherNamedFileAtTargetKept: the base tracks a
// worker-mcp.json as an operator file — the branch may even have rewritten
// it — and cleaning must not remove it (fail-closed). The other launcher
// file and the daemon sections still drop.
func TestDaemonSectionsMergeLauncherNamedFileAtTargetKept(t *testing.T) {
	f := dsmLauncherFixture(t)
	f.commit(launcherMCPConfigFileName, `{"mcpServers":{"rewritten":{"command":"rewritten"}}}`+"\n")
	f.commit(launcherStartingContextFileName, "# Worker starting context\n\nWork from the task spec only.\n")
	f.commit(DefaultRulesFile, dsmBaseRules+dsmDaemon)

	if err := f.m.cleanDaemonSectionsBeforeMerge(context.Background(), f.path, f.base); err != nil {
		t.Fatal(err)
	}
	if !f.headMissing(launcherStartingContextFileName) {
		t.Fatalf("committed %s still present at the branch tip", launcherStartingContextFileName)
	}
	if f.headMissing(launcherMCPConfigFileName) {
		t.Fatalf("base-tracked %s was removed; a launcher-named file that is part of the target must survive", launcherMCPConfigFileName)
	}
	if got := f.branchFile(launcherMCPConfigFileName); got != `{"mcpServers":{"rewritten":{"command":"rewritten"}}}`+"\n" {
		t.Fatalf("base-tracked %s was rewritten: %q", launcherMCPConfigFileName, got)
	}
	if got := f.branchFile(DefaultRulesFile); got != dsmBaseRules {
		t.Fatalf("branch rules file after cleanup = %q, want the base blob %q", got, dsmBaseRules)
	}

	f.merge(MergeOutcomeMerged)
	if !f.prdMissing(launcherStartingContextFileName) {
		t.Fatalf("launcher %s leaked into the PRD branch", launcherStartingContextFileName)
	}
	if f.prdMissing(launcherMCPConfigFileName) {
		t.Fatalf("base-tracked %s missing after the merge", launcherMCPConfigFileName)
	}
	if got := f.prdFile(DefaultRulesFile); got != dsmBaseRules {
		t.Fatalf("PRD rules file after merge = %q, want unchanged %q", got, dsmBaseRules)
	}
}

// TestDaemonSectionsMergeLauncherFilesDroppedWithRealEditKept: a branch
// whose AGENT.md carries a real edit keeps the whole file (edit kept, daemon
// sections dropped) while its committed launcher files are still removed —
// in one cleanup commit.
func TestDaemonSectionsMergeLauncherFilesDroppedWithRealEditKept(t *testing.T) {
	f := dsmFixture(t, dsmBaseRules)
	f.commit(launcherMCPConfigFileName, `{"mcpServers":{"daemon":{"command":"daemon"}}}`+"\n")
	f.commit(launcherStartingContextFileName, "context\n")
	f.commit(DefaultRulesFile, dsmBaseRules+"And run go vet ./...\n"+dsmDaemon)

	if err := f.m.cleanDaemonSectionsBeforeMerge(context.Background(), f.path, f.base); err != nil {
		t.Fatal(err)
	}
	if !f.headMissing(launcherMCPConfigFileName) || !f.headMissing(launcherStartingContextFileName) {
		t.Fatalf("launcher files still present at the branch tip")
	}
	want := dsmBaseRules + "And run go vet ./...\n"
	if got := f.branchFile(DefaultRulesFile); got != want {
		t.Fatalf("branch rules file after cleanup = %q, want the edit kept and the sections dropped %q", got, want)
	}

	f.merge(MergeOutcomeMerged)
	if !f.prdMissing(launcherMCPConfigFileName) || !f.prdMissing(launcherStartingContextFileName) {
		t.Fatalf("launcher files leaked into the PRD branch")
	}
	if got := f.prdFile(DefaultRulesFile); got != want {
		t.Fatalf("PRD rules file after merge = %q, want %q", got, want)
	}
}
