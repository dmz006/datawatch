package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func prestartWrite(t *testing.T, dir, name, body string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
}

func prestartSetMode(t *testing.T, mode string) {
	t.Helper()
	SetChecksConfig(config.AutonomousChecksConfig{PreStartVerifier: mode})
	t.Cleanup(func() { SetChecksConfig(config.AutonomousChecksConfig{}) })
}

func prestartFixture(t *testing.T, script string) (*Manager, *PRD, *Task, string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	prd, err := m.CreatePRD("prestart", dir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	return m, prd, &Task{ID: "task-1", PRDID: prd.ID, VerificationScript: script}, dir
}

func TestCheckVerifierPrestart(t *testing.T) {
	dir := t.TempDir()
	prestartWrite(t, dir, "ok.sh", "#!/usr/bin/env bash\ntrue\n", 0o755)
	prestartWrite(t, dir, "noexec.sh", "#!/bin/sh\n", 0o644)
	prestartWrite(t, dir, "noshebang.sh", "echo hi\n", 0o755)
	outside := t.TempDir()
	prestartWrite(t, outside, "evil.sh", "#!/bin/sh\n", 0o755)
	if err := os.Symlink(filepath.Join(outside, "evil.sh"), filepath.Join(dir, "link.sh")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, script, outcome, reason string
	}{
		{"valid", "ok.sh", PrestartOK, ""},
		{"empty is n/a", "  ", PrestartNA, ""},
		{"missing", "nope.sh", PrestartProblem, "does not exist"},
		{"not executable", "noexec.sh", PrestartProblem, "not executable"},
		{"no shebang", "noshebang.sh", PrestartProblem, "no shebang"},
		{"dotdot escape", "../" + filepath.Base(outside) + "/evil.sh", PrestartProblem, "escapes"},
		{"absolute escape", filepath.Join(outside, "evil.sh"), PrestartProblem, "escapes"},
		{"symlink escape", "link.sh", PrestartProblem, "outside the project"},
		{"directory", ".", PrestartProblem, "not a regular file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckVerifierPrestart(dir, c.script)
			if got.Outcome != c.outcome || !strings.Contains(got.Reason, c.reason) {
				t.Fatalf("got %+v, want outcome %s reason containing %q", got, c.outcome, c.reason)
			}
		})
	}
}

func TestGateVerifierPrestart_OnBlocksMissing(t *testing.T) {
	prestartSetMode(t, config.ChecksModeOn)
	m, prd, task, dir := prestartFixture(t, "missing.sh")
	err := m.gateVerifierPrestart(prd, task, dir)
	if err == nil {
		t.Fatal("on mode must block a missing script")
	}
	if task.Status != TaskBlocked || !strings.Contains(task.Error, "does not exist") {
		t.Fatalf("status=%s error=%q", task.Status, task.Error)
	}
	s, ok := m.store.SnapshotPRD(prd.ID)
	if !ok || s.Triage == nil {
		t.Fatal("no triage entry recorded")
	}
	found := false
	for _, n := range s.Triage.Notes {
		if n.Source == prestartTriageSource {
			found = true
		}
	}
	if !found {
		t.Fatalf("triage notes missing prestart note: %+v", s.Triage.Notes)
	}
}

func TestGateVerifierPrestart_OnBlocksOtherProblems(t *testing.T) {
	prestartSetMode(t, config.ChecksModeOn)
	for name, setup := range map[string]func(dir string) string{
		"non-executable": func(d string) string { prestartWrite(t, d, "a.sh", "#!/bin/sh\n", 0o644); return "a.sh" },
		"no shebang":     func(d string) string { prestartWrite(t, d, "b.sh", "x\n", 0o755); return "b.sh" },
		"escape":         func(string) string { return "../x.sh" },
	} {
		m, prd, task, dir := prestartFixture(t, "")
		task.VerificationScript = setup(dir)
		if err := m.gateVerifierPrestart(prd, task, dir); err == nil || task.Status != TaskBlocked {
			t.Fatalf("%s: err=%v status=%s, want blocked", name, err, task.Status)
		}
	}
}

func TestGateVerifierPrestart_ValidPasses(t *testing.T) {
	prestartSetMode(t, config.ChecksModeOn)
	m, prd, task, dir := prestartFixture(t, "ok.sh")
	prestartWrite(t, dir, "ok.sh", "#!/usr/bin/env bash\n", 0o755)
	if err := m.gateVerifierPrestart(prd, task, dir); err != nil || task.Status == TaskBlocked {
		t.Fatalf("valid script blocked: err=%v status=%s", err, task.Status)
	}
}

func TestGateVerifierPrestart_ShadowNeverBlocks(t *testing.T) {
	prestartSetMode(t, config.ChecksModeShadow)
	m, prd, task, dir := prestartFixture(t, "missing.sh")
	if err := m.gateVerifierPrestart(prd, task, dir); err != nil || task.Status == TaskBlocked {
		t.Fatalf("shadow blocked: err=%v status=%s", err, task.Status)
	}
	got := false
	for _, d := range prd.Decisions {
		if d.Kind == "verifier_prestart_shadow" {
			got = true
		}
	}
	if !got {
		t.Fatal("shadow mode recorded no finding")
	}
}

func TestGateVerifierPrestart_OffSkips(t *testing.T) {
	prestartSetMode(t, config.ChecksModeOff)
	m, prd, task, dir := prestartFixture(t, "missing.sh")
	if err := m.gateVerifierPrestart(prd, task, dir); err != nil || task.Status == TaskBlocked {
		t.Fatalf("off blocked: err=%v status=%s", err, task.Status)
	}
	if len(prd.Decisions) != 0 {
		t.Fatalf("off recorded decisions: %+v", prd.Decisions)
	}
}

func TestGateVerifierPrestart_EmptyIsNA(t *testing.T) {
	prestartSetMode(t, config.ChecksModeOn)
	m, prd, task, dir := prestartFixture(t, "")
	if err := m.gateVerifierPrestart(prd, task, dir); err != nil || task.Status == TaskBlocked {
		t.Fatalf("empty script blocked: err=%v status=%s", err, task.Status)
	}
}

func TestSpecInstructsCreateScript(t *testing.T) {
	script := ".datawatch/verify/task1.sh"
	cases := []struct {
		name, spec string
		want       bool
	}{
		{"write it to", "Write the checker first.\nWrite it to .datawatch/verify/task1.sh, then run it.", true},
		{"chmod +x same path", "Author the guardrail, then run:\nchmod +x .datawatch/verify/task1.sh", true},
		{"quoted path", "Write it to `.datawatch/verify/task1.sh`.", true},
		{"creating the script at", "Create the script at .datawatch/verify/task1.sh and make it pass.", true},
		{"base name only", "Then chmod +x task1.sh (already under .datawatch/verify/)", true},
		{"no creation verb", "The script lives at .datawatch/verify/task1.sh; run it with bash.", false},
		{"creates a different path", "Write it to .datawatch/verify/other.sh", false},
		{"verbatim instruction is not creation", "Write it VERBATIM to .datawatch/verify/task1.sh", false},
		{"empty spec", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := specInstructsCreateScript(c.spec, script); got != c.want {
				t.Fatalf("specInstructsCreateScript(%q, %q) = %v, want %v", c.spec, script, got, c.want)
			}
		})
	}
	t.Run("empty script", func(t *testing.T) {
		if got := specInstructsCreateScript("does not apply", ""); got != false {
			t.Fatalf("expected false for empty script, got %v", got)
		}
	})
}

func TestGateVerifierPrestart_OnAcceptsScriptTheTaskCreates(t *testing.T) {
	prestartSetMode(t, config.ChecksModeOn)
	script := ".datawatch/verify/task1.sh"
	m, prd, task, dir := prestartFixture(t, script)
	task.Spec = "Write the guardrail first.\nWrite it to " + script +
		" and chmod +x " + script + ", then run it with `bash " + script + "`."
	if err := m.gateVerifierPrestart(prd, task, dir); err != nil || task.Status == TaskBlocked {
		t.Fatalf("on mode must accept a script the task itself creates: err=%v status=%s", err, task.Status)
	}
	found := false
	for _, d := range prd.Decisions {
		if d.Kind == "verifier_prestart_task_created" {
			found = true
		}
		if d.Kind == "verifier_prestart_shadow" {
			t.Fatalf("on mode recorded a shadow finding: %+v", prd.Decisions)
		}
	}
	if !found {
		t.Fatalf("no task-created decision recorded: %+v", prd.Decisions)
	}
	if s, ok := m.store.SnapshotPRD(prd.ID); ok && s.Triage != nil && len(s.Triage.Notes) != 0 {
		t.Fatalf("task-created acceptance must not record triage: %+v", s.Triage.Notes)
	}
}

func TestGateVerifierPrestart_OnBlocksMissingWithoutCreateInstruction(t *testing.T) {
	prestartSetMode(t, config.ChecksModeOn)
	script := ".datawatch/verify/task1.sh"
	m, prd, task, dir := prestartFixture(t, script)
	// Mentions the path but never tells the worker to create it.
	task.Spec = "The guardrail lives at " + script + "; run it with bash."
	if err := m.gateVerifierPrestart(prd, task, dir); err == nil || task.Status != TaskBlocked {
		t.Fatalf("a spec that only mentions the script must still block: err=%v status=%s", err, task.Status)
	}
}

func TestGateVerifierPrestart_OnBlocksMissingWhenSpecCreatesOtherPath(t *testing.T) {
	prestartSetMode(t, config.ChecksModeOn)
	script := ".datawatch/verify/task1.sh"
	m, prd, task, dir := prestartFixture(t, script)
	task.Spec = "Write the guardrail to .datawatch/verify/other.sh and run it."
	if err := m.gateVerifierPrestart(prd, task, dir); err == nil || task.Status != TaskBlocked {
		t.Fatalf("creating a different path must not rescue the missing script: err=%v status=%s", err, task.Status)
	}
}

func TestGateVerifierPrestart_ShadowAcceptsScriptTheTaskCreates(t *testing.T) {
	prestartSetMode(t, config.ChecksModeShadow)
	script := ".datawatch/verify/task1.sh"
	m, prd, task, dir := prestartFixture(t, script)
	task.Spec = "Write it to " + script + ", then chmod +x " + script + "."
	if err := m.gateVerifierPrestart(prd, task, dir); err != nil || task.Status == TaskBlocked {
		t.Fatalf("shadow blocked a task-created script: err=%v status=%s", err, task.Status)
	}
	for _, d := range prd.Decisions {
		if d.Kind == "verifier_prestart_shadow" {
			t.Fatalf("task-created acceptance must not be recorded as a shadow finding: %+v", prd.Decisions)
		}
	}
}
