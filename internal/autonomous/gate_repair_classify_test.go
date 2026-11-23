package autonomous

// Tests for ClassifyGateFailure (gate_repair_classify.go). Each fixture under
// testdata/gate_repair/ is a self-contained failed-gate capture: a small
// "# " header carrying the GateFailureInput fields (exit code, signal-killed
// flag, script path, the story indices, the task's changed files and the
// script text) above a "---" separator, and the gate's output text below it.
// The fixture set covers one file per failure class plus one unrecognised
// output that must fall through to unknown.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gateRepairFixtureDir holds one .txt fixture per failure class.
const gateRepairFixtureDir = "testdata/gate_repair"

// loadGateRepairFixture parses one fixture file into its expected class and
// the GateFailureInput its header describes. Header lines are "# key: value"
// lines; the script text follows the "# script text:" line as "#   " lines;
// the gate output is everything below the first "---" separator.
func loadGateRepairFixture(t *testing.T, path string) (GateFailureClass, GateFailureInput) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}

	lines := strings.Split(string(raw), "\n")
	var header, body []string
	inBody := false
	for _, line := range lines {
		if !inBody && strings.TrimSpace(line) == "---" {
			inBody = true
			continue
		}
		if inBody {
			body = append(body, line)
			continue
		}
		header = append(header, line)
	}

	var (
		expectClass  string
		exitCode     int
		signalKilled bool
		scriptPath   string
		ownFolder    bool
		scriptStory  int
		gateStory    int
		changedFiles []string
		scriptLines  []string
	)
	inScriptText := false
	for _, line := range header {
		if strings.HasPrefix(line, "# script text:") {
			inScriptText = true
			continue
		}
		if inScriptText {
			if strings.TrimSpace(line) != "" {
				scriptLines = append(scriptLines, strings.TrimPrefix(line, "#   "))
			}
			continue
		}
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "# "), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "expect":
			expectClass = strings.TrimSpace(value)
		case "exit-code":
			exitCode, _ = strconv.Atoi(strings.TrimSpace(value))
		case "signal-killed":
			signalKilled = strings.TrimSpace(value) == "yes"
		case "script":
			scriptPath = strings.TrimSpace(value)
		case "own-verify-folder":
			ownFolder = strings.TrimSpace(value) == "yes"
		case "script-story":
			scriptStory, _ = strconv.Atoi(strings.TrimSpace(value))
		case "gate-story":
			gateStory, _ = strconv.Atoi(strings.TrimSpace(value))
		case "changed":
			if v := strings.TrimSpace(value); v != "" && v != "(none)" {
				changedFiles = append(changedFiles, strings.Fields(v)...)
			}
		}
	}
	if expectClass == "" {
		t.Fatalf("fixture %s: missing '# expect:' header line", path)
	}

	// Drop the trailing blank line left by the file's final newline.
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}

	in := GateFailureInput{
		Output:            strings.Join(body, "\n"),
		ExitCode:          exitCode,
		SignalKilled:      signalKilled,
		ScriptText:        strings.Join(scriptLines, "\n"),
		ScriptPath:        scriptPath,
		ChangedFiles:      changedFiles,
		InOwnVerifyFolder: ownFolder,
		ScriptStoryIndex:  scriptStory,
		GateStoryIndex:    gateStory,
	}
	return GateFailureClass(expectClass), in
}

// TestGateRepairClassifyFixtures loads every fixture under testdata/
// gate_repair/ and asserts ClassifyGateFailure returns the class the fixture
// expects: one file per failure class, plus the unrecognised output that must
// classify as unknown.
func TestGateRepairClassifyFixtures(t *testing.T) {
	entries, err := os.ReadDir(gateRepairFixtureDir)
	if err != nil {
		t.Fatalf("reading fixture dir %s: %v", gateRepairFixtureDir, err)
	}

	type caseDef struct {
		name string
		path string
		in   GateFailureInput
		want GateFailureClass
	}
	var cases []caseDef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		path := filepath.Join(gateRepairFixtureDir, e.Name())
		want, in := loadGateRepairFixture(t, path)
		cases = append(cases, caseDef{name: e.Name(), path: path, in: in, want: want})
	}
	if len(cases) < 7 { // six classes + the unrecognised output
		t.Fatalf("expected at least 7 fixtures, got %d", len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyGateFailure(tc.in); got != tc.want {
				t.Fatalf("ClassifyGateFailure = %q, want %q\ninput: %+v", got, tc.want, tc.in)
			}
		})
	}
}

// TestGateRepairClassifyFailureModes feeds ClassifyGateFailure outputs that
// must not be classified: a genuine failing-check output with no class
// signature, and an empty input. Both must come back unknown.
func TestGateRepairClassifyFailureModes(t *testing.T) {
	cases := []struct {
		name string
		in   GateFailureInput
	}{
		{
			name: "unrecognised_failing_check",
			in: GateFailureInput{
				Output:            "--- FAIL: TestScope (0.01s)\n    scope_test.go:12: got 403, want 200\nFAIL\nFAIL\tgithub.com/dmz006/datawatch/internal/autonomous\t1.203s\nFAIL",
				ExitCode:          1,
				ScriptText:        "set -e\ngo test ./internal/autonomous/ -count=1",
				ScriptPath:        ".datawatch/verify/story1-tests.sh",
				ChangedFiles:      []string{"internal/autonomous/scope_test.go"},
				InOwnVerifyFolder: true,
			},
		},
		{
			name: "empty_input",
			in:   GateFailureInput{},
		},
		{
			// A tracker-ID leak check whose reported matches are genuine
			// tracker IDs (not hex colors) is a real leak, not a hex
			// false-positive: it must stay unknown.
			name: "real_tracker_id_not_hex",
			in: GateFailureInput{
				Output: `42: "fixed in B440; see B441 for the follow-up",
43: "regression from F12",
✗ FAIL: 2 internal-ref leak(s) in user-facing surface.`,
				ExitCode:   1,
				ScriptText: "grep -nE '\\b(BL[0-9]+|F[0-9]+|B[0-9]+|S[0-9]+)\\b' locales/en.json && exit 1",
				ScriptPath: ".datawatch/verify/story1-no-internal-refs.sh",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyGateFailure(tc.in); got != GateFailureUnknown {
				t.Fatalf("ClassifyGateFailure = %q, want %q", got, GateFailureUnknown)
			}
		})
	}
}

// TestGateRepairClassifyDescriptions asserts every failure class carries a
// non-empty, distinct proposed-repair description, and that an unknown class
// value falls back to the unknown repair.
func TestGateRepairClassifyDescriptions(t *testing.T) {
	classes := []GateFailureClass{
		GateFailureLaterStoryScript,
		GateFailureDocsMirrorStale,
		GateFailureAllowedListRejects,
		GateFailureTrackerIDCSSHex,
		GateFailurePipeSigpipe,
		GateFailureRestartKilled,
		GateFailureUnknown,
	}
	seen := make(map[string]GateFailureClass, len(classes))
	for _, c := range classes {
		repair := c.Repair()
		if strings.TrimSpace(repair) == "" {
			t.Errorf("%s: empty repair description", c)
		}
		if prev, dup := seen[repair]; dup {
			t.Errorf("%s: repair description duplicates %s", c, prev)
		}
		seen[repair] = c
	}
	if got, want := GateFailureClass("no-such-class").Repair(), GateFailureUnknown.Repair(); got != want {
		t.Errorf("unrecognized class repair = %q, want the unknown-class repair %q", got, want)
	}
	// Sanity: the description map is exactly the known class set.
	if len(GateRepairDescriptions) != len(classes) {
		t.Errorf("GateRepairDescriptions has %d entries, want %d", len(GateRepairDescriptions), len(classes))
	}
}

// TestGateRepairClassifyOrdering documents the most-specific-wins order with
// a synthetic output that carries two class signatures at once: a later-story
// script whose output also looks like a "not found" bashism must classify as
// the structural fact (later_story_script), not the weaker output shape.
func TestGateRepairClassifyOrdering(t *testing.T) {
	in := GateFailureInput{
		Output:            "sh: .datawatch/verify/story2-scope.sh: not found",
		ExitCode:          127,
		ScriptPath:        ".datawatch/verify/story2-scope.sh",
		InOwnVerifyFolder: true,
		ScriptStoryIndex:  2,
		GateStoryIndex:    0,
	}
	if got, want := ClassifyGateFailure(in), GateFailureLaterStoryScript; got != want {
		t.Fatalf("ClassifyGateFailure = %q, want %q (the story-ordering fact must win over the 'not found' bashism)", got, want)
	}

	// The same output at the gate's own story is not a later-story script; the
	// "not found" line then stands on its own as the bashism signature, so it
	// classifies as pipe_sigpipe.
	in.ScriptStoryIndex = 0
	if got, want := ClassifyGateFailure(in), GateFailurePipeSigpipe; got != want {
		t.Fatalf("ClassifyGateFailure = %q, want %q (at the gate's own story the 'not found' bashism is the only signature)", got, want)
	}

	// And an output with no signature at all at the gate's own story falls
	// through to unknown.
	in.Output = "command exited with code 2"
	if got, want := ClassifyGateFailure(in), GateFailureUnknown; got != want {
		t.Fatalf("ClassifyGateFailure = %q, want %q", got, want)
	}
}
