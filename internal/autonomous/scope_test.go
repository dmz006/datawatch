package autonomous

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
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
	if p := LintTaskScope(prd, "t", "Work in the repo at /home/dmz/workspace/datawatch and edit it", nil); len(p) != 1 {
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

func runToTerminal(t *testing.T, m *Manager, api *API, id string, spawn SpawnFn, verify VerifyFn) *PRD {
	t.Helper()
	prd, _ := m.Store().GetPRD(id)
	m.mu.Lock()
	m.Store().WithLock(func() { prd.Status = PRDApproved })
	m.mu.Unlock()
	_ = m.Store().SavePRD(prd)
	api.SetExecutors(spawn, verify)
	if err := api.Run(id); err != nil {
		t.Fatal(err)
	}
	pollPRDTerminal(t, m, id, 5*time.Second)
	// The executor goroutine writes the terminal status inside Manager.Run,
	// then still has its deferred cleanup and a final EmitPRDUpdate to run.
	// Drain it before the caller reads the PRD.
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Store().SnapshotPRD(id)
	return got
}

func TestScope_ExecutorBlocksOutOfScopeTaskWithoutSpawning(t *testing.T) {
	m, api, _, verify := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "work in /home/dmz/workspace/datawatch"}}}})
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
