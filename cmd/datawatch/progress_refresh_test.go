// progress_refresh_test.go — the executor's periodic poll half of the loop
// rescue: refreshTaskProgress runs DetectLoop over the worker session log's
// recent turns. A looping log yields a structured finding (which the wait
// loop in main.go hands to Manager.RescueLoop); a log whose worker is
// legitimately acting yields none, so no rescue action is taken (no
// corrective sent, no stop/retry, no record, no event).

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/pilog"
	"github.com/dmz006/datawatch/internal/session"
)

// writePiLog writes a pi session log at <root>/sessions/--<project>--/
// <time>_<id>.jsonl for workDir, returning its path.
func writePiLogLines(t *testing.T, root, workDir string, started time.Time, lines ...string) string {
	t.Helper()
	dir := filepath.Join(root, "sessions", pilogProjectDir(workDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	name := started.UTC().Format("2006-01-02T15-04-05-000Z") + "_abcd.jsonl"
	p := filepath.Join(dir, name)
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func pilogProjectDir(workDir string) string {
	s := strings.TrimLeft(workDir, `/\`)
	s = strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(s)
	return "--" + s + "--"
}

func assistantText(text string) string {
	return `{"type":"message","id":"e1","parentId":null,"timestamp":"2026-10-27T14:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"}]}}`
}

func assistantTool(text, name, args string) string {
	return `{"type":"message","id":"e2","parentId":null,"timestamp":"2026-10-27T14:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"` + text + `"},{"type":"toolCall","id":"call_1","name":"` + name + `","arguments":` + args + `}]}}`
}

func TestRefreshTaskProgress_LoopingLogYieldsFinding(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	workDir := filepath.Join(t.TempDir(), "taskloop")
	started := time.Date(2026, 10, 27, 14, 0, 0, 0, time.UTC)
	writePiLogLines(t, root, workDir, started,
		assistantText(`Let me create the test file for the store.`),
		assistantText(`I will now write the implementation.`),
		assistantText(`Next, I'll create the remaining file.`),
	)

	s := &session.Session{ID: "abcd", FullID: "host-loop1", CreatedAt: started, ProjectDir: workDir}
	snap, warn, finding, looping := refreshTaskProgress(s, workDir, config.LoopDetectTurnsEffective(nil))

	if snap == nil {
		t.Fatal("refreshTaskProgress returned no snapshot for a readable pi log")
	}
	if !looping {
		t.Fatal("looping = false, want true for three announce-only turns")
	}
	if finding.Action == "" {
		t.Fatalf("finding = %+v, want the announced action named", finding)
	}
	if finding.Hint == "" {
		t.Fatalf("finding = %+v, want a hint about calling the write tool", finding)
	}
	if !strings.Contains(finding.Warning(), "looping") {
		t.Errorf("warning %q does not read as a looping warning", finding.Warning())
	}
	_ = warn // coarser streak warning: independent of the finding
}

func TestRefreshTaskProgress_ActioningLogYieldsNoFinding(t *testing.T) {
	// A worker that is actually acting is not looping: no finding, so the
	// wait loop takes no rescue action (no corrective sent, no stop/retry,
	// no record, no event).
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	workDir := filepath.Join(t.TempDir(), "taskact")
	started := time.Date(2026, 10, 27, 14, 0, 0, 0, time.UTC)
	writePiLogLines(t, root, workDir, started,
		assistantTool("Reading the file.", "read", `{"path":"internal/a.go"}`),
		assistantTool("Editing it.", "edit", `{"path":"internal/a.go"}`),
		assistantTool("Building it.", "bash", `{"command":"go build ./..."}`),
	)

	s := &session.Session{ID: "abcd", FullID: "host-act1", CreatedAt: started, ProjectDir: workDir}
	_, _, finding, looping := refreshTaskProgress(s, workDir, config.LoopDetectTurnsEffective(nil))
	if looping {
		t.Fatalf("looping = true for an acting worker: %+v", finding)
	}
}

func TestRefreshTaskProgress_DetectionOffYieldsNoFinding(t *testing.T) {
	// The poll hands the recent turns to autonomous.DetectLoop; with the
	// window off (n <= 0, DetectLoop's documented disable) even a looping
	// log yields no finding, so the wait loop takes no rescue action (no
	// corrective sent, no stop/retry, no record, no event).
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	workDir := filepath.Join(t.TempDir(), "taskoff")
	started := time.Date(2026, 10, 27, 14, 0, 0, 0, time.UTC)
	path := writePiLogLines(t, root, workDir, started,
		assistantText(`Let me create the test file.`),
		assistantText(`I will now write the implementation.`),
		assistantText(`Next, I'll create the remaining file.`),
	)

	turns, err := pilog.ReadTurns(path)
	if err != nil {
		t.Fatalf("ReadTurns: %v", err)
	}
	if len(turns) < 3 {
		t.Fatalf("got %d assistant turns, want 3 (fixture log)", len(turns))
	}
	recent := pilog.Recent(turns, 3)

	_, okOn := autonomous.DetectLoop(recent, 3) // same window the poll uses
	_, okOff := autonomous.DetectLoop(recent, 0)
	if !okOn {
		t.Fatal("with the poll's window (3) a looping log should yield a finding")
	}
	if okOff {
		t.Fatal("with the window off (0) no finding may fire — no rescue action")
	}
}

func TestRefreshTaskProgress_ZeroWindowDisablesDetection(t *testing.T) {
	// autonomous.loop_detect_turns = 0 → the poll passes 0 through and the
	// whole Detect half is skipped (not even the log turns are read), so a
	// looping log yields no finding and no rescue action fires.
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	workDir := filepath.Join(t.TempDir(), "taskzero")
	started := time.Date(2026, 10, 27, 14, 0, 0, 0, time.UTC)
	writePiLogLines(t, root, workDir, started,
		assistantText(`Let me create the test file.`),
		assistantText(`I will now write the implementation.`),
		assistantText(`Next, I'll create the remaining file.`),
	)

	s := &session.Session{ID: "abcd", FullID: "host-zero1", CreatedAt: started, ProjectDir: workDir}
	_, _, finding, looping := refreshTaskProgress(s, workDir, 0)
	if looping {
		t.Fatalf("looping = true with window 0: %+v", finding)
	}
	if finding.Action != "" {
		t.Fatalf("finding = %+v, want zero finding with window 0", finding)
	}
	// The snapshot itself must still be produced — 0 disables the detector
	// only, not progress visibility.
	snap, _, _, _ := refreshTaskProgress(s, workDir, 0)
	if snap == nil {
		t.Fatal("zero-window refresh must still return a progress snapshot")
	}
}

func TestLoopDetectTurnsEffective(t *testing.T) {
	if got := config.LoopDetectTurnsEffective(nil); got != 3 {
		t.Errorf("unset (nil) → %d, want default 3", got)
	}
	z := 0
	if got := config.LoopDetectTurnsEffective(&z); got != 0 {
		t.Errorf("0 → %d, want 0 (explicit off must not be treated as unset)", got)
	}
	n := 7
	if got := config.LoopDetectTurnsEffective(&n); got != 7 {
		t.Errorf("7 → %d, want 7", got)
	}
	neg := -2
	if err := config.ValidateLoopDetectTurns(&neg); err == nil {
		t.Error("negative must be rejected")
	}
	if err := config.ValidateLoopDetectTurns(&z); err != nil {
		t.Errorf("zero must be allowed: %v", err)
	}
	if err := config.ValidateLoopDetectTurns(nil); err != nil {
		t.Errorf("nil (unset) must be allowed: %v", err)
	}
}

func TestValidatePiThinkingLevel(t *testing.T) {
	for _, l := range []string{"", "off", "minimal", "low", "medium", "high"} {
		if err := config.ValidatePiThinkingLevel(l); err != nil {
			t.Errorf("%q must be accepted: %v", l, err)
		}
	}
	for _, l := range []string{"ultra", "OFF", " xhigh", "deep", "xhigh"} {
		if err := config.ValidatePiThinkingLevel(l); err == nil {
			t.Errorf("%q must be rejected", l)
		}
	}
}
