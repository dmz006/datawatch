package mergeshadow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
)

// runnerRepo builds a repository whose main has a version file, a changelog,
// a README and a text file. Branch "work" adds feat.go and nothing else.
func runnerRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "version.go", "package main\n\nvar Version = \"1.2.0\"\n")
	write(t, repo, "CHANGELOG.md", "# Changelog\n\n## [1.2.0] - 2026-11-01\n\n- old\n")
	write(t, repo, "README.md", "# Demo\n")
	write(t, repo, "notes.txt", "base\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "base")
	git(t, repo, "checkout", "-q", "-b", "work")
	write(t, repo, "feat.go", "package main\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "feature")
	git(t, repo, "checkout", "-q", "main")
	return repo
}

// runnerConflictRepo is runnerRepo with main and work both editing the
// changelog (a mechanical file) and notes.txt (a document), so the trial
// merge conflicts on both.
func runnerConflictRepo(t *testing.T) string {
	t.Helper()
	repo := runnerRepo(t)
	git(t, repo, "checkout", "-q", "work")
	write(t, repo, "CHANGELOG.md", "# Changelog\n\n## [1.3.0] - 2026-11-09\n\n- work\n")
	write(t, repo, "notes.txt", "work\n")
	git(t, repo, "commit", "-q", "-am", "work edits")
	git(t, repo, "checkout", "-q", "main")
	write(t, repo, "CHANGELOG.md", "# Changelog\n\n## [1.2.1] - 2026-11-05\n\n- main\n")
	write(t, repo, "notes.txt", "main\n")
	git(t, repo, "commit", "-q", "-am", "main edits")
	return repo
}

// fakeGates returns a GateFunc that records each call and reports the given
// status for every gate. It asserts that the trial worktree holds the merge.
func fakeGates(t *testing.T, status GateStatus, calls *int) GateFunc {
	return func(_ context.Context, dir string, timeout time.Duration) []GateResult {
		*calls++
		if _, err := os.Stat(filepath.Join(dir, "feat.go")); err != nil {
			t.Errorf("gate ran in %s without the merged feat.go: %v", dir, err)
		}
		if timeout <= 0 {
			t.Errorf("gate got a non-positive timeout %v", timeout)
		}
		out := make([]GateResult, 0, len(gateNames))
		for _, name := range gateNames {
			out = append(out, GateResult{Name: name, Status: status, Duration: time.Second, Detail: "fake"})
		}
		return out
	}
}

func runnerInput(t *testing.T, repo, branch string) Input {
	t.Helper()
	return Input{
		AutomatonID: "a1",
		Project:     "demo",
		RepoPath:    repo,
		Branch:      branch,
		BaseRef:     "main",
		DataDir:     t.TempDir(),
		Pipeline:    config.MergePipelineConfig{VersionFiles: []string{"version.go"}},
		ChangelogTasks: []ChangelogTask{{
			ID:     "t9",
			Kind:   ChangelogTaskKind,
			Output: "## [v1.3.0] - 2026-11-09\n\n### Added\n- thing\n",
		}},
		GateTimeout: time.Minute,
	}
}

func fixedClock() func() time.Time {
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func TestRunnerCleanMergeStoresPlanWithoutApplying(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "work")
	in.ChangeRecord = &changerecord.ChangeRecord{AutomatonID: "a1", Packages: []string{"main"}}
	mainBefore := strings.TrimSpace(git(t, repo, "rev-parse", "main"))
	calls := 0

	plan, err := Runner{Gates: fakeGates(t, GatePass, &calls), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 {
		t.Errorf("gate runner called %d times, want 1", calls)
	}
	if plan.Applied {
		t.Error("plan marked applied")
	}
	if plan.AutomatonID != "a1" || plan.Project != "demo" || plan.Branch != "work" || plan.BaseRef != "main" {
		t.Errorf("identity fields: %+v", plan)
	}
	if plan.ReleaseSize != ReleaseSizeMinor {
		t.Errorf("release size %q, want minor (1.2.0 to v1.3.0)", plan.ReleaseSize)
	}
	if plan.PlannedVersion != "v1.3.0" || plan.ChangelogSource != "t9" {
		t.Errorf("planned %q source %q", plan.PlannedVersion, plan.ChangelogSource)
	}
	if plan.ChangelogHeading != "## [v1.3.0] - 2026-11-09" || !strings.Contains(plan.ChangelogBody, "- thing") {
		t.Errorf("changelog heading %q body %q", plan.ChangelogHeading, plan.ChangelogBody)
	}
	if !strings.Contains(plan.ReadmeLine, "v1.3.0") || !strings.Contains(plan.ReadmeLine, "2026-11-09") {
		t.Errorf("readme line %q", plan.ReadmeLine)
	}
	if plan.Conflicts == nil || len(plan.Conflicts) != 0 {
		t.Errorf("conflicts %#v, want empty non-nil", plan.Conflicts)
	}
	if len(plan.Gates) != len(gateNames) {
		t.Fatalf("gates %d, want %d", len(plan.Gates), len(gateNames))
	}
	for _, g := range plan.Gates {
		if g.Status != string(GatePass) {
			t.Errorf("gate %s status %q", g.Name, g.Status)
		}
	}
	if len(plan.Findings) != 0 {
		t.Errorf("findings %+v, want none", plan.Findings)
	}

	stored, err := Load(in.DataDir, "a1")
	if err != nil {
		t.Fatalf("stored plan: %v", err)
	}
	if stored.Applied || stored.PlannedVersion != "v1.3.0" {
		t.Errorf("stored plan %+v", stored)
	}

	if got := strings.TrimSpace(git(t, repo, "rev-parse", "main")); got != mainBefore {
		t.Errorf("main moved from %s to %s", mainBefore, got)
	}
	if strings.Contains(git(t, repo, "worktree", "list"), "merge-shadow") {
		t.Error("trial worktree left registered")
	}
	if _, err := os.Stat(filepath.Join(repo, "feat.go")); !os.IsNotExist(err) {
		t.Errorf("merge leaked into the project checkout: %v", err)
	}
}

func TestRunnerConflictsClassifiedAndGatesSkipped(t *testing.T) {
	repo := runnerConflictRepo(t)
	in := runnerInput(t, repo, "work")
	in.ChangelogTasks = nil // no entry: the changelog must stay empty, not invented
	calls := 0

	plan, err := Runner{Gates: fakeGates(t, GatePass, &calls), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 0 {
		t.Errorf("gates ran %d times on a conflicted merge, want 0", calls)
	}
	byPath := map[string]Conflict{}
	for _, c := range plan.Conflicts {
		byPath[c.Path] = c
	}
	if c := byPath["CHANGELOG.md"]; c.FileClass != string(FileClassMechanical) || c.ResolvedBy != "mechanical" {
		t.Errorf("CHANGELOG.md conflict %+v, want mechanical and resolved", c)
	}
	if c := byPath["notes.txt"]; c.FileClass != string(FileClassDocument) || c.ResolvedBy != "" || c.Resolution != "unresolved" {
		t.Errorf("notes.txt conflict %+v, want document and unresolved", c)
	}
	if len(plan.Gates) != len(gateNames) {
		t.Fatalf("gates %d, want %d", len(plan.Gates), len(gateNames))
	}
	for _, g := range plan.Gates {
		if g.Status != string(GateSkipped) {
			t.Errorf("gate %s status %q, want skipped", g.Name, g.Status)
		}
	}
	if plan.ChangelogSource != ChangelogSourceMissing || plan.ChangelogHeading != "" || plan.ChangelogBody != "" {
		t.Errorf("changelog %q %q %q, want missing and empty", plan.ChangelogSource, plan.ChangelogHeading, plan.ChangelogBody)
	}
	if plan.PlannedVersion != "" || plan.ReleaseSize != ReleaseSizeNone || plan.ReadmeLine != "" {
		t.Errorf("planned %q size %q readme %q, want none", plan.PlannedVersion, plan.ReleaseSize, plan.ReadmeLine)
	}
	codes := map[string]int{}
	for _, f := range plan.Findings {
		if !f.Blocking {
			t.Errorf("finding %s not blocking", f.Code)
		}
		codes[f.Code]++
	}
	if codes["conflict_unresolved"] != 1 || codes["changelog_missing"] != 1 || len(plan.Findings) != 2 {
		t.Errorf("findings %v, want one conflict_unresolved and one changelog_missing", codes)
	}
}

func TestRunnerFailedGatesAreBlockingFindings(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "work")
	gates := func(_ context.Context, _ string, _ time.Duration) []GateResult {
		return []GateResult{
			{Name: "build", Status: GatePass},
			{Name: "lint", Status: GateFail, Detail: "exit 2"},
			{Name: "test", Status: GateTimeout, Detail: "killed after 1m0s"},
		}
	}
	plan, err := Runner{Gates: gates, Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	codes := map[string]bool{}
	for _, f := range plan.Findings {
		if !f.Blocking {
			t.Errorf("finding %s not blocking", f.Code)
		}
		codes[f.Code] = true
	}
	if !codes["gate_fail"] || !codes["gate_timeout"] || len(plan.Findings) != 2 {
		t.Errorf("findings %+v, want gate_fail and gate_timeout only", plan.Findings)
	}
}

func TestRunnerSkippedGateIsNotAFinding(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "work")
	gates := func(_ context.Context, _ string, _ time.Duration) []GateResult {
		return []GateResult{{Name: "build", Status: GateSkipped, Detail: "no target"}}
	}
	plan, err := Runner{Gates: gates, Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(plan.Gates) != 1 || plan.Gates[0].Status != string(GateSkipped) {
		t.Errorf("gates %+v", plan.Gates)
	}
	if len(plan.Findings) != 0 {
		t.Errorf("a skipped gate raised findings: %+v", plan.Findings)
	}
}

func TestRunnerTrialFailureStillStoresBlockingPlan(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "no-such-branch")
	calls := 0

	plan, err := Runner{Gates: fakeGates(t, GatePass, &calls), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 0 {
		t.Errorf("gates ran after a failed trial merge")
	}
	if len(plan.Findings) == 0 || plan.Findings[0].Code != "trial_merge_failed" || !plan.Findings[0].Blocking {
		t.Errorf("findings %+v, want a blocking trial_merge_failed first", plan.Findings)
	}
	if len(plan.Gates) != len(gateNames) || plan.Gates[0].Status != string(GateSkipped) {
		t.Errorf("gates %+v, want all skipped", plan.Gates)
	}
	if _, err := Load(in.DataDir, "a1"); err != nil {
		t.Errorf("plan not stored: %v", err)
	}
}

func TestRunnerRejectsMissingInput(t *testing.T) {
	repo := runnerRepo(t)
	base := runnerInput(t, repo, "work")
	cases := map[string]func(*Input){
		"automaton id": func(in *Input) { in.AutomatonID = "" },
		"repo path":    func(in *Input) { in.RepoPath = "" },
		"branch":       func(in *Input) { in.Branch = "" },
		"data dir":     func(in *Input) { in.DataDir = "" },
	}
	for name, mut := range cases {
		in := base
		mut(&in)
		if _, err := (Runner{Gates: fakeGates(t, GatePass, new(int))}).Run(context.Background(), in); err == nil {
			t.Errorf("missing %s: no error", name)
		}
	}
}

func TestRunnerCancelledContextIsAnError(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "work")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := Runner{Gates: fakeGates(t, GatePass, &calls)}.Run(ctx, in)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v, want context.Canceled", err)
	}
	if _, lerr := Load(in.DataDir, "a1"); lerr == nil {
		t.Error("a cancelled run stored a plan")
	}
}

func TestTrialMergeWithRunsInsideWhileWorktreeExists(t *testing.T) {
	repo := runnerConflictRepo(t)
	scratch := filepath.Join(t.TempDir(), "scratch")
	var seen []string
	var sawWT string
	conflicts, err := TrialMergeWith(context.Background(), repo, "main", "work", scratch,
		func(wt string, paths []string) error {
			sawWT = wt
			seen = paths
			if _, serr := os.Stat(filepath.Join(wt, "CHANGELOG.md")); serr != nil {
				t.Errorf("worktree missing inside callback: %v", serr)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("TrialMergeWith: %v", err)
	}
	if len(conflicts) != 2 || len(seen) != 2 {
		t.Errorf("conflicts %v, callback saw %v", conflicts, seen)
	}
	if _, serr := os.Stat(sawWT); !os.IsNotExist(serr) {
		t.Errorf("worktree %s still present after return: %v", sawWT, serr)
	}
}

func TestTrialMergeWithPropagatesCallbackError(t *testing.T) {
	repo := runnerRepo(t)
	scratch := filepath.Join(t.TempDir(), "scratch")
	boom := errors.New("callback failed")
	_, err := TrialMergeWith(context.Background(), repo, "main", "work", scratch,
		func(string, []string) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err %v, want the callback error", err)
	}
}

func TestReleaseSizeBetween(t *testing.T) {
	cases := []struct {
		from, to string
		want     ReleaseSize
	}{
		{"1.2.0", "1.3.0", ReleaseSizeMinor},
		{"1.2.0", "v1.3.0", ReleaseSizeMinor},
		{"v1.2.0", "v2.0.0", ReleaseSizeMajor},
		{"1.2.0", "1.2.1", ReleaseSizePatch},
		{"1.2.0", "1.2.0", ReleaseSizeNone},
		{"1.2.1", "1.2.0", ReleaseSizeNone},
		{"", "1.2.0", ReleaseSizeNone},
		{"1.2.0", "", ReleaseSizeNone},
		{"1.2", "1.3.0", ReleaseSizeNone},
		{"1.x.0", "1.3.0", ReleaseSizeNone},
	}
	for _, c := range cases {
		if got := releaseSizeBetween(c.from, c.to); got != c.want {
			t.Errorf("releaseSizeBetween(%q, %q) = %q, want %q", c.from, c.to, got, c.want)
		}
	}
}

func TestVersionFromHeading(t *testing.T) {
	if got := versionFromHeading("## [v1.3.0] - 2026-11-09"); got != "v1.3.0" {
		t.Errorf("got %q", got)
	}
	for _, bad := range []string{"", "# Changelog", "## 1.3.0", "## [1.3.0]"} {
		if got := versionFromHeading(bad); got != "" {
			t.Errorf("versionFromHeading(%q) = %q, want empty", bad, got)
		}
	}
}

func TestRunnerDefaultsBaseRefAndGateTimeout(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "work")
	in.BaseRef = ""
	in.GateTimeout = 0
	calls := 0
	plan, err := Runner{Gates: fakeGates(t, GatePass, &calls), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if plan.BaseRef != "main" {
		t.Errorf("base ref %q, want main", plan.BaseRef)
	}
	if calls != 1 {
		t.Errorf("gates called %d times", calls)
	}
}
