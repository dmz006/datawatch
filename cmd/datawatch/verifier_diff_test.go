package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t.test")
	run("config", "user.name", "t")
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "commit", "-q", "-m", name).CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
}

// TestEnsureProjectDirOwnGitRepo_FallsThroughToHome_GetsOwnRepo reproduces
// the exact a2833a5e incident: a project dir with no .git of its own, whose
// nearest ancestor repo is the operator's home directory, must get its own
// repo so a worker's later `git add -A` can't reach up into $HOME.
func TestEnsureProjectDirOwnGitRepo_FallsThroughToHome_GetsOwnRepo(t *testing.T) {
	home := t.TempDir()
	gitInit(t, home)
	commitFile(t, home, "README.md", "home repo")

	projectDir := filepath.Join(home, "workspace", "some-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ensureProjectDirOwnGitRepo(context.Background(), projectDir, home, execGit)

	top, err := exec.Command("git", "-C", projectDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("rev-parse --show-toplevel after fix: %v", err)
	}
	got := strings.TrimSpace(string(top))
	// Resolve symlinks (macOS /tmp, etc.) the same way git itself does.
	wantDir, _ := filepath.EvalSymlinks(projectDir)
	gotDir, _ := filepath.EvalSymlinks(got)
	if gotDir != wantDir {
		t.Fatalf("project dir toplevel = %q, want its own dir %q (still falls through to home)", got, wantDir)
	}
}

// TestEnsureProjectDirOwnGitRepo_AlreadyOwnRepo_NoOp verifies a project dir
// that already has its own repo (the common case) is left untouched.
func TestEnsureProjectDirOwnGitRepo_AlreadyOwnRepo_NoOp(t *testing.T) {
	home := t.TempDir()
	gitInit(t, home)
	projectDir := t.TempDir()
	gitInit(t, projectDir)

	var calls [][]string
	fakeRun := func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		return exec.Command("git", args...).Output()
	}
	ensureProjectDirOwnGitRepo(context.Background(), projectDir, home, fakeRun)

	for _, c := range calls {
		if len(c) > 0 && c[0] == "init" {
			t.Fatalf("git init called on a project dir that already has its own repo: %v", c)
		}
	}
}

// TestEnsureProjectDirOwnGitRepo_NestedInOtherRepo_NoOp verifies a project
// dir intentionally nested inside some other real (non-home) repo is left
// alone — only falling through all the way to $HOME is treated as unsafe.
func TestEnsureProjectDirOwnGitRepo_NestedInOtherRepo_NoOp(t *testing.T) {
	home := t.TempDir()
	gitInit(t, home)

	outer := t.TempDir()
	gitInit(t, outer)
	commitFile(t, outer, "README.md", "outer repo")
	nested := filepath.Join(outer, "internal", "some-subdir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	fakeRun := func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		return exec.Command("git", args...).Output()
	}
	ensureProjectDirOwnGitRepo(context.Background(), nested, home, fakeRun)

	for _, c := range calls {
		if len(c) > 0 && c[0] == "init" {
			t.Fatalf("git init called on a dir legitimately nested inside another repo: %v", c)
		}
	}
}

func TestResolveVerifierCandidates(t *testing.T) {
	resolve := func(raw string) (string, string, error) {
		switch raw {
		case "ollama-datawatch":
			return "ollama", "qwen3.8:27b", nil
		case "ollama-local-node":
			return "ollama", "qwen3:1.7b", nil
		case "claude-code":
			return "claude-code", "sonnet", nil
		case "bogus":
			return "", "", fmt.Errorf("unknown backend")
		}
		return "", "", fmt.Errorf("unknown backend")
	}
	askCompatible := func(kind string) bool { return kind == "ollama" }

	got := resolveVerifierCandidates(
		[]string{"ollama-datawatch", " ", "claude-code", "bogus", "ollama-local-node"},
		resolve, askCompatible)

	want := []verifierCandidate{
		{backend: "ollama-datawatch", kind: "ollama", model: "qwen3.8:27b"},
		{backend: "ollama-local-node", kind: "ollama", model: "qwen3:1.7b"},
	}
	if len(got) != len(want) {
		t.Fatalf("resolveVerifierCandidates() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestResolveVerifierCandidates_Empty(t *testing.T) {
	got := resolveVerifierCandidates(nil,
		func(string) (string, string, error) { return "ollama", "m", nil },
		func(string) bool { return true })
	if len(got) != 0 {
		t.Fatalf("resolveVerifierCandidates(nil) = %+v, want empty", got)
	}
}

func TestAskFailureReason(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		wantFailed bool
		wantReason string
	}{
		{"ok with answer", 200, `{"answer": "{\"ok\": true}"}`, false, ""},
		{
			"timeout error body", 503,
			`{"error":"LLM unavailable: no reachable ComputeNode for this LLM: compute node \"datawatch\" (transient): ollama: Post \"http://datawatch:11434/api/generate\": context deadline exceeded"}`,
			true, `LLM unavailable: no reachable ComputeNode for this LLM: compute node "datawatch" (transient): ollama: Post "http://datawatch:11434/api/generate": context deadline exceeded`,
		},
		{"non-200 no error field", 500, `not even json`, true, "HTTP 500"},
		{"200 but error field present", 200, `{"error":"something odd"}`, true, "something odd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, failed := askFailureReason(c.statusCode, []byte(c.body))
			if failed != c.wantFailed {
				t.Fatalf("failed = %v, want %v", failed, c.wantFailed)
			}
			if reason != c.wantReason {
				t.Fatalf("reason = %q, want %q", reason, c.wantReason)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"already bare", `{"ok": true}`, `{"ok": true}`},
		{"whitespace padded", "  \n{\"ok\": true}\n  ", `{"ok": true}`},
		{"markdown fence with json tag", "```json\n{\"ok\": true}\n```", `{"ok": true}`},
		{"markdown fence no tag", "```\n{\"ok\": true}\n```", `{"ok": true}`},
		{"leading prose", `Sure, here is the verdict: {"ok": true}`, `{"ok": true}`},
		{"trailing prose", `{"ok": true} Let me know if you need anything else.`, `{"ok": true}`},
		{"thinking preamble", "<think>\nLet me check the diff... looks fine.\n</think>\n{\"ok\": true, \"severity\": \"info\"}", `{"ok": true, "severity": "info"}`},
		{"no braces at all", "I cannot verify this.", "I cannot verify this."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractJSON(c.in)
			if got != c.want {
				t.Fatalf("extractJSON(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Reproduces PRD 0fb4e302's failure mode: a worker edits a pre-existing
// TRACKED file without committing (session.auto_git_commit=false, the
// operator default) — the commit-range diff the verifier used to rely on
// exclusively stays empty because HEAD never moves.
func TestVerifierDiff_UncommittedEditToTrackedFile_CommitRangeDiffIsEmpty(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "spec.md", "stub\n")
	preTaskSHA := gitHead(t, dir)
	taskStart := time.Now()
	time.Sleep(20 * time.Millisecond) // clear filesystem mtime resolution vs time.Now() skew

	target := filepath.Join(dir, "spec.md")
	if err := os.WriteFile(target, []byte("stub\nreal content written by the worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	committedDiff, err := exec.CommandContext(context.Background(), "git", "-C", dir, "diff", preTaskSHA+"..HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(committedDiff) != 0 {
		t.Fatalf("setup invariant broken: commit-range diff should be empty pre-fix, got %q", committedDiff)
	}

	workingDiff, _, err := gitWorkingTreeDiffSince(context.Background(), dir, taskStart)
	if err != nil {
		t.Fatalf("gitWorkingTreeDiffSince: %v", err)
	}
	if len(workingDiff) == 0 {
		t.Fatal("gitWorkingTreeDiffSince must see the uncommitted edit made during this task's run")
	}
	if taskProducedNoOutput(committedDiff, workingDiff, nil, nil) {
		t.Fatal("a real uncommitted edit must not be reported as no output")
	}
}

// Reproduces the contamination this scoping fix closes, observed live on PRD
// 0fb4e302: task 4 and task 5 both passed verification within seconds of
// spawning, against an uncommitted tracked-file edit that had been sitting in
// the working tree since BEFORE either task started (an unrelated concurrent
// source edit to the same project_dir). An unscoped `git diff HEAD` cannot
// tell that edit apart from the worker's own; scoping by mtime-since-start can.
func TestVerifierDiff_LeftoverUncommittedEditFromBeforeTaskStart_IsExcluded(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "unrelated.go", "package x\n")

	// Someone (an earlier task, or a concurrent unrelated editor) leaves an
	// uncommitted change BEFORE this task starts.
	if err := os.WriteFile(filepath.Join(dir, "unrelated.go"), []byte("package x\n// stale edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	taskStart := time.Now()

	workingDiff, _, err := gitWorkingTreeDiffSince(context.Background(), dir, taskStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(workingDiff) != 0 {
		t.Fatalf("a change made before task start must not count as this task's evidence, got %q", workingDiff)
	}
	if !taskProducedNoOutput(nil, workingDiff, nil, nil) {
		t.Fatal("with only a pre-existing leftover diff, this task must still be reported as having produced no output")
	}
}

// A change made before start (leftover) AND a change made after start (real
// work) coexist: only the post-start file is picked up.
func TestVerifierDiff_MixOfLeftoverAndRealChange_OnlyRecentCounted(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "leftover.md", "a\n")
	commitFile(t, dir, "target.md", "stub\n")
	os.WriteFile(filepath.Join(dir, "leftover.md"), []byte("a\nstale\n"), 0o644) //nolint:errcheck

	time.Sleep(20 * time.Millisecond)
	taskStart := time.Now()
	time.Sleep(20 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "target.md"), []byte("stub\nreal work\n"), 0o644) //nolint:errcheck

	workingDiff, _, err := gitWorkingTreeDiffSince(context.Background(), dir, taskStart)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workingDiff), "target.md") {
		t.Fatal("the real, recent change must be included")
	}
	if strings.Contains(string(workingDiff), "leftover.md") {
		t.Fatal("the pre-existing leftover change must be excluded")
	}
}

func TestVerifierDiff_NoOutputWhenNothingChanged(t *testing.T) {
	if !taskProducedNoOutput(nil, nil, nil, nil) {
		t.Fatal("all-empty signals must mean no output")
	}
}

func TestVerifierDiff_AnySingleSignalCounts(t *testing.T) {
	cases := []struct {
		name                      string
		committed, working        []byte
		newUntracked, overwritten []string
	}{
		{"committed diff", []byte("+x"), nil, nil, nil},
		{"working diff", nil, []byte("+x"), nil, nil},
		{"new untracked file", nil, nil, []string{"a.md"}, nil},
		{"overwritten pre-existing file", nil, nil, nil, []string{"File: a.md\ncontent"}},
	}
	for _, c := range cases {
		if taskProducedNoOutput(c.committed, c.working, c.newUntracked, c.overwritten) {
			t.Errorf("%s: must count as output", c.name)
		}
	}
}

// Auto-commit still works: when session.auto_git_commit=true and the worker's
// changes get committed during the session, the commit-range diff alone is
// non-empty and the working-tree diff is clean.
func TestVerifierDiff_CommittedChangeAloneCounts(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "spec.md", "stub\n")
	preTaskSHA := gitHead(t, dir)
	taskStart := time.Now()
	commitFile(t, dir, "spec.md", "stub\nreal content\n")

	committedDiff, err := exec.CommandContext(context.Background(), "git", "-C", dir, "diff", preTaskSHA+"..HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	workingDiff, _, err := gitWorkingTreeDiffSince(context.Background(), dir, taskStart)
	if err != nil {
		t.Fatal(err)
	}
	if len(committedDiff) == 0 {
		t.Fatal("committed change must be visible in the commit-range diff")
	}
	if len(workingDiff) != 0 {
		t.Fatal("nothing uncommitted should remain after the auto-commit")
	}
	if taskProducedNoOutput(committedDiff, workingDiff, nil, nil) {
		t.Fatal("committed change alone must count as output")
	}
}

// TestWaitingInputTick_ResetsOnAnyOtherState verifies a session that
// flickers back to Running (or any non-WaitingInput state) before the
// stall threshold doesn't accumulate towards it — the debounce exists
// specifically so a momentary disclaimer-prompt flicker (or a state
// transition still settling) isn't mistaken for a genuine stall.
func TestWaitingInputTick_ResetsOnAnyOtherState(t *testing.T) {
	ticks := 0
	for _, state := range []session.State{session.StateWaitingInput, session.StateWaitingInput, session.StateRunning, session.StateWaitingInput} {
		var stalled bool
		ticks, stalled = waitingInputTick(ticks, state)
		if stalled {
			t.Fatalf("stalled=true too early at ticks=%d (state=%s) — the Running tick in between should have reset the counter", ticks, state)
		}
	}
	if ticks != 1 {
		t.Fatalf("ticks = %d, want 1 (only the single WaitingInput tick after the reset counts)", ticks)
	}
}

// TestWaitingInputTick_StallsAtThreshold verifies sustained WaitingInput
// crosses the threshold at exactly waitingInputStallThreshold consecutive
// ticks, not before and not indefinitely after.
func TestWaitingInputTick_StallsAtThreshold(t *testing.T) {
	ticks := 0
	var stalled bool
	for i := 0; i < waitingInputStallThreshold-1; i++ {
		ticks, stalled = waitingInputTick(ticks, session.StateWaitingInput)
		if stalled {
			t.Fatalf("stalled=true after only %d tick(s), want it to hold off until %d", i+1, waitingInputStallThreshold)
		}
	}
	ticks, stalled = waitingInputTick(ticks, session.StateWaitingInput)
	if !stalled {
		t.Fatalf("stalled=false at exactly the threshold (%d consecutive ticks)", waitingInputStallThreshold)
	}
	if ticks != waitingInputStallThreshold {
		t.Fatalf("ticks = %d, want %d", ticks, waitingInputStallThreshold)
	}
}

// TestWaitingInputTick_TerminalAndRunningStatesNeverStall verifies states
// that aren't StateWaitingInput never trigger a stall, regardless of prior
// tick count — the wait loop already breaks out on terminal states before
// this function is ever consulted, but the function itself must be safe
// either way.
func TestWaitingInputTick_TerminalAndRunningStatesNeverStall(t *testing.T) {
	for _, state := range []session.State{session.StateRunning, session.StateComplete, session.StateFailed, session.StateKilled} {
		ticks, stalled := waitingInputTick(waitingInputStallThreshold+5, state)
		if stalled {
			t.Fatalf("state=%s stalled=true, want false (not StateWaitingInput)", state)
		}
		if ticks != 0 {
			t.Fatalf("state=%s ticks = %d, want reset to 0", state, ticks)
		}
	}
}

// fakeAskResolver simulates resolveAskBackend/askCompatible for
// resolveVerifierBackendModel tests without needing a real inference
// registry: raw strings that appear in namedKinds resolve as a "named LLM"
// with that kind + a fixed model; anything else resolves as a bare kind
// string with no model (mirroring the real resolveAskBackend's fallback).
func fakeAskResolver(namedKinds map[string]string, namedModels map[string]string) (
	resolve func(string) (string, string, error), compatible func(string) bool,
) {
	compatible = func(kind string) bool { return kind == "ollama" || kind == "openwebui" }
	resolve = func(raw string) (string, string, error) {
		if kind, ok := namedKinds[raw]; ok {
			return kind, namedModels[raw], nil
		}
		return raw, "", nil
	}
	return resolve, compatible
}

// fakeIsSessionKind stands in for isInferenceSessionKind in
// resolveVerifierBackendModel tests: "claude-code" is the only kind
// these tests treat as session-only, matching the real
// inference.IsSessionBackendKind's classification of that kind.
func fakeIsSessionKind(kind string) bool { return kind == "claude-code" }

// TestResolveVerifierBackendModel_DefaultsToPRDBackend covers the v8.36.0
// fix: an unconfigured verification_backend/model must default to the
// PRD's own ask-compatible backend/model, not a hardcoded "ollama" ignoring
// the PRD entirely.
func TestResolveVerifierBackendModel_DefaultsToPRDBackend(t *testing.T) {
	resolve, compatible := fakeAskResolver(
		map[string]string{"ollama-datawatch": "ollama", "ollama": "ollama"},
		map[string]string{"ollama-datawatch": "qwen3.8:27b", "ollama": "qwen3:1.7b"},
	)
	backend, kind, model, useSession := resolveVerifierBackendModel("", "", "ollama-datawatch", "qwen3.8:27b", "", resolve, compatible, fakeIsSessionKind)
	if backend != "ollama-datawatch" || kind != "ollama" || model != "qwen3.8:27b" {
		t.Fatalf("got backend=%q kind=%q model=%q, want the PRD's own ollama-datawatch/qwen3.8:27b", backend, kind, model)
	}
	if useSession {
		t.Fatal("ask-compatible kind should never request session routing")
	}
}

// TestResolveVerifierBackendModel_SessionOnlyBackendNoProjectDirFallsBackToOllama
// covers a headless PRD (no ProjectDir — nothing to spawn a session into):
// a claude-code (session-only) PRD backend has no single-shot ask adapter
// and nowhere to run a real session, so it must fall back to the global
// "ollama" default exactly as before B124 — not silently pass "claude-code"
// through to /api/ask (which would just fail differently), and not request
// session routing with no directory to run in.
func TestResolveVerifierBackendModel_SessionOnlyBackendNoProjectDirFallsBackToOllama(t *testing.T) {
	resolve, compatible := fakeAskResolver(
		map[string]string{"claude-code": "claude-code", "ollama": "ollama"},
		map[string]string{"ollama": "qwen3:1.7b"},
	)
	backend, kind, model, useSession := resolveVerifierBackendModel("", "", "claude-code", "claude-sonnet-5", "", resolve, compatible, fakeIsSessionKind)
	if backend != "ollama" || kind != "ollama" {
		t.Fatalf("got backend=%q kind=%q, want fallback to ollama (claude-code has no single-shot ask adapter and no project dir to spawn into)", backend, kind)
	}
	if model != "qwen3:1.7b" {
		t.Fatalf("model = %q, want ollama's own default (qwen3.8:27b would be a stale pairing from the discarded claude-code backend)", model)
	}
	if useSession {
		t.Fatal("no project dir means nowhere to spawn a session — useSession must be false")
	}
}

// TestResolveVerifierBackendModel_SessionOnlyBackendWithProjectDirUsesSession
// (B124) is the fix: a claude-code PRD backend WITH a real project
// directory to run in must route to a real tool-using session instead of
// silently downgrading to ollama — the whole reason BL416's verifier never
// actually got tool access despite being pointed at claude-code.
func TestResolveVerifierBackendModel_SessionOnlyBackendWithProjectDirUsesSession(t *testing.T) {
	resolve, compatible := fakeAskResolver(
		map[string]string{"claude-code": "claude-code", "ollama": "ollama"},
		map[string]string{"ollama": "qwen3:1.7b"},
	)
	backend, kind, model, useSession := resolveVerifierBackendModel("", "", "claude-code", "claude-sonnet-5", "/work/prd-123", resolve, compatible, fakeIsSessionKind)
	if !useSession {
		t.Fatal("a session-only kind with a real project dir must route to a session, not downgrade to ollama")
	}
	if backend != "claude-code" || kind != "claude-code" {
		t.Fatalf("got backend=%q kind=%q, want the real claude-code backend preserved for the session path", backend, kind)
	}
	if model != "claude-sonnet-5" {
		t.Fatalf("model = %q, want the PRD's own claude-sonnet-5 preserved", model)
	}
}

// TestResolveVerifierBackendModel_SessionIgnoresAskCompatibleConfiguredModel
// (B124 follow-up, v9.0.45) — found live: an explicitly configured
// verification_model is an ask-compatible model name (e.g. an Ollama
// model), and must NEVER be applied to a session call just because it's
// set. A real claude-code verify session rejected "qwen3.8:27b" outright
// ("There's an issue with the selected model...") and hung until the
// session timeout. The session path must use the backend's own
// registry-default model instead.
func TestResolveVerifierBackendModel_SessionIgnoresAskCompatibleConfiguredModel(t *testing.T) {
	resolve, compatible := fakeAskResolver(
		map[string]string{"claude-code": "claude-code", "ollama": "ollama"},
		map[string]string{"claude-code": "opus", "ollama": "qwen3:1.7b"},
	)
	// cfgModel ("qwen3.8:27b") is explicitly configured — an ask-only
	// model name that happens to be non-empty, the exact condition that
	// triggered the live bug.
	backend, kind, model, useSession := resolveVerifierBackendModel(
		"claude-code", "qwen3.8:27b", "", "", "/work/prd-123", resolve, compatible, fakeIsSessionKind)
	if !useSession {
		t.Fatal("a session-only kind with a real project dir must route to a session")
	}
	if backend != "claude-code" || kind != "claude-code" {
		t.Fatalf("got backend=%q kind=%q, want claude-code preserved", backend, kind)
	}
	if model == "qwen3.8:27b" {
		t.Fatal("verifyModel is the ask-compatible configured model — this is exactly the bug: a real claude-code session would reject this model name")
	}
	if model != "opus" {
		t.Fatalf("model = %q, want the session backend's own registry-default model %q", model, "opus")
	}
}

// TestResolveVerifierBackendModel_ExplicitConfigWins verifies an explicitly
// configured verification_backend/model always takes priority over the
// PRD's own backend/model.
func TestResolveVerifierBackendModel_ExplicitConfigWins(t *testing.T) {
	resolve, compatible := fakeAskResolver(
		map[string]string{"ollama-strong": "ollama", "ollama-datawatch": "ollama"},
		map[string]string{"ollama-strong": "gpt-oss:120b"},
	)
	backend, _, model, _ := resolveVerifierBackendModel("ollama-strong", "gpt-oss:120b", "ollama-datawatch", "qwen3.8:27b", "", resolve, compatible, fakeIsSessionKind)
	if backend != "ollama-strong" || model != "gpt-oss:120b" {
		t.Fatalf("got backend=%q model=%q, want the explicitly configured verification_backend/model to win", backend, model)
	}
}

// TestResolveVerifierBackendModel_NoPRDBackendFallsBackToOllama verifies an
// empty PRD backend (no config, no PRD backend set) still falls back to
// "ollama" — the pre-existing default, unaffected by this change.
func TestResolveVerifierBackendModel_NoPRDBackendFallsBackToOllama(t *testing.T) {
	resolve, compatible := fakeAskResolver(map[string]string{"ollama": "ollama"}, map[string]string{"ollama": "qwen3:1.7b"})
	backend, kind, model, useSession := resolveVerifierBackendModel("", "", "", "", "", resolve, compatible, fakeIsSessionKind)
	if backend != "ollama" || kind != "ollama" || model != "qwen3:1.7b" {
		t.Fatalf("got backend=%q kind=%q model=%q, want the ollama default", backend, kind, model)
	}
	if useSession {
		t.Fatal("empty backend defaulting to ollama should never request session routing")
	}
}

// Operator-reported (2026-10-06): a PWA story/task file chip pointed at
// "docs/02-datawatch-tls-surface.md", which never existed — the decomposer
// predicted that filename at plan time, but the worker that did the actual
// research wrote "docs/02-tls-network-surface-inventory.md" instead.
// FilesPlanned is inherently a pre-work guess (the decompose prompt itself
// says so: "Empty array is valid when paths are unknown ahead of time").
// FilesTouched is the real-evidence sibling the PWA already renders
// separately ("Output files:") but that was never populated — dedupeFiles
// is the merge step feeding it from the verifier's own git-diff evidence.
func TestDedupeFiles_MergesAndSortsAcrossEvidenceSources(t *testing.T) {
	got := dedupeFiles(
		[]string{"docs/06-comparison-recommendations.md", "docs/01-le-options-landscape.md"}, // committed diff
		[]string{"docs/04-plan-b-dns01-delegation.md"},                                       // working-tree diff
		[]string{"docs/02-tls-network-surface-inventory.md"},                                 // new untracked file — the real name, not the planned one
	)
	want := []string{
		"docs/01-le-options-landscape.md",
		"docs/02-tls-network-surface-inventory.md",
		"docs/04-plan-b-dns01-delegation.md",
		"docs/06-comparison-recommendations.md",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestDedupeFiles_DuplicatesAcrossListsCollapseToOne(t *testing.T) {
	got := dedupeFiles(
		[]string{"a.md", "b.md"},
		[]string{"b.md"}, // same file flagged by two evidence sources
		[]string{"a.md", "c.md"},
	)
	want := []string{"a.md", "b.md", "c.md"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v (duplicates must collapse)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestDedupeFiles_EmptyAndNilInputsIgnored(t *testing.T) {
	got := dedupeFiles(nil, []string{}, []string{"", "x.md", ""})
	if len(got) != 1 || got[0] != "x.md" {
		t.Fatalf("got %v, want [x.md] (nil/empty lists and blank entries must not produce spurious output)", got)
	}
}

func TestDedupeFiles_AllEmptyReturnsNil(t *testing.T) {
	if got := dedupeFiles(nil, []string{}); len(got) != 0 {
		t.Fatalf("got %v, want empty — RecordTaskFilesTouched's caller gates on len(touched) > 0 to avoid a pointless write", got)
	}
}

func TestPriorAttemptWorkLanded_OneReachableCommit(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "base.md", "base")
	baseSHA := gitHead(t, dir)
	commitFile(t, dir, "feature.md", "prior work")
	sha := gitHead(t, dir)

	diff, names, ok := priorAttemptWorkLanded(context.Background(), dir, baseSHA, []string{sha})
	if !ok {
		t.Fatalf("ok = false, want true for a reachable prior commit")
	}
	if len(diff) == 0 {
		t.Fatalf("diff empty, want the prior commit's patch")
	}
	if !strings.Contains(string(diff), "feature.md") {
		t.Fatalf("diff %q does not mention feature.md", diff)
	}
	if len(names) != 1 || names[0] != "feature.md" {
		t.Fatalf("names = %v, want [feature.md]", names)
	}
}

func TestPriorAttemptWorkLanded_NoShas(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "base.md", "base")
	baseSHA := gitHead(t, dir)

	diff, names, ok := priorAttemptWorkLanded(context.Background(), dir, baseSHA, nil)
	if ok {
		t.Fatalf("ok = true, want false with no shas")
	}
	if len(diff) != 0 {
		t.Fatalf("diff = %q, want empty", diff)
	}
	if len(names) != 0 {
		t.Fatalf("names = %v, want empty", names)
	}
}

func TestPriorAttemptWorkLanded_ResetAwayCommit(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "base.md", "base")
	baseSHA := gitHead(t, dir)

	// A commit on a branch that is then dropped: not an ancestor of HEAD.
	if out, err := exec.Command("git", "-C", dir, "checkout", "-q", "-b", "detached-line").CombinedOutput(); err != nil {
		t.Fatalf("checkout -b: %v: %s", err, out)
	}
	commitFile(t, dir, "lost.md", "work that got rolled back")
	lostSHA := gitHead(t, dir)
	if out, err := exec.Command("git", "-C", dir, "checkout", "-q", "--detach", baseSHA).CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "branch", "-D", "detached-line").CombinedOutput(); err != nil {
		t.Fatalf("branch -D: %v: %s", err, out)
	}

	diff, names, ok := priorAttemptWorkLanded(context.Background(), dir, baseSHA, []string{lostSHA})
	if ok {
		t.Fatalf("ok = true, want false for a commit reset away from HEAD (diff=%q names=%v)", diff, names)
	}
	if len(diff) != 0 {
		t.Fatalf("diff = %q, want empty", diff)
	}
	if len(names) != 0 {
		t.Fatalf("names = %v, want empty", names)
	}

	// Sanity: if baseSHA itself is not an ancestor of HEAD, refuse outright.
	_, _, ok = priorAttemptWorkLanded(context.Background(), dir, lostSHA, []string{lostSHA})
	if ok {
		t.Fatalf("ok = true, want false when baseSHA is itself unreachable")
	}
}

func TestPriorAttemptWorkLanded_TwoReachableCommits(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "base.md", "base")
	baseSHA := gitHead(t, dir)
	commitFile(t, dir, "first.md", "one")
	firstSHA := gitHead(t, dir)
	commitFile(t, dir, "second.md", "two")
	secondSHA := gitHead(t, dir)

	diff, names, ok := priorAttemptWorkLanded(context.Background(), dir, baseSHA, []string{firstSHA, secondSHA})
	if !ok {
		t.Fatalf("ok = false, want true (diff=%q)", diff)
	}
	if !strings.Contains(string(diff), "first.md") || !strings.Contains(string(diff), "second.md") {
		t.Fatalf("diff missing a commit's patch: %q", diff)
	}
	if len(names) != 2 || names[0] != "first.md" || names[1] != "second.md" {
		t.Fatalf("names = %v, want [first.md second.md] in order", names)
	}

	// Duplicate sha must not duplicate its file name.
	_, names, ok = priorAttemptWorkLanded(context.Background(), dir, baseSHA, []string{firstSHA, firstSHA})
	if !ok || len(names) != 1 || names[0] != "first.md" {
		t.Fatalf("names = %v (ok=%v), want first.md listed exactly once for a duplicated sha", names, ok)
	}
}

func TestResolveEmptyDiffWithPriorWork_DecisionTable(t *testing.T) {
	cases := []struct {
		name            string
		perAttemptEmpty bool
		hasAttemptSHAs  bool
		priorWorkLanded bool
		wantFallback    bool
	}{
		{"retry with a reachable prior commit: fall back, not no-output", true, true, true, true},
		{"retry whose prior commit is unreachable: no fallback, still no-output", true, true, false, false},
		{"first attempt with genuinely empty diff: no fallback, still no-output", true, false, true, false},
		{"first attempt, no shas, nothing landed: no fallback", true, false, false, false},
		{"evidence present: fallback never consulted", false, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveEmptyDiffWithPriorWork(tc.perAttemptEmpty, tc.hasAttemptSHAs, tc.priorWorkLanded)
			if got != tc.wantFallback {
				t.Fatalf("resolveEmptyDiffWithPriorWork(%v, %v, %v) = %v, want %v",
					tc.perAttemptEmpty, tc.hasAttemptSHAs, tc.priorWorkLanded, got, tc.wantFallback)
			}
		})
	}
}

// TestResolveEmptyDiffWithPriorWork_Retries is the git-backed half of the
// verifier's empty-diff decision: it drives priorAttemptWorkLanded with real
// repos exactly as the verifier does, then runs the pure decision on the
// result, so the three spec cases (reachable / unreachable / first attempt)
// are covered end to end without standing up the whole verifier.
func TestResolveEmptyDiffWithPriorWork_Retries(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	commitFile(t, dir, "base.md", "base")
	baseSHA := gitHead(t, dir)

	// 1. Retry with a reachable prior commit -> not reported as no-output.
	commitFile(t, dir, "work.md", "real work from the earlier attempt")
	earlierSHA := gitHead(t, dir)
	perAttemptEmpty := taskProducedNoOutput(nil, nil, nil, nil) // this attempt committed nothing
	if !perAttemptEmpty {
		t.Fatalf("test setup: wanted an empty per-attempt evidence bundle")
	}
	priorDiff, priorNames, priorOk := priorAttemptWorkLanded(context.Background(), dir, baseSHA, []string{earlierSHA})
	if !priorOk {
		t.Fatalf("priorAttemptWorkLanded ok = false for a reachable commit (diff=%q)", priorDiff)
	}
	if !resolveEmptyDiffWithPriorWork(perAttemptEmpty, len([]string{earlierSHA}) > 0, priorOk) {
		t.Fatalf("retry with a reachable prior commit must fall back to the prior diff, not report no-output")
	}
	if len(priorDiff) == 0 || !strings.Contains(string(priorDiff), "work.md") {
		t.Fatalf("prior diff must carry the reachable commit's patch for the verifier's evidence: %q", priorDiff)
	}
	if len(priorNames) == 0 {
		t.Fatalf("prior names = %v, want the commit's changed files for RecordTaskFilesTouched", priorNames)
	}

	// 2. Retry whose prior commit is unreachable -> still no-output.
	if out, err := exec.Command("git", "-C", dir, "reset", "--hard", baseSHA).CombinedOutput(); err != nil {
		t.Fatalf("reset --hard: %v: %s", err, out)
	}
	_, _, priorOk = priorAttemptWorkLanded(context.Background(), dir, baseSHA, []string{earlierSHA})
	if priorOk {
		t.Fatalf("priorAttemptWorkLanded ok = true after the commit was reset away from HEAD")
	}
	perAttemptEmpty = taskProducedNoOutput(nil, nil, nil, nil)
	if resolveEmptyDiffWithPriorWork(perAttemptEmpty, len([]string{earlierSHA}) > 0, priorOk) {
		t.Fatalf("retry whose prior commit is unreachable must still be reported as no-output")
	}

	// 3. First attempt with a genuinely empty diff (no attempt commits) ->
	//    still no-output, even though a reachable commit happens to exist.
	if resolveEmptyDiffWithPriorWork(true, false, true) {
		t.Fatalf("first attempt with an empty diff must still be reported as no-output")
	}
	if resolveEmptyDiffWithPriorWork(perAttemptEmpty, false, true) {
		t.Fatalf("verifier must not consult prior-attempt work when AttemptCommitSHAs is empty")
	}
}
