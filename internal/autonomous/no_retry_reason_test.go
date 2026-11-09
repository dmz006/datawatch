package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testVerifyScript = ".datawatch/verify/check.sh"

type noRetryCase struct {
	name       string
	task       *Task
	v          VerificationResult
	previous   []string
	wantDir    bool // give the task a temp project dir of its own
	prdDir     bool // the task's directory comes from the PRD's ProjectDir (task itself has none)
	scriptFile bool // additionally create the task's verification script in the dir
	wantOK     bool
	wantReason string
}

func TestNoRetryReason(t *testing.T) {
	noFiles := VerificationResult{OK: false, Summary: "no files changed; nothing to verify"}

	cases := []noRetryCase{
		{
			name:       "no files changed on a read-only task",
			task:       &Task{ReadOnly: true},
			v:          noFiles,
			wantOK:     true,
			wantReason: "writes nothing",
		},
		{
			name:       "no files changed on a task with no planned files",
			task:       &Task{},
			v:          noFiles,
			wantOK:     true,
			wantReason: "writes nothing",
		},
		{
			name:       "no files changed reported by a nil task",
			task:       nil,
			v:          noFiles,
			wantOK:     true,
			wantReason: "writes nothing",
		},
		{
			name:       "no files changed in an issue line",
			task:       &Task{ReadOnly: true},
			v:          VerificationResult{OK: false, Summary: "failed", Issues: []string{"No files changed in the diff"}},
			wantOK:     true,
			wantReason: "no files changed",
		},
		{
			name:   "no files changed on a task that writes files is retried",
			task:   &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}},
			v:      noFiles,
			wantOK: false,
		},
		{
			name:   "read-only task with a changing-files failure is retried",
			task:   &Task{ReadOnly: true},
			v:      VerificationResult{OK: false, Summary: "tests fail after the change"},
			wantOK: false,
		},
		{
			name:       "verification script missing from the task's own project dir",
			task:       &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}, VerificationScript: testVerifyScript},
			v:          VerificationResult{OK: false, Summary: "build fails"},
			wantDir:    true,
			wantOK:     true,
			wantReason: "check.sh",
		},
		{
			name:       "verification script missing from the PRD's project dir",
			task:       &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}, VerificationScript: testVerifyScript},
			v:          VerificationResult{OK: false, Summary: "build fails"},
			prdDir:     true,
			wantOK:     true,
			wantReason: "check.sh",
		},
		{
			name:       "verification script present in the PRD's project dir is not treated as missing",
			task:       &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}, VerificationScript: testVerifyScript},
			v:          VerificationResult{OK: false, Summary: "build fails"},
			prdDir:     true,
			scriptFile: true,
			wantOK:     false,
		},
		{
			name:       "script missing in both the task's and the PRD's dirs is no-retry",
			task:       &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}, VerificationScript: testVerifyScript},
			v:          VerificationResult{OK: false, Summary: "build fails"},
			wantDir:    true,
			prdDir:     true,
			wantOK:     true,
			wantReason: "check.sh",
		},
		{
			name:   "script naming an absolute path is a path error, not missing",
			task:   &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}, VerificationScript: "/etc/passwd"},
			v:      VerificationResult{OK: false, Summary: "build fails"},
			wantOK: false,
		},
		{
			name: "two identical earlier summaries",
			task: &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}},
			v:    VerificationResult{OK: false, Summary: "build failed: link error"},
			previous: []string{
				"  Build failed: LINK ERROR  ",
				"build failed: link error",
			},
			wantOK:     true,
			wantReason: "failed twice",
		},
		{
			name:     "one identical earlier summary is retried",
			task:     &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}},
			v:        VerificationResult{OK: false, Summary: "build failed: link error"},
			previous: []string{"build failed: link error"},
			wantOK:   false,
		},
		{
			name:     "a different second summary is retried",
			task:     &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}},
			v:        VerificationResult{OK: false, Summary: "timed out after 300s"},
			previous: []string{"build failed: link error"},
			wantOK:   false,
		},
		{
			name:     "two identical summaries followed by a different one is retried",
			task:     &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}},
			v:        VerificationResult{OK: false, Summary: "disk full"},
			previous: []string{"flaky network", "flaky network"},
			wantOK:   false,
		},
		{
			name:     "blank summary never counts as a repeated failure",
			task:     &Task{FilesPlanned: []string{"internal/autonomous/feature.go"}},
			v:        VerificationResult{OK: false, Summary: "   "},
			previous: []string{"build failed: link error", "build failed: link error"},
			wantOK:   false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.task == nil {
				reason, ok := noRetryReason(c.task, c.v, c.previous)
				assertNoRetryResult(t, reason, ok, c)
				return
			}
			task := *c.task // per-run copy; the case table is shared
			prd := &PRD{}
			if c.wantDir {
				task.ProjectDir = t.TempDir()
				if c.scriptFile {
					mustWriteScript(t, task.ProjectDir)
				}
			}
			if c.prdDir {
				prd.ProjectDir = t.TempDir()
				if c.scriptFile {
					mustWriteScript(t, prd.ProjectDir)
				}
			}
			// The spec's no-context entry point is pinned for cases with no
			// PRD; cases whose directory comes from the PRD go through
			// noRetryReasonWithPRD, which sees the PRD's ProjectDir.
			var (
				reason string
				ok     bool
			)
			if c.prdDir && !c.wantDir {
				reason, ok = noRetryReasonWithPRD(prd, &task, c.v, c.previous)
			} else {
				reason, ok = noRetryReason(&task, c.v, c.previous)
			}
			assertNoRetryResult(t, reason, ok, c)
		})
	}
}

// TestNoRetryReasonWithPRD pins the precedence a caller with the PRD in
// hand gets: the task's own project dir is looked up before the PRD's, and
// when the task's dir holds the script the PRD-level absence of it does not
// make case (b) fire.
func TestNoRetryReasonWithPRD(t *testing.T) {
	taskDir := t.TempDir()
	prdDir := t.TempDir()
	mustWriteScript(t, taskDir) // task's dir has the script
	// prdDir deliberately lacks the script.
	task := &Task{
		FilesPlanned:       []string{"internal/autonomous/feature.go"},
		VerificationScript: testVerifyScript,
		ProjectDir:         taskDir,
	}
	v := VerificationResult{OK: false, Summary: "build fails"}
	if reason, ok := noRetryReasonWithPRD(&PRD{ProjectDir: prdDir}, task, v, nil); ok {
		t.Fatalf("ok = true (reason %q), want retry: the task's own dir holds the script", reason)
	}
	// Same task without its own dir: the PRD's dir is the lookup dir and
	// the script is absent there, so case (b) fires.
	bare := *task
	bare.ProjectDir = ""
	reason, ok := noRetryReasonWithPRD(&PRD{ProjectDir: prdDir}, &bare, v, nil)
	if !ok {
		t.Fatalf("ok = false, want true: the PRD's dir has no script")
	}
	if !strings.Contains(reason, testVerifyScript) {
		t.Fatalf("reason = %q, want it to name %s", reason, testVerifyScript)
	}
}

// mustWriteScript writes the canonical test verification script into dir.
func mustWriteScript(t *testing.T, dir string) {
	t.Helper()
	scriptPath := filepath.Join(dir, testVerifyScript)
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o755); err != nil {
		t.Fatalf("mkdir script dir: %v", err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
}

// TestVerificationScriptMissing pins the missing-vs-path-error distinction:
// an empty, absolute, or escaping path is a path error (false), an absent
// file or a non-regular file is missing (true).
func TestVerificationScriptMissing(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, testVerifyScript)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch", "verify", "asdir"), 0o755); err != nil {
		t.Fatalf("mkdir asdir: %v", err)
	}
	cases := []struct {
		name  string
		path  string
		setup func()
		want  bool
	}{
		{name: "empty path is a path error", path: "", want: false},
		{name: "absent file is missing", path: testVerifyScript, want: true},
		{name: "present regular file is not missing", path: testVerifyScript, setup: func() {
			if err := os.WriteFile(file, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
				t.Fatalf("write: %v", err)
			}
		}, want: false},
		{name: "directory in the script's place is missing", path: ".datawatch/verify/asdir", want: true},
		{name: "absolute path is a path error", path: "/etc/passwd", want: false},
		{name: "escaping path is a path error", path: "../outside.sh", want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.setup != nil {
				c.setup()
			}
			if got := verificationScriptMissing(c.path, dir); got != c.want {
				t.Fatalf("verificationScriptMissing(%q, %q) = %v, want %v", c.path, dir, got, c.want)
			}
		})
	}
}

func assertNoRetryResult(t *testing.T, reason string, ok bool, c noRetryCase) {
	t.Helper()
	if ok != c.wantOK {
		t.Fatalf("noRetryReason ok = %v (reason %q), want %v", ok, reason, c.wantOK)
	}
	if ok && !strings.Contains(reason, c.wantReason) {
		t.Fatalf("reason = %q, want it to contain %q", reason, c.wantReason)
	}
	if !ok && reason != "" {
		t.Fatalf("reason = %q on a retry, want empty", reason)
	}
}
