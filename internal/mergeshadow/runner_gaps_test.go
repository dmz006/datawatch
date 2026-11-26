package mergeshadow

// The gaps of the finish-time plan: release size and planned version, the
// changelog source, and the findings that never reached a stored plan.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// gapRepo has a version file at 1.2.0 on main and a branch "auto" that adds a
// changelog draft and a file at the repository root.
func gapRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-c", "user.email=t@example.invalid", "-c", "user.name=Test", "-c", "commit.gpgsign=false"}, args...)
		if out, err := runGit(context.Background(), dir, full...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	put := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	put("pkg/version.go", "package x\n\nvar Version = \"1.2.0\"\n")
	git("add", "-A")
	git("commit", "-q", "-m", "base")
	git("checkout", "-q", "-b", "auto")
	put("docs/plans/drafts/a1-changelog.md", "## Draft heading\n\n- from the draft\n")
	put("stray.out", "left over\n")
	git("add", "-A")
	git("commit", "-q", "-m", "work")
	git("checkout", "-q", "main")
	return dir
}

func gapInput(t *testing.T, repo string) Input {
	return Input{
		AutomatonID: "a1", Project: "p", RepoPath: repo, Branch: "auto", DataDir: t.TempDir(),
		Pipeline:       config.MergePipelineConfig{VersionFiles: []string{"pkg/version.go"}},
		ChangelogTasks: []ChangelogTask{{ID: "t9", Kind: ChangelogTaskKind, Output: "## [9.9.9] - 2026-01-01\n\n- from the task\n"}},
	}
}

func gapPlan(t *testing.T, in Input, gates ...GateResult) *MergePlan {
	t.Helper()
	r := Runner{Gates: func(context.Context, string, time.Duration) []GateResult { return gates }}
	if _, err := r.Run(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	p, err := Load(in.DataDir, in.AutomatonID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func hasFinding(p *MergePlan, code string) bool {
	for _, f := range p.Findings {
		if f.Code == code && f.Blocking {
			return true
		}
	}
	return false
}

func TestPlanTakesTheSizeFromTheAutomatonAndTheVersionFromTheBase(t *testing.T) {
	repo := gapRepo(t)
	for size, want := range map[ReleaseSize]string{ReleaseSizePatch: "1.2.1", ReleaseSizeMinor: "1.3.0", ReleaseSizeMajor: "2.0.0", ReleaseSizeNone: ""} {
		in := gapInput(t, repo)
		in.ReleaseSize = size
		p := gapPlan(t, in)
		if p.ReleaseSize != size || p.PlannedVersion != want {
			t.Errorf("%s: size %q version %q, want %q", size, p.ReleaseSize, p.PlannedVersion, want)
		}
	}
	in := gapInput(t, repo)
	in.ReleaseSize = ReleaseSizePatch
	in.Pipeline = config.MergePipelineConfig{VersionFiles: []string{"pkg/none.go"}}
	if p := gapPlan(t, in); p.PlannedVersion != "" || !hasFinding(p, BaseVersionUnreadCode) {
		t.Errorf("an unreadable base version: %q %+v", p.PlannedVersion, p.Findings)
	}
}

func TestPlanChangelogComesFromTheDraftElseTheTask(t *testing.T) {
	repo := gapRepo(t)
	in := gapInput(t, repo)
	in.ChangelogDraftPath = "docs/plans/drafts/a1-changelog.md"
	p := gapPlan(t, in)
	if p.ChangelogSource != in.ChangelogDraftPath || p.ChangelogHeading != "## Draft heading" || p.ChangelogBody != "- from the draft" {
		t.Errorf("source %q heading %q body %q", p.ChangelogSource, p.ChangelogHeading, p.ChangelogBody)
	}
	in = gapInput(t, repo)
	in.ChangelogDraftPath = "docs/plans/drafts/absent-changelog.md"
	if p := gapPlan(t, in); p.ChangelogSource != "t9" || p.ChangelogBody != "- from the task" {
		t.Errorf("fallback: source %q body %q", p.ChangelogSource, p.ChangelogBody)
	}
}

// Each named blocking code is in a stored plan.
func TestStoredPlanCarriesTheNamedFindings(t *testing.T) {
	repo := gapRepo(t)
	in := gapInput(t, repo)
	in.CheckRootFiles = true
	gate := GateResult{Name: "lint", Status: GateFail, Findings: []Finding{
		{Code: LintVersionMismatchCode, Message: "v", Blocking: true},
		{Code: LintToolUnavailableCode, Message: "u", Blocking: true},
	}}
	p := gapPlan(t, in, gate)
	for _, code := range []string{LintVersionMismatchCode, LintToolUnavailableCode, StrayRootFileCode} {
		if !hasFinding(p, code) {
			t.Errorf("the stored plan lacks %s: %+v", code, p.Findings)
		}
	}
	in = gapInput(t, repo)
	in.Review = &ReviewVerdict{Commit: "refs/heads/main"}
	if p := gapPlan(t, in); !hasFinding(p, ReviewStaleCode) {
		t.Errorf("the stored plan lacks %s: %+v", ReviewStaleCode, p.Findings)
	}
}
