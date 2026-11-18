package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

const uwIgnoredReport = ".datawatch/reports/evidence.txt"

// uwIgnoredReportRepo makes a repository whose .gitignore (committed) ignores
// .datawatch/ and that holds an untracked report under it.
func uwIgnoredReportRepo(t *testing.T) string {
	t.Helper()
	dir := uwRepo(t)
	uwWrite(t, dir, ".gitignore", ".datawatch/\n")
	uwGit(t, dir, "add", ".gitignore")
	uwGit(t, dir, "commit", "-q", "-m", "ignore")
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	uwWrite(t, dir, uwIgnoredReport, "evidence\n")
	return dir
}

// uwStatusPlusIgnored runs real git, but adds the ignored report to the
// status output as untracked, so only the ignore filter can drop it.
func uwStatusPlusIgnored(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := defaultDriftGitRunner(ctx, dir, args...)
	if err != nil {
		return out, err
	}
	if len(args) > 0 && args[0] == "status" {
		out += "?? " + uwIgnoredReport + "\x00"
	}
	return out, nil
}

func TestUncommittedIgnoredReportIsNotWork(t *testing.T) {
	dir := uwIgnoredReportRepo(t)
	files, err := DetectUncommittedWork(context.Background(), dir, uwStatusPlusIgnored)
	if err != nil {
		t.Fatalf("DetectUncommittedWork: %v", err)
	}
	for _, f := range files {
		if f.Path == uwIgnoredReport || strings.HasPrefix(f.Path, ".datawatch/") {
			t.Fatalf("ignored report listed as work: %+v", files)
		}
	}
}

func TestUncommittedIgnoredReportOpensNoQuestion(t *testing.T) {
	dir := uwIgnoredReportRepo(t)
	r := newUncommittedHoldRig(t, true)
	r.prd.ProjectDir = dir
	r.prd.Git = GitWorkflow{Branch: "automaton/" + r.prd.ID}
	if err := r.m.Store().SavePRD(r.prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	r.m.driftGit = uwStatusPlusIgnored
	r.m.SetTaskCommit(func(_ context.Context, _ string, files []string, _ string) (string, error) {
		t.Fatalf("task commit called with %v; the ignored report is not work", files)
		return "", nil
	})
	held, err := r.m.checkUncommittedWork(context.Background(), r.prd, r.task)
	if err != nil {
		t.Fatalf("checkUncommittedWork: %v", err)
	}
	if held {
		t.Fatal("held = true; want false")
	}
	if r.task.UncommittedWork != nil {
		t.Errorf("task hold recorded: %+v", r.task.UncommittedWork)
	}
	if tk, _ := r.m.Store().GetTask(r.task.ID); tk != nil && (tk.UncommittedWork != nil || tk.Status == TaskBlocked) {
		t.Errorf("stored task held: status=%s hold=%+v", tk.Status, tk.UncommittedWork)
	}
	if qs := r.store.List(QuestionFilter{}); len(qs) != 0 {
		t.Errorf("questions = %d; want 0", len(qs))
	}
	if d := r.decisions("uncommitted_work_held"); len(d) != 0 {
		t.Errorf("hold decisions = %+v", d)
	}
}

func TestUncommittedInjectedOnlyAgentMdNotListed(t *testing.T) {
	dir := uwRepo(t)
	injected := platformcontext.UpsertSection("", platformcontext.PlatformContext, platformcontext.PlatformContextVersion)
	uwWrite(t, dir, "AGENT.md", injected)
	uwWrite(t, dir, "base.txt", "changed\n")
	files, err := DetectUncommittedWork(context.Background(), dir, nil)
	if err != nil {
		t.Fatalf("DetectUncommittedWork: %v", err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if strings.Join(paths, ",") != "base.txt" {
		t.Fatalf("listed = %v; want only base.txt", paths)
	}
}
