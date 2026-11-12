package autonomous

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParsePiPromptExitStatus(t *testing.T) {
	cases := []struct {
		name   string
		pane   string
		want   int
		wantOK bool
	}{
		{"status one", "working\nDATAWATCH_PI_FAILED: pi exited with status 1 (model m)\n", 1, true},
		{"status 127", "sh: pi: not found\nDATAWATCH_PI_FAILED: pi exited with status 127 (model m)\r\n", 127, true},
		{"no marker", "all quiet\nDATAWATCH_PI_FAILED: pi produced no output\n", 0, false},
		{"empty pane", "", 0, false},
		{"last marker wins",
			"DATAWATCH_PI_FAILED: pi exited with status 1 (model m)\nretry\nDATAWATCH_PI_FAILED: pi exited with status 2 (model m)\n", 2, true},
		{"marker without number", "DATAWATCH_PI_FAILED: pi exited with status \n", 0, false},
	}
	for _, c := range cases {
		got, ok := ParsePiPromptExitStatus(c.pane)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

// TestPiExitMarkerPrefixMatchesBackend reads the backend source rather than
// importing it, and checks that the launch command it builds prints the exit
// line in the shape the prefix describes: the prefix, then a status verb.
func TestPiExitMarkerPrefixMatchesBackend(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "llm", "backends", "pi", "backend.go"))
	if err != nil {
		t.Fatalf("read backend source: %v", err)
	}
	start := strings.Index(string(src), "func buildLaunchCmdWithAgentDir(")
	if start < 0 {
		t.Fatal("buildLaunchCmdWithAgentDir not found in backend source")
	}
	body := string(src)[start:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}
	// The Go source holds the shell printf format with the status verb
	// escaped for Sprintf (%%s); render it for a sample status.
	if !strings.Contains(body, "printf '"+piExitMarkerPrefix+"%%s") {
		t.Fatalf("launch command no longer prints %q followed by the status", piExitMarkerPrefix)
	}
	format := piExitMarkerPrefix + "%s"
	line := fmt.Sprintf(format+" (model %s)", "127", "m")
	if got, ok := ParsePiPromptExitStatus(line); !ok || got != 127 {
		t.Fatalf("sample line %q parsed as (%d, %v)", line, got, ok)
	}
}

func TestRunnerEvidenceFromCapsTailAndDropsEmptyLines(t *testing.T) {
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i), "", "   ")
	}
	ev := RunnerEvidenceFrom(strings.Join(lines, "\n"), nil)
	if len(ev.LastOutput) != 20 {
		t.Fatalf("tail length = %d, want 20", len(ev.LastOutput))
	}
	if ev.LastOutput[0] != "line 11" || ev.LastOutput[19] != "line 30" {
		t.Fatalf("tail = %v", ev.LastOutput)
	}
	if ev.ExitStatus != nil {
		t.Fatalf("ExitStatus = %d, want nil", *ev.ExitStatus)
	}
}

func TestRunnerEvidenceFromStatusAndProcessCap(t *testing.T) {
	procs := make([]string, 55)
	for i := range procs {
		procs[i] = fmt.Sprintf("proc %d", i)
	}
	ev := RunnerEvidenceFrom("x\nDATAWATCH_PI_FAILED: pi exited with status 3 (model m)\n", procs)
	if ev.ExitStatus == nil || *ev.ExitStatus != 3 {
		t.Fatalf("ExitStatus = %v, want 3", ev.ExitStatus)
	}
	if len(ev.Processes) != 40 || ev.Processes[39] != "proc 39" {
		t.Fatalf("processes = %d entries, last %q", len(ev.Processes), ev.Processes[len(ev.Processes)-1])
	}
}

func TestRecordFailedAttemptCopiesAndClearsRunnerEvidence(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	taskID := prd.Story[0].Tasks[0].ID
	task := lookupTask(prd, taskID)

	status := 127
	m.store.WithLock(func() {
		task.RunnerEvidence = &RunnerEvidence{
			ExitStatus: &status,
			LastOutput: []string{"a", "b"},
			Processes:  []string{"pi --print"},
		}
	})
	m.recordFailedAttempt(context.Background(), prd, task, attemptRun{},
		attemptFailure{Outcome: attemptOutcomeStalled, Summary: "stalled"})

	got, _ := m.Store().GetPRD(prd.ID)
	rt := lookupTask(got, taskID)
	if rt.RunnerEvidence != nil {
		t.Fatalf("task RunnerEvidence not cleared: %+v", rt.RunnerEvidence)
	}
	if len(rt.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(rt.Attempts))
	}
	a := rt.Attempts[0]
	if a.RunnerExitStatus == nil || *a.RunnerExitStatus != 127 {
		t.Fatalf("RunnerExitStatus = %v, want 127", a.RunnerExitStatus)
	}
	if !reflect.DeepEqual(a.RunnerLastOutput, []string{"a", "b"}) || !reflect.DeepEqual(a.RunnerProcesses, []string{"pi --print"}) {
		t.Fatalf("attempt evidence = %+v", a)
	}

	// A second attempt with no evidence records none.
	m.recordFailedAttempt(context.Background(), got, rt, attemptRun{},
		attemptFailure{Outcome: attemptOutcomeFailed, Summary: "again"})
	got, _ = m.Store().GetPRD(prd.ID)
	b := lookupTask(got, taskID).Attempts[1]
	if b.RunnerExitStatus != nil || b.RunnerLastOutput != nil || b.RunnerProcesses != nil {
		t.Fatalf("attempt without evidence carries some: %+v", b)
	}
}

func TestFormatEscalationAttemptFullRunnerEvidence(t *testing.T) {
	base := TaskAttempt{Number: 2, Backend: "pi-prompt", Outcome: attemptOutcomeStalled, FailureSummary: "stalled"}
	plain := formatEscalationAttemptFull(base)
	if strings.Contains(plain, "Runner") {
		t.Fatalf("attempt without evidence mentions the runner:\n%s", plain)
	}

	status := 1
	with := base
	with.RunnerExitStatus = &status
	with.RunnerLastOutput = []string{"boom"}
	with.RunnerProcesses = []string{"node pi"}
	got := formatEscalationAttemptFull(with)
	if !strings.HasPrefix(got, plain) {
		t.Fatalf("evidence must only append to the existing block:\n%s", got)
	}
	for _, want := range []string{"  Runner exit status: 1\n", "  Runner last output:\n    boom\n", "  Runner processes:\n    - node pi\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
