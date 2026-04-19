// F10 sprint 8 (S8.4) — agent audit trail tests.

package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

func TestMemoryAuditor_AppendAndAll(t *testing.T) {
	a := NewMemoryAuditor()
	a.Append(AuditEvent{Event: "spawn", AgentID: "a1"})
	a.Append(AuditEvent{Event: "terminate", AgentID: "a1"})
	got := a.All()
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].Event != "spawn" || got[1].Event != "terminate" {
		t.Errorf("order/content wrong: %+v", got)
	}
}

func TestMemoryAuditor_RecentN(t *testing.T) {
	a := NewMemoryAuditor()
	for i := 0; i < 5; i++ {
		a.Append(AuditEvent{Event: "x", Note: string(rune('a' + i))})
	}
	rec := a.Recent(3)
	if len(rec) != 3 {
		t.Fatalf("len=%d want 3", len(rec))
	}
	// Last 3 should be c, d, e.
	if rec[0].Note != "c" || rec[2].Note != "e" {
		t.Errorf("recent slice wrong: %+v", rec)
	}
	if got := a.Recent(0); len(got) != 5 {
		t.Errorf("recent(0) should return all: %d", len(got))
	}
	if got := a.Recent(100); len(got) != 5 {
		t.Errorf("recent(>n) should return all: %d", len(got))
	}
}

func TestFileAuditor_WritesValidJSONLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	a, err := NewFileAuditor(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Append(AuditEvent{Event: "spawn", AgentID: "x", Project: "p"})
	a.Append(AuditEvent{Event: "terminate", AgentID: "x"})
	a.Close()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines=%d want 2: %s", len(lines), body)
	}
	for _, line := range lines {
		var ev AuditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("invalid JSON: %q (%v)", line, err)
		}
	}
}

func TestFileAuditor_DefaultsAndAppendMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deep", "nested", "audit.jsonl")
	a, err := NewFileAuditor(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Append(AuditEvent{Event: "first"})
	a.Close()

	// Reopen + append second line.
	a2, err := NewFileAuditor(path)
	if err != nil {
		t.Fatal(err)
	}
	a2.Append(AuditEvent{Event: "second"})
	a2.Close()

	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `"first"`) || !strings.Contains(string(body), `"second"`) {
		t.Errorf("append mode lost content: %s", body)
	}
}

func TestFileAuditor_RejectsEmptyPath(t *testing.T) {
	if _, err := NewFileAuditor(""); err == nil {
		t.Error("expected error for empty path")
	}
}

// emit() with nil auditor is a no-op (no panic).
func TestEmit_NilAuditorSafe(t *testing.T) {
	emit(nil, "x", "y", "z", "w", "s", "n", nil)
}

// Manager wires the auditor: Spawn / Terminate / RecordResult /
// Spawn-fail all emit one event.
func TestManager_AuditEmissions(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	m := NewManager(ps, cs)
	m.RegisterDriver(&fakeDriver{kind: "docker"})
	aud := NewMemoryAuditor()
	m.Auditor = aud

	a, err := m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RecordResult(a.ID, &AgentResult{Status: "ok", Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Terminate(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	got := aud.All()
	if len(got) != 3 {
		t.Fatalf("emissions=%d want 3 (spawn, result, terminate); events=%+v", len(got), got)
	}
	wantEvents := []string{"spawn", "result", "terminate"}
	for i, want := range wantEvents {
		if got[i].Event != want {
			t.Errorf("event[%d]=%q want %q", i, got[i].Event, want)
		}
		if got[i].AgentID != a.ID {
			t.Errorf("event[%d] agent_id=%q want %q", i, got[i].AgentID, a.ID)
		}
	}
}

// Spawn-fail also emits an event tagged spawn_fail.
func TestManager_AuditSpawnFail(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	m := NewManager(ps, cs)
	d := &fakeDriver{kind: "docker", spawnErr: errors.New("boom")}
	m.RegisterDriver(d)
	aud := NewMemoryAuditor()
	m.Auditor = aud

	_, _ = m.Spawn(context.Background(), SpawnRequest{ProjectProfile: "p", ClusterProfile: "c"})
	got := aud.All()
	found := false
	for _, ev := range got {
		if ev.Event == "spawn_fail" && strings.Contains(ev.Note, "boom") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("spawn_fail event missing or wrong note: %+v", got)
	}
}

// MemoryAuditor is concurrent-safe.
func TestMemoryAuditor_ConcurrentAppend(t *testing.T) {
	a := NewMemoryAuditor()
	var wg sync.WaitGroup
	const N = 100
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.Append(AuditEvent{Event: "x"})
		}()
	}
	wg.Wait()
	if got := len(a.All()); got != N {
		t.Errorf("count=%d want %d", got, N)
	}
}

// ── F10 S8.4 — CEF format ────────────────────────────────────────────

func TestFormatCEFLine_Basic(t *testing.T) {
	ev := AuditEvent{
		Event: "spawn", AgentID: "abc", Project: "p", Cluster: "c",
		State: "starting", Note: "ok",
	}
	got := FormatCEFLine(ev)
	if !strings.HasPrefix(got, "CEF:0|datawatch|datawatch|") {
		t.Errorf("CEF header wrong: %s", got)
	}
	for _, want := range []string{
		"|100|AgentSpawn|3|", // signature + name + severity for spawn
		"deviceCustomString1=spawn",
		"duser=abc",
		"deviceCustomString2=p",
		"deviceCustomString3=c",
		"deviceCustomString4=starting",
		"msg=ok",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("CEF missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatCEFLine_SeverityMapping(t *testing.T) {
	cases := map[string]string{
		"spawn":      "|100|AgentSpawn|3|",
		"spawn_fail": "|101|AgentSpawnFailure|6|",
		"terminate":  "|110|AgentTerminate|3|",
		"result":     "|120|AgentResult|3|",
		"bootstrap":  "|130|AgentBootstrap|4|",
		"revoke":     "|200|TokenRevoke|4|",
		"sweep":      "|210|TokenSweep|3|",
		"unknown":    "|0|AgentEvent|3|",
	}
	for event, wantHeader := range cases {
		t.Run(event, func(t *testing.T) {
			got := FormatCEFLine(AuditEvent{Event: event})
			if !strings.Contains(got, wantHeader) {
				t.Errorf("CEF for %q missing %q in:\n%s", event, wantHeader, got)
			}
		})
	}
}

// CEF spec: header escapes `|` and `\`; extension escapes `=`, `\`,
// `\n`, `\r`. Pipes are NOT special in extension fields.
// Security-relevant — bad escapes break SIEM parsing or let an
// attacker inject synthetic events.
func TestFormatCEFLine_Escapes(t *testing.T) {
	// Extension field: `=` + `\` + `\n` are escaped; `|` is allowed bare.
	ev := AuditEvent{
		Event:   "result",
		AgentID: `agent\with|pipes`,
		Note:    "line1\nline2=value",
	}
	got := FormatCEFLine(ev)
	if !strings.Contains(got, `duser=agent\\with|pipes`) {
		t.Errorf("extension backslash escape missing: %s", got)
	}
	if !strings.Contains(got, `line1\nline2\=value`) {
		t.Errorf("extension newline + equals escape missing: %s", got)
	}
}

// Header escapes (pipes) — the version string lands in the header
// position, so swap cefVersion temporarily to one with a pipe.
func TestFormatCEFLine_HeaderEscapes(t *testing.T) {
	saved := cefVersion
	cefVersion = func() string { return `v|x\y` }
	defer func() { cefVersion = saved }()
	got := FormatCEFLine(AuditEvent{Event: "spawn"})
	if !strings.Contains(got, `|v\|x\\y|`) {
		t.Errorf("header pipe + backslash escapes missing: %s", got)
	}
}

// FileAuditor with CEF format produces CEF lines in the file.
func TestFileAuditor_CEFFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.cef")
	a, err := NewFileAuditorWithFormat(path, FormatCEF)
	if err != nil {
		t.Fatal(err)
	}
	a.Append(AuditEvent{Event: "spawn", AgentID: "x", Project: "p"})
	a.Close()
	body, _ := os.ReadFile(path)
	line := strings.TrimSpace(string(body))
	if !strings.HasPrefix(line, "CEF:0|datawatch|datawatch|") {
		t.Errorf("file content not CEF: %s", line)
	}
	if !strings.Contains(line, "duser=x") {
		t.Errorf("CEF missing duser: %s", line)
	}
}
