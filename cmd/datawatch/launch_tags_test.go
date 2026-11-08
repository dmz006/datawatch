// Launch placement tags (docs/plans/2026-11-06-automata-pages-redesign.md,
// section 12: "tag every session at launch (worker, verifier, planner,
// judge, supervisor)" with role, kind, Automaton and stage) — the shared
// one-shot session body must forward the four launch_* fields to
// /api/sessions/start when a caller sets them, and must send none of them
// when the caller passes the zero value, so existing callers post
// byte-for-byte the body they sent before this feature existed.
package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

// TestLaunchTagsPerSite pins the launch tags each one-shot launch site posts
// (docs/plans/2026-11-06-automata-pages-redesign.md, section 12 — the
// operator's "everything we can tag should be tagged"). The builder per
// site lives in session_judgment.go next to the launchTags type; the
// decompose/verifier/guardrail-judge sites in main.go and the supervisor
// site in supervisor_loop_models_wire.go all go through these, and the
// task-worker sites (an ordinary run and the auto-fix re-launch, tagged by
// the executor's placement write) are pinned by their production values
// below (role from the capacity vocabulary, kind from the autonomous
// package's TaskLaunchKind rule) — so a site silently losing its tags (or
// picking the wrong role/kind) fails here instead of showing up as
// untagged sessions later.
//
//	launch site                                             role       kind
//	decompose (decomposeFnSession, main.go)                 planner    decompose
//	verifier (runOneShotJSONSessionWithEffort, main.go)     verifier   task
//	guardrail judge (runOneShotJudgeSessionCaptureID)       judge      review (rejudge on a re-judgment)
//	supervisor loop (supervisorLoopSessionFn)                supervisor — (no Automaton)
//	task worker (executeOne, internal/autonomous/executor.go) worker     task
//	auto-fix (executeOne's auto-fix re-launch, ibid.)        worker     auto-fix
func TestLaunchTagsPerSite(t *testing.T) {
	const prd = "prd-1a2b"
	workerRole := string(capacity.RoleWorker)
	newTask := func(title string) *autonomous.Task { return &autonomous.Task{Title: title} }
	cases := []struct {
		name          string
		got           launchTags
		wantRole      string
		wantKind      string
		wantAutomaton string
		wantStage     string
	}{
		{"decompose", launchTagsForDecompose(prd), "planner", "decompose", prd, ""},
		{"verifier", launchTagsForVerify(prd, "story-1"), "verifier", "task", prd, "story-1"},
		{"guardrail judge rules-review", launchTagsForJudge(prd, false), "judge", "review", prd, ""},
		{"guardrail judge re-judgment", launchTagsForJudge(prd, true), "judge", "rejudge", prd, ""},
		{"supervisor loop", launchTagsForSupervisorLoop(), "supervisor", "", "", ""},
		// The worker sites tag through the executor's placement write, not the
		// one-shot body: the role is the capacity vocabulary's worker, and the
		// kind comes from the production rule autonomous.TaskLaunchKind (an
		// auto-fix launch is a re-run that carries the fix context, or a
		// synthesized "Auto-fix: " corrective task).
		{"task worker, fresh run", launchTags{Role: workerRole, Kind: autonomous.TaskLaunchKind(newTask("wire it"), "")}, "worker", "task", "", ""},
		{"auto-fix, re-run with fix context", launchTags{Role: workerRole, Kind: autonomous.TaskLaunchKind(newTask("wire it"), "fix the failing build")}, "worker", "auto-fix", "", ""},
		{"auto-fix, synthesized corrective task", launchTags{Role: workerRole, Kind: autonomous.TaskLaunchKind(newTask("Auto-fix: rules-review findings (attempt 1)"), "")}, "worker", "auto-fix", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.Role != tc.wantRole {
				t.Errorf("role = %q, want %q", tc.got.Role, tc.wantRole)
			}
			if tc.got.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", tc.got.Kind, tc.wantKind)
			}
			if tc.got.Automaton != tc.wantAutomaton {
				t.Errorf("automaton = %q, want %q", tc.got.Automaton, tc.wantAutomaton)
			}
			if tc.got.Stage != tc.wantStage {
				t.Errorf("stage = %q, want %q", tc.got.Stage, tc.wantStage)
			}
			// The values must stay inside the server's launch vocabulary —
			// validateSessionLaunch in internal/server refuses an unknown
			// role/kind with 400 at start time, so builder drift would break
			// real launches even if the expectations above stayed put.
			// Mirror of launchRoles/launchKinds in
			// internal/server/session_launch_tags.go.
			roles := map[string]bool{"worker": true, "verifier": true, "planner": true, "judge": true, "supervisor": true}
			kinds := map[string]bool{"task": true, "decompose": true, "review": true, "rejudge": true, "auto-fix": true}
			if tc.got.Role != "" && !roles[tc.got.Role] {
				t.Errorf("role %q is not in the server launch-role vocabulary", tc.got.Role)
			}
			if tc.got.Kind != "" && !kinds[tc.got.Kind] {
				t.Errorf("kind %q is not in the server launch-kind vocabulary", tc.got.Kind)
			}
		})
	}
}

func TestLaunchTags_PostedOnStartRequest(t *testing.T) {
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".launch-tags-set.json")
	const want = `{"ok":true}`

	srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, want, true)
	withFakeLoopback(t, srv)
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()

	_, err := runOneShotJSONSession(cfg, "prd-tags-set", projectDir, "claude-code", "sonnet",
		"autonomous:test-tags", "do it", outputFile, "launch-tags-set.json", 10*time.Second,
		launchTags{Role: "judge", Kind: "review", Automaton: "b7910001", Stage: "verify"})
	if err != nil {
		t.Fatalf("runOneShotJSONSession: %v", err)
	}

	for key, wantVal := range map[string]string{
		"launch_role":      "judge",
		"launch_kind":      "review",
		"launch_automaton": "b7910001",
		"launch_stage":     "verify",
	} {
		got, ok := startBody[key]
		if !ok {
			t.Errorf("start request has no %s field: %v", key, startBody)
			continue
		}
		if got != wantVal {
			t.Errorf("start request %s = %v, want %q", key, got, wantVal)
		}
	}
}

// A partial launchTags posts exactly the fields it set and none of the
// empty ones — the omit-when-empty contract is per field, not all-or-nothing.
func TestLaunchTags_PartialPostsOnlySetFields(t *testing.T) {
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".launch-tags-partial.json")
	const want = `{"ok":true}`

	srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, want, true)
	withFakeLoopback(t, srv)
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()

	_, err := runOneShotJSONSession(cfg, "prd-tags-partial", projectDir, "claude-code", "sonnet",
		"autonomous:test-tags", "do it", outputFile, "launch-tags-partial.json", 10*time.Second,
		launchTags{Role: "worker", Automaton: "dae00001"})
	if err != nil {
		t.Fatalf("runOneShotJSONSession: %v", err)
	}

	if got := startBody["launch_role"]; got != "worker" {
		t.Errorf("start request launch_role = %v, want worker", got)
	}
	if got := startBody["launch_automaton"]; got != "dae00001" {
		t.Errorf("start request launch_automaton = %v, want dae00001", got)
	}
	for _, key := range []string{"launch_kind", "launch_stage"} {
		if got, ok := startBody[key]; ok {
			t.Errorf("empty field %s posted as %v — it must be absent", key, got)
		}
	}
}

// Zero launchTags: not one of the four fields may appear in the start
// request at all (verified through the judge wrapper, so the
// runOneShotJSONSessionWithReader seam is the same one judges use).
func TestLaunchTags_ZeroPostsNothing(t *testing.T) {
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".launch-tags-zero.json")
	const want = `{"ok":true}`

	srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, want, true)
	withFakeLoopback(t, srv)
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()

	_, _, err := runOneShotJudgeSessionCaptureID(cfg, "prd-tags-zero", projectDir, "claude-code", "sonnet", "",
		"autonomous:test-tags-zero", "do it", outputFile, "launch-tags-zero.json", 10*time.Second, "", launchTags{})
	if err != nil {
		t.Fatalf("runOneShotJudgeSessionCaptureID: %v", err)
	}

	if got := startBody["task"]; got == nil {
		t.Fatalf("start request has no task field at all — capture is broken: %v", startBody)
	}
	for _, key := range []string{"launch_role", "launch_kind", "launch_automaton", "launch_stage"} {
		if got, ok := startBody[key]; ok {
			t.Errorf("zero launchTags posted %s = %v — an unset caller must send none of the launch fields", key, got)
		}
	}
}
