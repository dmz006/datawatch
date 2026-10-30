package autonomous

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestScope_EffectiveDirsAndPathInDirs(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj"}
	if got := EffectiveWriteDirs(prd); len(got) != 1 || got[0] != "/w/proj" {
		t.Fatalf("default write dirs = %v", got)
	}
	prd.ReadDirs = []string{"/w/ref"}
	prd.WriteDirs = []string{"/w/out"}
	if got := EffectiveReadDirs(prd); len(got) != 2 {
		t.Fatalf("read dirs = %v", got)
	}
	if !PathInDirs("/w/out/a/b.md", EffectiveWriteDirs(prd)) || PathInDirs("/w/outside/x", EffectiveWriteDirs(prd)) {
		t.Fatal("PathInDirs prefix handling wrong")
	}
}

func TestScope_LintFlagsOutOfScopePaths(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj", ReadDirs: []string{"/w/ref"}}
	if p := LintTaskScope(prd, "t", "Work in the repo at /home/user/workspace/datawatch and edit it", nil); len(p) != 1 {
		t.Fatalf("want 1 problem, got %v", p)
	}
	if p := LintTaskScope(prd, "t", "read /w/ref/notes.md then write docs/x.md", []string{"docs/x.md"}); len(p) != 0 {
		t.Fatalf("in-scope spec flagged: %v", p)
	}
	if p := LintTaskScope(prd, "t", "x", []string{"../other/f.md", "/etc/passwd"}); len(p) != 2 {
		t.Fatalf("want 2 file problems, got %v", p)
	}
	if p := LintTaskScope(prd, "t", "x", []string{"/w/ref/f.md"}); len(p) != 1 {
		t.Fatalf("read-only dir must not be writable: %v", p)
	}
}

func TestScope_LintIgnoresProseMentionsOfSystemBinaries(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj"}
	for _, spec := range []string{
		"If GNU time is missing, fall back to /usr/bin/time -v to measure peak RSS",
		"run the check with /bin/sh -c 'make test'",
		"call /usr/local/bin/foo on the output",
		"on macOS use /opt/homebrew/bin/gtime instead",
	} {
		if p := LintTaskScope(prd, "t", spec, nil); len(p) != 0 {
			t.Fatalf("binary mention flagged for %q: %v", spec, p)
		}
	}
}

func TestScope_LintStillFlagsNonBinaryAndWriteVerbPaths(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj"}
	for spec, path := range map[string]string{
		"Read /etc/passwd and copy it into docs/x.md":   "/etc/passwd",
		"write the report to /usr/bin/report.txt":       "/usr/bin/report.txt",
		"run /usr/bin/x/y against the build":            "/usr/bin/x/y",
		"link against /usr/lib/libfoo.so for the probe": "/usr/lib/libfoo.so",
	} {
		p := LintTaskScope(prd, "t", spec, nil)
		if len(p) != 1 || !strings.Contains(p[0], path) {
			t.Fatalf("want 1 problem mentioning %s for %q, got %v", path, spec, p)
		}
	}
}

func TestScope_LintStillFlagsPlannedFileUnderBinDir(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj"}
	p := LintTaskScope(prd, "t", "x", []string{"/usr/bin/time"})
	if len(p) != 1 || !strings.Contains(p[0], "outside the writable directories") {
		t.Fatalf("planned file under /usr/bin must be flagged: %v", p)
	}
}

func TestScope_LintIgnoresGrepPatternProse(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj"}
	spec := "Grep your final output for 'real hostnames', then for the home-directory path /home/<user>/workspace, then for 'API keys'."
	if p := LintTaskScope(prd, "t", spec, nil); len(p) != 0 {
		t.Fatalf("search-pattern prose flagged: %v", p)
	}
}

func TestScope_LintIgnoresPlaceholderAndGlobPaths(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj"}
	for _, spec := range []string{
		"Save the report under /home/{user}/reports before you finish.",
		"Collect every log matching /home/user/*/logs/today.txt for the summary.",
		"Expand /home/$USER/notes in the template.",
		"Compare against ~alice's copy at /home/~alice/notes.md.",
	} {
		if p := LintTaskScope(prd, "t", spec, nil); len(p) != 0 {
			t.Fatalf("placeholder/glob path flagged for %q: %v", spec, p)
		}
	}
}

func TestScope_LintIgnoresQuotedSearchPatterns(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj"}
	for _, spec := range []string{
		"search for '/home/<user>/.ssh/id_rsa' in the output",
		"search for '/home/user/secret.txt' in the output",
		"match `/home/user/secret.txt` against the log",
	} {
		if p := LintTaskScope(prd, "t", spec, nil); len(p) != 0 {
			t.Fatalf("quoted search pattern flagged for %q: %v", spec, p)
		}
	}
	// A quoted path with no pattern verb before it is still a path claim.
	if p := LintTaskScope(prd, "t", "copy '/home/user/secret.txt' into docs", nil); len(p) != 1 {
		t.Fatalf("quoted non-pattern path must still be flagged: %v", p)
	}
}

func TestScope_LintStillFlagsOutOfScopeReadPath(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj", ReadDirs: []string{"/w/ref"}}
	if p := LintTaskScope(prd, "t", "Read /home/user/workspace/datawatch/notes.md and summarise it", nil); len(p) != 1 {
		t.Fatalf("want 1 problem for out-of-scope read path, got %v", p)
	}
}

func TestScope_PlanningPromptCarriesBoundary(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj", ReadDirs: []string{"/w/ref"}}
	got := fmt.Sprintf(PlanningPromptSession, ScopeBlock(prd), "/w/proj/.out.json", "spec")
	for _, want := range []string{"/w/proj", "WRITE only under", "READ (never modify) under: /w/ref", "/w/proj/.out.json"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestScope_PlanningPromptsSteerAwayFromSystemBinaryPaths(t *testing.T) {
	prompts := map[string]string{
		"PlanningPrompt":        fmt.Sprintf(PlanningPrompt, "spec"),
		"PlanningPromptSession": fmt.Sprintf(PlanningPromptSession, "scope", "/w/proj/.out.json", "spec"),
	}
	for name, got := range prompts {
		for _, want := range []string{"system binar", "command -v"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s missing %q", name, want)
			}
		}
		if strings.Contains(got, "%!") {
			t.Errorf("%s has a formatting error: %q", name, got)
		}
	}
}

func TestScope_SetPRDDirsValidatesAndRelints(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "edit /w/other/a.go"}}}})
	if err := m.SetPRDDirs(prd.ID, nil, []string{"relative/dir"}); err == nil {
		t.Fatal("relative dir must be rejected")
	}
	if err := m.SetPRDDirs(prd.ID, nil, []string{"/w/proj"}); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if len(got.ScopeWarnings) == 0 {
		t.Fatal("expected scope warning for /w/other")
	}
	if err := m.SetPRDDirs(prd.ID, []string{"/w/other"}, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = m.Store().GetPRD(prd.ID)
	if len(got.ScopeWarnings) != 0 || len(got.WriteDirs) != 1 {
		t.Fatalf("warnings should clear and write dirs stay: %+v", got)
	}
}

// runToTerminal approves the PRD, wires executors, runs it, and returns the
// final PRD once the executor has fully exited. It shares the
// approve/drain/Run/wait-exit sequence with runToTerminalOrBlocked (both
// call settlePRD in story_halt_test.go) so the boot-resume scan never
// overlaps the executor — a fake (nonexistent) project dir lets the scan
// park a finished PRD as PRDBlocked and clobber the asserted terminal state.
func runToTerminal(t *testing.T, m *Manager, api *API, id string, spawn SpawnFn, verify VerifyFn) *PRD {
	t.Helper()
	final := settlePRD(t, m, api, id, spawn, verify)
	if final.Status != PRDCompleted && final.Status != PRDFailed {
		t.Fatalf("PRD %s ended %q, want completed or failed", id, final.Status)
	}
	return final
}

func TestScope_ExecutorBlocksOutOfScopeTaskWithoutSpawning(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	// B165 — a real project dir: a fake one lets a concurrent boot-resume
	// scan park this PRD (dir-missing) and clobber the PRDFailed state
	// the test asserts.
	prd, _ := m.CreatePRD("s", t.TempDir(), "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "work in /home/user/workspace/datawatch"}}}})
	spawned := 0
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		spawned++
		return SpawnResult{SessionID: "x"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if spawned != 0 {
		t.Fatalf("out-of-scope task spawned %d sessions", spawned)
	}
	if got.Status != PRDFailed {
		t.Fatalf("want failed, got %s", got.Status)
	}
	if got.Story[0].Status != StoryFailed {
		t.Fatalf("story with failed task must be failed, got %s", got.Story[0].Status)
	}
}

func TestScope_ExecutorSpawnsTaskNamingSystemBinary(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "If GNU time is missing, fall back to /usr/bin/time -v to measure peak RSS"}}}})
	spawned := 0
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		spawned++
		return SpawnResult{SessionID: "x"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if spawned != 1 {
		t.Fatalf("task naming a system binary spawned %d sessions, want 1", spawned)
	}
	if got.Status != PRDCompleted || got.Story[0].Status != StoryCompleted {
		t.Fatalf("want completed, got %s / %s", got.Status, got.Story[0].Status)
	}
}

func TestScope_ExecutorPrependsScopeBlockToSpec(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
	var spec string
	spawn := func(_ context.Context, r SpawnRequest) (SpawnResult, error) {
		spec = r.Spec
		return SpawnResult{SessionID: "x"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted || got.Story[0].Status != StoryCompleted {
		t.Fatalf("want completed, got %s / %s", got.Status, got.Story[0].Status)
	}
	if !strings.HasPrefix(spec, "SCOPE (hard boundary)") || !strings.Contains(spec, "write docs/x.md") {
		t.Fatalf("spec not prefixed with scope block: %q", spec)
	}
}

func TestScope_LintSpecProblemNamesTokenLineAndRemedy(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj"}
	spec := "Step one: summarise the notes.\nStep two: keep the draft in docs/x.md.\nStep three: read /home/user/workspace/datawatch/notes.md and summarise it."
	p := LintTaskScope(prd, "task 't'", spec, nil)
	if len(p) != 1 {
		t.Fatalf("want 1 problem, got %v", p)
	}
	for _, want := range []string{
		"/home/user/workspace/datawatch/notes.md",
		"line 3",
		"edit the task spec to remove or quote it, or widen the PRD read dirs if the task must read it",
	} {
		if !strings.Contains(p[0], want) {
			t.Fatalf("problem %q missing %q", p[0], want)
		}
	}
}

func TestScope_LintPlannedFileNamesPathAndRemedy(t *testing.T) {
	prd := &PRD{ProjectDir: "/w/proj", ReadDirs: []string{"/w/ref"}}
	p := LintTaskScope(prd, "task 't'", "x", []string{"/w/ref/f.md"})
	if len(p) != 1 {
		t.Fatalf("want 1 problem, got %v", p)
	}
	for _, want := range []string{
		"/w/ref/f.md",
		"remove it from the task's files or widen the PRD write dirs",
	} {
		if !strings.Contains(p[0], want) {
			t.Fatalf("problem %q missing %q", p[0], want)
		}
	}
}

// scopeLintRun runs one task whose spec names a path outside the PRD scope,
// under the given scope_lint mode, with a fake spawn. It returns the terminal
// PRD and how many sessions the executor spawned.
func scopeLintRun(t *testing.T, mode string) (*PRD, int) {
	t.Helper()
	m, api, _, verify := apiFixture(t)
	cfg := m.Config()
	cfg.ScopeLint = mode
	m.SetConfig(cfg)
	prd, _ := m.CreatePRD("s", t.TempDir(), "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "work in /home/user/workspace/datawatch"}}}})
	// Plan-time lint: SetPRDDirs re-runs it and fills ScopeWarnings per mode.
	if err := m.SetPRDDirs(prd.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	spawned := 0
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		spawned++
		return SpawnResult{SessionID: "x"}, nil
	}
	return runToTerminal(t, m, api, prd.ID, spawn, verify), spawned
}

func hasScopeLintWarnDecision(prd *PRD) bool {
	for _, d := range prd.Decisions {
		if d.Kind == "scope_lint_warn" {
			return true
		}
	}
	return false
}

func TestScope_ScopeLintOffSkipsLintAndWarnings(t *testing.T) {
	got, spawned := scopeLintRun(t, scopeLintOff)
	if spawned != 1 {
		t.Fatalf("off mode: spawned %d sessions, want 1", spawned)
	}
	if hasScopeLintWarnDecision(got) {
		t.Fatalf("off mode: unexpected scope_lint_warn decision: %+v", got.Decisions)
	}
	if len(got.ScopeWarnings) != 0 {
		t.Fatalf("off mode: ScopeWarnings should be empty, got %v", got.ScopeWarnings)
	}
	if got.Status != PRDCompleted {
		t.Fatalf("off mode: want completed, got %s", got.Status)
	}
}

func TestScope_ScopeLintWarnSpawnsAndRecordsDecision(t *testing.T) {
	got, spawned := scopeLintRun(t, scopeLintWarn)
	if spawned != 1 {
		t.Fatalf("warn mode: spawned %d sessions, want 1", spawned)
	}
	if !hasScopeLintWarnDecision(got) {
		t.Fatalf("warn mode: missing scope_lint_warn decision: %+v", got.Decisions)
	}
	if len(got.ScopeWarnings) == 0 {
		t.Fatal("warn mode: ScopeWarnings should still be populated at plan time")
	}
	if got.Status != PRDCompleted {
		t.Fatalf("warn mode: want completed, got %s", got.Status)
	}
}

func TestScope_ScopeLintEnforceBlocksWithoutSpawning(t *testing.T) {
	got, spawned := scopeLintRun(t, scopeLintEnforce)
	if spawned != 0 {
		t.Fatalf("enforce mode: out-of-scope task spawned %d sessions", spawned)
	}
	if hasScopeLintWarnDecision(got) {
		t.Fatalf("enforce mode: blocked task must not record scope_lint_warn: %+v", got.Decisions)
	}
	if len(got.ScopeWarnings) == 0 {
		t.Fatal("enforce mode: ScopeWarnings should be populated at plan time")
	}
	if got.Status != PRDFailed {
		t.Fatalf("enforce mode: want failed, got %s", got.Status)
	}
}
