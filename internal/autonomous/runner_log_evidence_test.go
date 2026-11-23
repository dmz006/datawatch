package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// numberedLines returns "l1\nl2\n...\nlN\n".
func numberedLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "l%d\n", i)
	}
	return b.String()
}

func TestRunnerEvidenceFromLogContent(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantLast   []string
		wantStatus *int
	}{
		{
			name:     "ANSI CSI sequences stripped",
			content:  "\x1b[31mred\x1b[0m text\n\x1b[1;32mgreen\x1b[0m\n",
			wantLast: []string{"red text", "green"},
		},
		{
			name:     "OSC title sequence and carriage returns stripped",
			content:  "\x1b]0;title\x07ready\r\nnext\r\n",
			wantLast: []string{"ready", "next"},
		},
		{
			name:     "blank lines dropped",
			content:  "a\n\n   \nb\n",
			wantLast: []string{"a", "b"},
		},
		{
			name:     "exactly forty lines all kept",
			content:  numberedLines(40),
			wantLast: strings.Split(strings.TrimSuffix(numberedLines(40), "\n"), "\n"),
		},
		{
			name:     "only the last forty lines kept",
			content:  numberedLines(55),
			wantLast: strings.Split(strings.TrimSuffix(numberedLines(55), "\n"), "\n")[15:],
		},
		{
			name:       "exit status parsed from failed line",
			content:    "work\nDATAWATCH_PI_FAILED: pi exited with status 3\n",
			wantLast:   []string{"work", "DATAWATCH_PI_FAILED: pi exited with status 3"},
			wantStatus: intPtr(3),
		},
		{
			name:       "exit status survives escape codes and trimming",
			content:    numberedLines(60) + "\x1b[31mDATAWATCH_PI_FAILED: pi exited with status 12\x1b[0m\r\n",
			wantLast:   append(strings.Split(strings.TrimSuffix(numberedLines(60), "\n"), "\n")[21:], "DATAWATCH_PI_FAILED: pi exited with status 12"),
			wantStatus: intPtr(12),
		},
		{
			name:     "no failed line means no exit status",
			content:  "all good\n",
			wantLast: []string{"all good"},
		},
		{
			name:     "empty file",
			content:  "",
			wantLast: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "output.log")
			if err := os.WriteFile(p, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			ev := RunnerEvidenceFromLog(p, "node-a", "model-a")
			if !reflect.DeepEqual(ev.LastOutput, tc.wantLast) {
				t.Fatalf("LastOutput = %q, want %q", ev.LastOutput, tc.wantLast)
			}
			if len(ev.LastOutput) > 40 {
				t.Fatalf("kept %d lines, want at most 40", len(ev.LastOutput))
			}
			switch {
			case tc.wantStatus == nil && ev.ExitStatus != nil:
				t.Fatalf("ExitStatus = %d, want nil", *ev.ExitStatus)
			case tc.wantStatus != nil && (ev.ExitStatus == nil || *ev.ExitStatus != *tc.wantStatus):
				t.Fatalf("ExitStatus = %v, want %d", ev.ExitStatus, *tc.wantStatus)
			}
			if ev.Node != "node-a" || ev.Model != "model-a" {
				t.Fatalf("node/model not carried: %+v", ev)
			}
		})
	}
}

func TestRunnerEvidenceFromLogNodeModelCarried(t *testing.T) {
	p := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ node, model string }{
		{"n1", "m1"},
		{"", ""},
		{"gpu-node", "qwen3.8:27b"},
	}
	for _, tc := range tests {
		t.Run(tc.node+"/"+tc.model, func(t *testing.T) {
			ev := RunnerEvidenceFromLog(p, tc.node, tc.model)
			if ev.Node != tc.node || ev.Model != tc.model {
				t.Fatalf("got node=%q model=%q", ev.Node, ev.Model)
			}
		})
	}
}

func TestRunnerEvidenceFromLogMissingFile(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
	}{
		{"nonexistent file", filepath.Join(dir, "none.log")},
		{"path is a directory", dir},
		{"empty path", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := RunnerEvidenceFromLog(tc.path, "n", "m")
			if ev.Node != "n" || ev.Model != "m" || ev.ExitStatus != nil || ev.LastOutput != nil {
				t.Fatalf("bad evidence: %+v", ev)
			}
		})
	}
}
