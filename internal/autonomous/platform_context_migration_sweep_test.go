package autonomous

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

// pcBlock renders the marked platform context block the same way the daemon
// appends it (begin marker for the current version, the fixed text, end
// marker), for rules-file fixtures.
func pcBlock() string {
	return "<!-- datawatch:platform-context v" + strconv.Itoa(platformcontext.PlatformContextVersion) + " -->\n" +
		platformcontext.PlatformContext + "\n<!-- /datawatch:platform-context -->\n"
}

// pcHarness is one Manager plus an audit capture for the migration sweep.
type pcHarness struct {
	m       *Manager
	mu      sync.Mutex
	actions []string
	details []map[string]any
}

func newPCHarness(t *testing.T) *pcHarness {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	h := &pcHarness{m: m}
	m.SetAuditFn(func(a string, d map[string]any) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.actions = append(h.actions, a)
		h.details = append(h.details, d)
	})
	return h
}

// addAutomation stores one draft Automation whose project directory is dir
// ("" = no project dir) and returns it.
func (h *pcHarness) addAutomation(t *testing.T, name, dir string) *Automation {
	t.Helper()
	a, err := h.m.CreateAutomation(AutomationInput{Name: name, ProjectDir: dir}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	return a
}

func (h *pcHarness) actionsFor(action string) []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []map[string]any
	for i, a := range h.actions {
		if a == action {
			out = append(out, h.details[i])
		}
	}
	return out
}

func pcWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func pcReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPlatformContextMigrationSweepCleansAgentMdAndRecordsOneEvent(t *testing.T) {
	h := newPCHarness(t)
	dir := t.TempDir()
	base := "# Rules\n\nuser line one\nuser line two\n"
	agent := filepath.Join(dir, "AGENT.md")
	pcWriteFile(t, agent, base+"\n"+pcBlock())
	h.addAutomation(t, "sweep project", dir)

	h.m.sweepPlatformContextMigration()

	if got := pcReadFile(t, agent); got != base {
		t.Fatalf("AGENT.md after sweep = %q\nwant the block-free content %q", got, base)
	}
	events := h.actionsFor(platformContextMigratedAction)
	if len(events) != 1 {
		t.Fatalf("recorded %d %s events, want exactly 1 (got %v)", len(events), platformContextMigratedAction, h.actions)
	}
	ev := events[0]
	if ev["path"] != agent {
		t.Errorf("event path = %v, want %s", ev["path"], agent)
	}
	if ev["committed"] != false {
		t.Errorf("event committed = %v, want false (the removal is not committed)", ev["committed"])
	}
	if note, _ := ev["note"].(string); !strings.Contains(note, agent) {
		t.Errorf("event note %q does not name the cleaned file", note)
	}
}

func TestPlatformContextMigrationSweepSecondPassRecordsNothing(t *testing.T) {
	h := newPCHarness(t)
	dir := t.TempDir()
	base := "# Rules\n\nuser line\n"
	agent := filepath.Join(dir, "AGENT.md")
	pcWriteFile(t, agent, base+"\n"+pcBlock())
	other := filepath.Join(dir, "CLAUDE.md")
	pcWriteFile(t, other, "# Claude\n\nother line\n"+pcBlock())
	h.addAutomation(t, "sweep project", dir)

	h.m.sweepPlatformContextMigration()
	if got := len(h.actionsFor(platformContextMigratedAction)); got != 2 {
		t.Fatalf("first pass recorded %d events, want one per cleaned file (2)", got)
	}
	cleanAfter := pcReadFile(t, agent)

	h.m.sweepPlatformContextMigration()
	if got := len(h.actionsFor(platformContextMigratedAction)); got != 2 {
		t.Fatalf("second pass recorded new events: total %d, want still 2 (no change)", got)
	}
	if got := pcReadFile(t, agent); got != cleanAfter {
		t.Fatalf("second pass rewrote AGENT.md: %q", got)
	}
}

func TestPlatformContextMigrationSweepNoBlockRecordsNothing(t *testing.T) {
	h := newPCHarness(t)
	dir := t.TempDir()
	agent := filepath.Join(dir, "AGENT.md")
	agentContent := "# Rules\n\nuser line one\nuser line two\n"
	pcWriteFile(t, agent, agentContent)
	other := filepath.Join(dir, "CLAUDE.md")
	otherContent := "# Claude\n\nno platform block here\n"
	pcWriteFile(t, other, otherContent)
	h.addAutomation(t, "clean project", dir)

	h.m.sweepPlatformContextMigration()

	if got := len(h.actionsFor(platformContextMigratedAction)); got != 0 {
		t.Fatalf("recorded %d events for block-free files, want 0", got)
	}
	if got := pcReadFile(t, agent); got != agentContent {
		t.Fatalf("block-free AGENT.md rewritten: %q", got)
	}
	if got := pcReadFile(t, other); got != otherContent {
		t.Fatalf("block-free CLAUDE.md rewritten: %q", got)
	}
}

func TestPlatformContextMigrationSweepMissingDirectoryDoesNotFail(t *testing.T) {
	h := newPCHarness(t)
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")
	h.addAutomation(t, "lost project", missing)

	// Must not panic, must not record, and must not affect other projects.
	dir := t.TempDir()
	base := "# Rules\n\nuser line\n"
	agent := filepath.Join(dir, "AGENT.md")
	pcWriteFile(t, agent, base+"\n"+pcBlock())
	h.addAutomation(t, "real project", dir)

	h.m.sweepPlatformContextMigration()

	if got := pcReadFile(t, agent); got != base {
		t.Fatalf("real project AGENT.md not cleaned: %q", got)
	}
	if got := len(h.actionsFor(platformContextMigratedAction)); got != 1 {
		t.Fatalf("recorded %d events, want 1 (the real project's file only)", got)
	}
}

func TestPlatformContextMigrationSweepDedupesProjectDirsAndIgnoresBlank(t *testing.T) {
	h := newPCHarness(t)
	dir := t.TempDir()
	base := "# Rules\n\nuser line\n"
	agent := filepath.Join(dir, "AGENT.md")
	pcWriteFile(t, agent, base+"\n"+pcBlock())
	h.addAutomation(t, "first", dir)
	h.addAutomation(t, "second", dir) // same directory again
	h.addAutomation(t, "blank", "")   // no project directory

	h.m.sweepPlatformContextMigration()

	if got := len(h.actionsFor(platformContextMigratedAction)); got != 1 {
		t.Fatalf("recorded %d events for one cleaned file across duplicate dirs, want 1", got)
	}
}
