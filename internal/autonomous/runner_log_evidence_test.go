package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerEvidenceFromLog(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "output.log")
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("line\n\n")
	}
	b.WriteString("\x1b[31mred\x1b[0m\r\n\x1b]0;title\x07done\n")
	b.WriteString("DATAWATCH_PI_FAILED: pi exited with status 3\n")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	ev := RunnerEvidenceFromLog(p, "n1", "m1")
	if len(ev.LastOutput) != 40 || ev.LastOutput[37] != "red" || ev.LastOutput[38] != "done" {
		t.Fatalf("bad tail: %q", ev.LastOutput)
	}
	if ev.ExitStatus == nil || *ev.ExitStatus != 3 || ev.Node != "n1" || ev.Model != "m1" {
		t.Fatalf("bad evidence: %+v", ev)
	}
}

func TestRunnerEvidenceFromLogMissing(t *testing.T) {
	ev := RunnerEvidenceFromLog(filepath.Join(t.TempDir(), "none.log"), "n", "m")
	if ev.Node != "n" || ev.Model != "m" || ev.ExitStatus != nil || ev.LastOutput != nil {
		t.Fatalf("bad evidence: %+v", ev)
	}
}
