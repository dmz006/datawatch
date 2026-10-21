package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func undeclaredPaths(task *Task) []string {
	var out []string
	for _, e := range task.UndeclaredEdits {
		out = append(out, e.Path)
	}
	return out
}

func TestCheckUndeclaredEdits_FilesGeneratedExempt(t *testing.T) {
	m, prd, task, dir, _, alerts := undeclaredFixture(t, "block", []string{"a.go"})
	task.FilesGenerated = []string{".datawatch/verify/t1.sh"}
	driftWrite(t, dir, "a.go", "package main\n\ntype a int\n")
	if err := os.MkdirAll(filepath.Join(dir, ".datawatch/verify"), 0o755); err != nil {
		t.Fatal(err)
	}
	driftWrite(t, dir, ".datawatch/verify/t1.sh", "#!/bin/sh\n")
	driftGit(t, dir, "add", "-A")
	driftGit(t, dir, "commit", "-q", "-m", "work")

	if m.checkUndeclaredEdits(context.Background(), prd, task) {
		t.Fatal("generated + planned files must not block")
	}
	if len(task.UndeclaredEdits) != 0 || len(alerts.all()) != 0 {
		t.Fatalf("unexpected findings %v alerts %v", task.UndeclaredEdits, alerts.all())
	}
}

func TestCheckUndeclaredEdits_NoDoubleReportWithPreExisting(t *testing.T) {
	m, prd, task, dir, _, _ := undeclaredFixture(t, "warn", []string{"a.go"})
	// operator's pre-dirty, unplanned file swept into the task commit
	driftWrite(t, dir, "operator.txt", "mine\n")
	task.PreTaskUntrackedFiles = []string{"operator.txt"}
	driftWrite(t, dir, "b.go", "package main\n")
	driftGit(t, dir, "add", "-A")
	driftGit(t, dir, "commit", "-q", "-m", "work")

	m.checkUndeclaredEdits(context.Background(), prd, task)
	if got := undeclaredPaths(task); !reflect.DeepEqual(got, []string{"b.go"}) {
		t.Fatalf("undeclared = %v, want only b.go (operator.txt is pre-existing drift)", got)
	}
	pre, _ := ClassifyFileDrift(task, task.PreTaskUntrackedFiles, task.PreTaskDirtyFiles, []string{"b.go", "operator.txt"})
	if !reflect.DeepEqual(pre, []string{"operator.txt"}) {
		t.Fatalf("preExisting = %v", pre)
	}
}

func TestCheckUndeclaredEdits_PreDirtyPlannedFileNotReported(t *testing.T) {
	m, prd, task, dir, _, _ := undeclaredFixture(t, "block", []string{"a.go"})
	task.PreTaskDirtyFiles = []string{"a.go"}
	driftWrite(t, dir, "a.go", "package main\n\ntype a int\n")
	driftGit(t, dir, "add", "-A")
	driftGit(t, dir, "commit", "-q", "-m", "work")

	if m.checkUndeclaredEdits(context.Background(), prd, task) || len(task.UndeclaredEdits) != 0 {
		t.Fatalf("pre-dirty planned file must be ignored, got %v", task.UndeclaredEdits)
	}
}

func TestCheckUndeclaredEdits_WorktreeModeSkipped(t *testing.T) {
	m, prd, task, dir, audit, alerts := undeclaredFixture(t, "block", []string{"a.go"})
	prd.Git.Branch = "automaton/x"
	workerCommit(t, dir)

	if m.checkUndeclaredEdits(context.Background(), prd, task) || len(task.UndeclaredEdits) != 0 {
		t.Fatal("worktree-mode PRD must be skipped")
	}
	if len(audit.acts) != 0 || len(alerts.all()) != 0 {
		t.Fatal("no audit/alert expected")
	}
}

func TestCheckUndeclaredEdits_ModeOffNoOp(t *testing.T) {
	m, prd, task, dir, audit, alerts := undeclaredFixture(t, "off", []string{"a.go"})
	workerCommit(t, dir)

	if m.checkUndeclaredEdits(context.Background(), prd, task) || len(task.UndeclaredEdits) != 0 {
		t.Fatal("off must be a no-op")
	}
	if len(audit.acts) != 0 || len(alerts.all()) != 0 || len(prd.Decisions) != 0 {
		t.Fatal("off must record nothing")
	}
}

func TestCheckUndeclaredEdits_BlockBlocksWarnDoesNot(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		blocked bool
	}{{"block", true}, {"warn", false}} {
		m, prd, task, dir, audit, alerts := undeclaredFixture(t, tc.mode, []string{"a.go"})
		workerCommit(t, dir)

		got := m.checkUndeclaredEdits(context.Background(), prd, task)
		if got != tc.blocked || (task.Status == TaskBlocked) != tc.blocked {
			t.Fatalf("%s: blocked=%v status=%s", tc.mode, got, task.Status)
		}
		if tc.blocked && task.Error != "undeclared_edit: b.go" {
			t.Fatalf("error = %q", task.Error)
		}
		if len(task.UndeclaredEdits) != 1 || task.UndeclaredEdits[0].LinesChanged != 3 {
			t.Fatalf("%s: edits = %v", tc.mode, task.UndeclaredEdits)
		}
		if len(audit.acts) != 1 || audit.acts[0] != "autonomous_undeclared_edit_detected" {
			t.Fatalf("%s: audit = %v", tc.mode, audit.acts)
		}
		if len(alerts.all()) != 1 {
			t.Fatalf("%s: alerts = %v", tc.mode, alerts.all())
		}
		last := prd.Decisions[len(prd.Decisions)-1]
		if last.Kind != "undeclared_edit_detected" {
			t.Fatalf("%s: decision kind %q", tc.mode, last.Kind)
		}
	}
}

func TestCheckUndeclaredEdits_NilAlertSinkSafe(t *testing.T) {
	m, prd, task, dir, _, _ := undeclaredFixture(t, "warn", []string{"a.go"})
	m.SetAlertSink(nil)
	workerCommit(t, dir)
	m.checkUndeclaredEdits(context.Background(), prd, task)
	if len(task.UndeclaredEdits) != 1 {
		t.Fatalf("edits = %v", task.UndeclaredEdits)
	}
}
