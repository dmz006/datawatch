package mergeshadow

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// rootStrayScenarioPath is the fixture that describes the repository in which
// the Automaton branch adds a file at the repository root.
const rootStrayScenarioPath = "../../testdata/mergeshadow/root-stray-file/scenario.json"

func loadRootStrayScenario(t *testing.T) replayScenario {
	t.Helper()
	data, err := os.ReadFile(rootStrayScenarioPath) // #nosec G304 -- fixed fixture path
	if err != nil {
		t.Fatalf("read scenario: %v", err)
	}
	var s replayScenario
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse scenario: %v", err)
	}
	return s
}

// TestReplayStrayRootFileBlocksPlan builds the root-stray-file scenario and
// checks that the blocking stray_root_file finding names the root file the
// Automaton branch added. The subdirectory file and the edit to the root
// README.md, which main already tracks, must not be named.
func TestReplayStrayRootFileBlocksPlan(t *testing.T) {
	s := loadRootStrayScenario(t)
	repo := buildReplayRepo(t, s)

	findings, err := CheckStrayRootFiles(context.Background(), repo, s.Branch, s.BaseRef)
	if err != nil {
		t.Fatalf("CheckStrayRootFiles: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings %+v, want exactly one", findings)
	}
	f := findings[0]
	if f.Code != StrayRootFileCode {
		t.Errorf("code = %q, want %q", f.Code, StrayRootFileCode)
	}
	if !f.Blocking {
		t.Error("finding is not blocking")
	}
	if !strings.Contains(f.Message, "stray-notes.txt") {
		t.Errorf("message %q does not name stray-notes.txt", f.Message)
	}
	for _, other := range []string{"docs/notes.md", "README.md"} {
		if strings.Contains(f.Message, other) {
			t.Errorf("message %q names %s, which must not be flagged", f.Message, other)
		}
	}
}

// TestCheckStrayRootFilesRejectsBadInput covers the argument guard.
func TestCheckStrayRootFilesRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	if _, err := CheckStrayRootFiles(ctx, "", "b", "main"); err == nil {
		t.Error("empty repo path accepted")
	}
	if _, err := CheckStrayRootFiles(ctx, t.TempDir(), "-x", "main"); err == nil {
		t.Error("branch starting with '-' accepted")
	}
	if _, err := CheckStrayRootFiles(ctx, t.TempDir(), "b", ""); err == nil {
		t.Error("empty base ref accepted")
	}
}
