package pilog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const samplePath = "../../testdata/pi/session-sample.jsonl"

func TestReadSample(t *testing.T) {
	p, err := Read(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	if p.AssistantTurns != 5 {
		t.Errorf("AssistantTurns = %d, want 5", p.AssistantTurns)
	}
	want := map[string]int{"bash": 3, "read": 1, "edit": 1}
	if len(p.ToolCalls) != len(want) {
		t.Errorf("ToolCalls = %v, want %v", p.ToolCalls, want)
	}
	for name, n := range want {
		if p.ToolCalls[name] != n {
			t.Errorf("ToolCalls[%q] = %d, want %d", name, p.ToolCalls[name], n)
		}
	}
	if p.LastToolName != "bash" || p.LastToolArgs != `{"command":"go vet ./..."}` {
		t.Errorf("last tool = %q %q", p.LastToolName, p.LastToolArgs)
	}
	if want := "Added the Version constant to version.go; go test and go vet both pass."; p.LastSummary != want {
		t.Errorf("LastSummary = %q, want %q", p.LastSummary, want)
	}
	if p.Tokens != 908+1054+1173+1252+1293 {
		t.Errorf("Tokens = %d", p.Tokens)
	}
	if want := time.Date(2026, 10, 27, 14, 0, 40, 0, time.UTC); !p.LastEventAt.Equal(want) {
		t.Errorf("LastEventAt = %s, want %s", p.LastEventAt, want)
	}
}

func writeLog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assistantLine(blocks string) string {
	return `{"type":"message","timestamp":"2026-10-27T14:00:00.000Z","message":{"role":"assistant","content":[` + blocks + `]}}` + "\n"
}

func TestSummaryTruncatedTo200Runes(t *testing.T) {
	long := strings.Repeat("é", 300) // multi-byte: a byte cut would split a rune
	p, err := Read(writeLog(t, assistantLine(`{"type":"text","text":"`+long+`"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if got := []rune(p.LastSummary); len(got) != 200 || string(got) != strings.Repeat("é", 200) {
		t.Errorf("LastSummary has %d runes, want 200 of é", len(got))
	}
	if p.Tokens != 0 {
		t.Errorf("Tokens = %d, want 0 without usage", p.Tokens)
	}

	short := "short thought"
	p, err = Read(writeLog(t, assistantLine(`{"type":"thinking","thinking":"`+short+`"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if p.LastSummary != short {
		t.Errorf("LastSummary = %q, want %q", p.LastSummary, short)
	}
}

func TestPartialLastLineTolerated(t *testing.T) {
	good := assistantLine(`{"type":"text","text":"done"},{"type":"toolCall","id":"c1","name":"read","arguments":{"path":"a.go"}}`)
	partial := `{"type":"message","timestamp":"2026-10-27T14:05:00.000Z","message":{"role":"assistant","content":[{"type":"te`
	p, err := Read(writeLog(t, good+partial))
	if err != nil {
		t.Fatal(err)
	}
	if p.AssistantTurns != 1 || p.ToolCalls["read"] != 1 || p.LastSummary != "done" {
		t.Errorf("progress = %+v", p)
	}
	if want := time.Date(2026, 10, 27, 14, 0, 0, 0, time.UTC); !p.LastEventAt.Equal(want) {
		t.Errorf("LastEventAt = %s, want %s (partial line must not count)", p.LastEventAt, want)
	}
}

func TestUnknownEventsIgnored(t *testing.T) {
	log := `{"type":"brand_new","timestamp":"2026-10-27T15:00:00Z","x":[1,2]}` + "\n" +
		`{"type":"message","message":{"role":"assistant","content":"plain string content"}}` + "\n"
	p, err := Read(writeLog(t, log))
	if err != nil {
		t.Fatal(err)
	}
	if p.AssistantTurns != 1 || len(p.ToolCalls) != 0 {
		t.Errorf("progress = %+v", p)
	}
	if want := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC); !p.LastEventAt.Equal(want) {
		t.Errorf("LastEventAt = %s", p.LastEventAt)
	}
}

func TestReadMissingFile(t *testing.T) {
	if _, err := Read(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Error("want error for missing file")
	}
}

func TestProjectDirName(t *testing.T) {
	if got, want := ProjectDirName("/home/dmz/work/demo"), "--home-dmz-work-demo--"; got != want {
		t.Errorf("ProjectDirName = %q, want %q", got, want)
	}
}

func TestLocateLogPicksClosestAtOrAfterStart(t *testing.T) {
	root := t.TempDir()
	workDir := "/home/dmz/work/demo"
	dir := filepath.Join(root, "--home-dmz-work-demo--")
	other := filepath.Join(root, "--home-dmz-work-other--")
	for _, d := range []string{dir, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(dir, "2026-10-27T13-00-00-000Z_aaaa.jsonl"):   "before",
		filepath.Join(dir, "2026-10-27T14-00-05-250Z_bbbb.jsonl"):   "match",
		filepath.Join(dir, "2026-10-27T14-30-00-000Z_cccc.jsonl"):   "later",
		filepath.Join(dir, "notes.txt"):                             "ignored",
		filepath.Join(other, "2026-10-27T14-00-01-000Z_dddd.jsonl"): "other project",
	}
	for p, c := range files {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	started := time.Date(2026, 10, 27, 14, 0, 0, 0, time.UTC)
	got, err := LocateLog(root, workDir, started)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "2026-10-27T14-00-05-250Z_bbbb.jsonl"); got != want {
		t.Errorf("LocateLog = %s, want %s", got, want)
	}

	if _, err := LocateLog(root, workDir, started.Add(time.Hour)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("want not-exist error when nothing is at/after start, got %v", err)
	}
	if _, err := LocateLog(root, "/no/such/dir", started); err == nil {
		t.Error("want error for unknown project")
	}
}
