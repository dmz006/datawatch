package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pilog"
)

// sampleSessionLog is a synthetic pi session transcript: one user prompt, two
// assistant messages (the first with a thinking + text + toolCall, the second
// with a single toolCall and usage). It exercises every field pilog.Read
// condenses, including one partially-truncated trailing line that Read must
// skip without error.
const sampleSessionLog = `{"type":"message","timestamp":"2026-02-11T10:00:20Z","message":{"role":"user","content":"do the thing"}}
{"type":"message","timestamp":"2026-02-11T10:05:10Z","message":{"role":"assistant","usage":{"input":100,"output":25,"totalTokens":125},"content":[{"type":"thinking","thinking":"plan it first"},{"type":"text","text":"Writing hello.txt"},{"type":"toolCall","name":"write","arguments":{"path":"hello.txt","content":"hi"}}]}}
{"type":"message","timestamp":"2026-02-11T10:06:41Z","message":{"role":"assistant","usage":{"input":90,"output":10,"totalTokens":100},"content":[{"type":"toolCall","name":"bash","arguments":{"command":"ls -la"}}]}}
{"type":"message","timestamp":"2026-02-11T1` // truncated mid-line: Read must tolerate it

// writeSampleLog writes sampleSessionLog under a pi-style log file name
// (so LocateLog can find it when given a matching workdir + start time).
func writeSampleLog(t *testing.T, sessionsRoot, workDir string) string {
	t.Helper()
	dir := filepath.Join(sessionsRoot, pilog.ProjectDirName(workDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(dir, "2026-02-11T10-00-00-000Z_progtest.jsonl")
	if err := os.WriteFile(p, []byte(sampleSessionLog), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	return p
}

// TestFromPilogSampleLog pins the mapping from a parsed sample log to the
// TaskProgress snapshot: RFC3339 last_event_at, turn/tool counts, the last
// action, and cumulative tokens.
func TestFromPilogSampleLog(t *testing.T) {
	t.Parallel()
	path := writeSampleLog(t, t.TempDir(), t.TempDir())

	prog, err := pilog.Read(path)
	if err != nil {
		t.Fatalf("pilog.Read: %v", err)
	}
	wantAt := time.Date(2026, 2, 11, 10, 6, 41, 0, time.UTC)
	if !prog.LastEventAt.Equal(wantAt) {
		t.Errorf("LastEventAt = %v, want %v", prog.LastEventAt, wantAt)
	}
	if prog.AssistantTurns != 2 {
		t.Errorf("AssistantTurns = %d, want 2", prog.AssistantTurns)
	}
	if got := prog.ToolCalls["bash"]; got != 1 {
		t.Errorf(`ToolCalls["bash"] = %d, want 1`, got)
	}
	if got := prog.ToolCalls["write"]; got != 1 {
		t.Errorf(`ToolCalls["write"] = %d, want 1`, got)
	}
	if prog.LastToolName != "bash" {
		t.Errorf("LastToolName = %q, want bash", prog.LastToolName)
	}
	if prog.LastSummary != "Writing hello.txt" {
		t.Errorf("LastSummary = %q, want %q", prog.LastSummary, "Writing hello.txt")
	}
	if prog.Tokens != 225 {
		t.Errorf("Tokens = %d, want 225", prog.Tokens)
	}

	snap := FromPilog(prog)
	if snap.LastEventAt != "2026-02-11T10:06:41Z" {
		t.Errorf("snap.LastEventAt = %q, want RFC3339 2026-02-11T10:06:41Z", snap.LastEventAt)
	}
	if snap.Tokens != 225 {
		t.Errorf("snap.Tokens = %d, want 225", snap.Tokens)
	}
}

// TestTaskProgressJSONShape pins the wire shape a task carries: progress and
// looping_warning are present (and correctly filled) for a live task, and
// omitted from the JSON entirely for a task that is not running — the
// omitempty keys make absence the signal, per spec.
func TestTaskProgressJSONShape(t *testing.T) {
	t.Parallel()
	snap := &TaskProgress{
		LastEventAt:    "2026-02-11T10:06:41Z",
		AssistantTurns: 2,
		ToolCalls:      map[string]int{"bash": 1, "write": 1},
		LastToolName:   "bash",
		LastSummary:    "Writing hello.txt",
		Tokens:         225,
	}

	running := Task{ID: "t1", Title: "running", Status: TaskInProgress,
		Progress: snap, LoopingWarning: "suspected looping: same call x5"}
	b, err := json.Marshal(running)
	if err != nil {
		t.Fatalf("marshal running task: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"progress"`,
		`"last_event_at":"2026-02-11T10:06:41Z"`,
		`"assistant_turns":2`,
		`"tool_calls"`,
		`"last_tool_name":"bash"`,
		`"last_summary":"Writing hello.txt"`,
		`"tokens":225`,
		`"looping_warning":"suspected looping: same call x5"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("running task JSON missing %s (got %s)", want, s)
		}
	}
	var round Task
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Progress == nil || round.Progress.Tokens != 225 {
		t.Errorf("round-trip Progress = %+v, want non-nil with Tokens 225", round.Progress)
	}

	pending := Task{ID: "t2", Title: "pending", Status: TaskPending}
	b2, err := json.Marshal(pending)
	if err != nil {
		t.Fatalf("marshal pending task: %v", err)
	}
	if strings.Contains(string(b2), `"progress"`) {
		t.Errorf("pending task JSON must omit the progress key: %s", b2)
	}
	if strings.Contains(string(b2), `"looping_warning"`) {
		t.Errorf("pending task JSON must omit looping_warning: %s", b2)
	}
}

// TestStorePersistsProgressOnlyWhileActive is the core store invariant: the
// snapshot is written to prds.jsonl while the task is in progress, survives a
// store reload, and is forced off (in memory and on disk) the moment the task
// leaves the active statuses — no matter which save path lands the change.
func TestStorePersistsProgressOnlyWhileActive(t *testing.T) {
	dataDir := t.TempDir()
	mgr, err := NewManager(dataDir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []Story{{
		ID: "sp1", Title: "Story",
		Tasks: []Task{
			{ID: "pt1", Title: "running", Status: TaskInProgress},
			{ID: "pt2", Title: "waiting", Status: TaskPending},
		},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}

	// Feed the in-progress task a snapshot built from a real (sample) log.
	path := writeSampleLog(t, t.TempDir(), t.TempDir())
	prog, err := pilog.Read(path)
	if err != nil {
		t.Fatalf("pilog.Read: %v", err)
	}
	snap := FromPilog(prog)
	if !mgr.RefreshTaskProgress("pt1", snap, "suspected looping: same call x5") {
		t.Fatalf("RefreshTaskProgress(pt1) = false, want true")
	}

	// A fresh store over the same data dir must still see the snapshot
	// (persist happened) because pt1 is in progress.
	mgr2, err := NewManager(dataDir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("second NewManager: %v", err)
	}
	tk, ok := mgr2.Store().GetTask("pt1")
	if !ok {
		t.Fatalf("GetTask(pt1) after reload = false")
	}
	if tk.Progress == nil || tk.Progress.Tokens != 225 {
		t.Fatalf("reloaded pt1.Progress = %+v, want non-nil with Tokens 225", tk.Progress)
	}
	if tk.LoopingWarning != "suspected looping: same call x5" {
		t.Errorf("reloaded pt1.LoopingWarning = %q", tk.LoopingWarning)
	}
	tk2, ok := mgr2.Store().GetTask("pt2")
	if !ok {
		t.Fatalf("GetTask(pt2) after reload = false")
	}
	if tk2.Progress != nil || tk2.LoopingWarning != "" {
		t.Errorf("reloaded pt2 (pending) must carry no progress: %+v %q", tk2.Progress, tk2.LoopingWarning)
	}

	raw, err := os.ReadFile(filepath.Join(dataDir, "autonomous", "prds.jsonl"))
	if err != nil {
		t.Fatalf("read prds.jsonl: %v", err)
	}
	if !strings.Contains(string(raw), `"looping_warning":"suspected looping: same call x5"`) {
		t.Errorf("prds.jsonl while in progress should persist looping_warning: %s", raw)
	}

	// Flip pt1 to a terminal status and save: the next persist must strip
	// the snapshot even though it is still on the in-memory object.
	p, ok := mgr2.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatalf("GetPRD after reload = false")
	}
	p.Story[0].Tasks[0].Status = TaskCompleted
	if err := mgr2.Store().SavePRD(p); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	mgr3, err := NewManager(dataDir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("third NewManager: %v", err)
	}
	tk, ok = mgr3.Store().GetTask("pt1")
	if !ok {
		t.Fatalf("GetTask(pt1) after status flip = false")
	}
	if tk.Progress != nil {
		t.Errorf("completed task must not carry Progress: %+v", tk.Progress)
	}
	if tk.LoopingWarning != "" {
		t.Errorf("completed task must not carry LoopingWarning: %q", tk.LoopingWarning)
	}
	raw2, err := os.ReadFile(filepath.Join(dataDir, "autonomous", "prds.jsonl"))
	if err != nil {
		t.Fatalf("read prds.jsonl: %v", err)
	}
	if strings.Contains(string(raw2), `"progress"`) {
		t.Errorf("prds.jsonl after status flip must contain no progress key: %s", raw2)
	}
}
