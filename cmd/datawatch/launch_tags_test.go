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

	"github.com/dmz006/datawatch/internal/config"
)

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

	if got, _ := startBody["task"]; got == nil {
		t.Fatalf("start request has no task field at all — capture is broken: %v", startBody)
	}
	for _, key := range []string{"launch_role", "launch_kind", "launch_automaton", "launch_stage"} {
		if got, ok := startBody[key]; ok {
			t.Errorf("zero launchTags posted %s = %v — an unset caller must send none of the launch fields", key, got)
		}
	}
}
