package autonomous

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// refreshManager returns a Manager with no coordinator, for refresh tests that
// only look at the registry.
func refreshManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// setPendingSource replaces the file registry settings of m.
func setPendingSource(m *Manager, src config.PendingItemSourceConfig) {
	cfg := m.Config()
	cfg.Automation.PendingItemSource = src
	m.SetConfig(cfg)
}

// writeRegistry writes content to a markdown file under a fresh temp dir and
// returns its path.
func writeRegistry(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "work-list.md")
	rewriteRegistry(t, path, content)
	return path
}

func rewriteRegistry(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

// tableOf builds a three column table (ref, title, status).
func tableOf(rows ...string) string {
	return "# Work\n\n| Ref | Title | Status |\n|---|---|---|\n" + strings.Join(rows, "\n") + "\n"
}

func fileItems(m *Manager) []*PendingItem {
	return m.pendingItems.List(PendingItemFilter{Source: PendingItemSourceFile})
}

func itemByRef(t *testing.T, m *Manager, ref string) *PendingItem {
	t.Helper()
	p, ok := m.pendingItems.FindByRef(PendingItemSourceFile, ref)
	if !ok {
		t.Fatalf("no file item with ref %q", ref)
	}
	return p
}

func wantRefresh(t *testing.T, got PendingItemRefreshResult, want PendingItemRefreshResult) {
	t.Helper()
	got.At, got.Path = want.At, want.Path // the time and the path are not under test here
	if got != want {
		t.Fatalf("refresh result\n got  %+v\n want %+v", got, want)
	}
}

func TestPendingItemRefreshNoFileRegistryConfigured(t *testing.T) {
	m := refreshManager(t)
	internal := newPendingItem(t, m, "an internal item")
	before := m.pendingItems.List(PendingItemFilter{})

	res := m.RefreshPendingItems()
	if res.Configured || res.Error != "" || res.Path != "" || res.At.IsZero() {
		t.Fatalf("result %+v, want unconfigured with no error", res)
	}
	if res.Rows+res.Added+res.Updated+res.Done+res.Dropped+res.Kept+res.Skipped != 0 {
		t.Fatalf("counts should all be zero: %+v", res)
	}
	if after := m.pendingItems.List(PendingItemFilter{}); !reflect.DeepEqual(before, after) {
		t.Fatalf("registry changed with no file registry configured")
	}
	if got, _ := m.GetPendingItem(internal.ID); got.State != PendingItemWaiting {
		t.Fatalf("internal item %+v", got)
	}
	last, ok := m.LastPendingItemRefresh()
	if !ok || last.Configured {
		t.Fatalf("last result %+v ok=%v", last, ok)
	}
}

func TestPendingItemRefreshDifferentIDPattern(t *testing.T) {
	m := refreshManager(t)
	path := writeRegistry(t, "| Summary | Key | Owner |\n|---|---|---|\n"+
		"| Build the importer | X-12 | a |\n"+
		"| Write the guide | X-7 | b |\n"+
		"| Not an item | note | c |\n")
	setPendingSource(m, config.PendingItemSourceConfig{
		Path: path, IDPattern: `^X-\d+$`, IDColumn: "key", TitleColumn: "SUMMARY",
	})
	res := m.RefreshPendingItems()
	if res.Error != "" {
		t.Fatalf("refresh error: %s", res.Error)
	}
	if res.Rows != 2 || res.Added != 2 || res.Skipped != 1 {
		t.Fatalf("result %+v, want 2 rows, 2 added, 1 skipped", res)
	}
	if got := itemByRef(t, m, "X-12"); got.Title != "Build the importer" || got.State != PendingItemWaiting || got.Source != PendingItemSourceFile {
		t.Fatalf("item %+v", got)
	}
	if _, ok := m.pendingItems.FindByRef(PendingItemSourceFile, "note"); ok {
		t.Fatal("a row whose id does not match the pattern became an item")
	}

	// A pattern that does not compile records the error and changes nothing.
	before := fileItems(m)
	setPendingSource(m, config.PendingItemSourceConfig{Path: path, IDPattern: `X-[`, IDColumn: "key", TitleColumn: "summary"})
	bad := m.RefreshPendingItems()
	if !bad.Configured || bad.Error == "" || !strings.Contains(bad.Error, "id pattern") {
		t.Fatalf("bad pattern result %+v", bad)
	}
	if after := fileItems(m); !reflect.DeepEqual(before, after) {
		t.Fatal("registry changed after an invalid id pattern")
	}
}

func TestPendingItemRefreshAddsRows(t *testing.T) {
	m := refreshManager(t)
	path := writeRegistry(t, tableOf("| A-1 | First piece | open |", "| A-2 | [Second](http://example.com/2) | open |", "| A-1 | Duplicate row | open |"))
	setPendingSource(m, config.PendingItemSourceConfig{Path: path, StateColumn: "status"})
	internal, err := m.CreatePendingItem("internal one", "A-1", "tester") // same ref, other source
	if err != nil {
		t.Fatal(err)
	}

	res := m.RefreshPendingItems()
	wantRefresh(t, res, PendingItemRefreshResult{Configured: true, Rows: 3, Added: 2, Skipped: 1})
	first := itemByRef(t, m, "A-1")
	if first.Title != "First piece" || first.State != PendingItemWaiting || first.ID == internal.ID {
		t.Fatalf("A-1 %+v", first)
	}
	if got := itemByRef(t, m, "A-2"); got.Title != "Second" {
		t.Fatalf("A-2 %+v", got)
	}
	if got, _ := m.GetPendingItem(internal.ID); got.Title != "internal one" || got.Source != PendingItemSourceInternal {
		t.Fatalf("the internal item changed: %+v", got)
	}
	last, ok := m.LastPendingItemRefresh()
	if !ok || last.Added != 2 || last.Path != path {
		t.Fatalf("last result %+v", last)
	}
}

func TestPendingItemRefreshUpdatesTitle(t *testing.T) {
	m := refreshManager(t)
	path := writeRegistry(t, tableOf("| A-1 | Old title | open |", "| A-2 | Steady | open |"))
	setPendingSource(m, config.PendingItemSourceConfig{Path: path})
	if res := m.RefreshPendingItems(); res.Added != 2 {
		t.Fatalf("first refresh %+v", res)
	}
	id := itemByRef(t, m, "A-1").ID

	rewriteRegistry(t, path, tableOf("| A-1 | New title | open |", "| A-2 | Steady | open |"))
	res := m.RefreshPendingItems()
	wantRefresh(t, res, PendingItemRefreshResult{Configured: true, Rows: 2, Updated: 1})
	got := itemByRef(t, m, "A-1")
	if got.ID != id || got.Title != "New title" || got.State != PendingItemWaiting {
		t.Fatalf("A-1 after update %+v", got)
	}
}

func TestPendingItemRefreshDoneReleasesHold(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	item, err := h.m.pendingItems.Create("wait for the design", "D-1", PendingItemSourceFile)
	if err != nil {
		t.Fatal(err)
	}
	a, err := h.m.CreateAutomation(AutomationInput{
		Name: "held", EntryMemberID: "m0",
		Members: []AutomationMember{
			pendingMember("m0", item.ID, "m1"),
			h.cm(1, "m2", "", ""),
			h.cm(2, "", "", ""),
		},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	held := activateHeld(t, h, a)
	if held.Status != AutomationBlocked || held.CurrentMemberID != "m0" {
		t.Fatalf("status %s cursor %q, want blocked at m0", held.Status, held.CurrentMemberID)
	}

	path := writeRegistry(t, tableOf("| D-1 | wait for the design | open |"))
	setPendingSource(h.m, config.PendingItemSourceConfig{Path: path, StateColumn: "Status"})
	if res := h.m.RefreshPendingItems(); res.Error != "" || res.Done != 0 {
		t.Fatalf("open row: %+v", res)
	}
	if got := h.get(a); got.Status != AutomationBlocked {
		t.Fatalf("an open row released the hold: %s", got.Status)
	}

	rewriteRegistry(t, path, tableOf("| D-1 | wait for the design | Shipped |"))
	res := h.m.RefreshPendingItems()
	wantRefresh(t, res, PendingItemRefreshResult{Configured: true, Rows: 1, Done: 1})
	if got, _ := h.m.GetPendingItem(item.ID); got.State != PendingItemDone || got.DoneAt == nil {
		t.Fatalf("item %+v", got)
	}
	got := h.get(a)
	if got.Status != AutomationActive || got.CurrentMemberID != "m1" {
		t.Fatalf("status %s cursor %q, want active at m1", got.Status, got.CurrentMemberID)
	}
	if m0 := automationMember(got, "m0"); m0.Status != MemberCompleted || m0.Outcome != pendingOutcomeDone {
		t.Fatalf("m0 %+v", m0)
	}
	if runs, _ := h.fake.counts(); len(runs) != 1 || runs[0] != h.prds[1].ID {
		t.Fatalf("runs %v, want [%s]", runs, h.prds[1].ID)
	}
}

func TestPendingItemRefreshMissingRowKeep(t *testing.T) {
	m := refreshManager(t)
	path := writeRegistry(t, tableOf("| A-1 | One | open |", "| A-2 | Two | open |"))
	setPendingSource(m, config.PendingItemSourceConfig{Path: path}) // on_missing_row unset reads keep
	m.RefreshPendingItems()

	rewriteRegistry(t, path, tableOf("| A-1 | One | open |"))
	res := m.RefreshPendingItems()
	wantRefresh(t, res, PendingItemRefreshResult{Configured: true, Rows: 1, Kept: 1})
	if got := itemByRef(t, m, "A-2"); got.State != PendingItemWaiting {
		t.Fatalf("A-2 %+v", got)
	}
	if n := len(fileItems(m)); n != 2 {
		t.Fatalf("%d file items, want 2", n)
	}
}

func TestPendingItemRefreshMissingRowDrop(t *testing.T) {
	h, _ := newControlHarness(t, 2)
	path := writeRegistry(t, tableOf(
		"| A-1 | Stays in file | open |",
		"| A-2 | Vanishes, free | open |",
		"| A-3 | Vanishes, member | open |",
		"| A-4 | Vanishes, attached | open |",
		"| A-5 | Vanishes, done | closed |"))
	setPendingSource(h.m, config.PendingItemSourceConfig{Path: path, StateColumn: "3", OnMissingRow: config.PendingItemOnMissingRowDrop})
	if res := h.m.RefreshPendingItems(); res.Error != "" || res.Added != 5 || res.Done != 1 {
		t.Fatalf("first refresh %+v", res)
	}
	internal := newPendingItem(t, h.m, "internal, never touched")
	member := itemByRef(t, h.m, "A-3")
	if _, err := h.m.CreateAutomation(AutomationInput{
		Name: "uses A-3", EntryMemberID: "m0",
		Members: []AutomationMember{pendingMember("m0", member.ID, "m1"), h.cm(1, "", "", "")},
	}, "operator"); err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	attachedID := itemByRef(t, h.m, "A-4").ID
	if _, err := h.m.pendingItems.Update(attachedID, func(p *PendingItem) error {
		p.State, p.AutomatonID = PendingItemAttached, h.prds[1].ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	rewriteRegistry(t, path, tableOf("| A-1 | Stays in file | open |"))
	res := h.m.RefreshPendingItems()
	wantRefresh(t, res, PendingItemRefreshResult{Configured: true, Rows: 1, Dropped: 1, Kept: 3})
	if _, ok := h.m.pendingItems.FindByRef(PendingItemSourceFile, "A-2"); ok {
		t.Fatal("the free waiting item should have been dropped")
	}
	for _, ref := range []string{"A-1", "A-3", "A-4", "A-5"} {
		itemByRef(t, h.m, ref)
	}
	if got, err := h.m.GetPendingItem(internal.ID); err != nil || got.Source != PendingItemSourceInternal {
		t.Fatalf("internal item: %+v %v", got, err)
	}
}

func TestPendingItemRefreshUnreadableFileKeepsRegistry(t *testing.T) {
	m := refreshManager(t)
	path := writeRegistry(t, tableOf("| A-1 | One | open |", "| A-2 | Two | open |"))
	setPendingSource(m, config.PendingItemSourceConfig{Path: path, OnMissingRow: config.PendingItemOnMissingRowDrop})
	m.RefreshPendingItems()
	before := m.pendingItems.List(PendingItemFilter{})

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	res := m.RefreshPendingItems()
	if !res.Configured || res.Error == "" || res.Added+res.Updated+res.Done+res.Dropped != 0 {
		t.Fatalf("missing file result %+v", res)
	}
	if after := m.pendingItems.List(PendingItemFilter{}); !reflect.DeepEqual(before, after) {
		t.Fatal("registry changed after an unreadable file")
	}

	// A file with no usable table is an error too: it must not read as "every
	// row vanished".
	rewriteRegistry(t, path, "# Notes\n\nNo table here.\n")
	res = m.RefreshPendingItems()
	if res.Error == "" || res.Dropped != 0 {
		t.Fatalf("no-table result %+v", res)
	}
	if after := m.pendingItems.List(PendingItemFilter{}); !reflect.DeepEqual(before, after) {
		t.Fatal("registry changed after a file with no table")
	}
	if last, _ := m.LastPendingItemRefresh(); last.Error != res.Error {
		t.Fatalf("last result %+v", last)
	}
}

func TestPendingItemRefreshPathEscapeRefused(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	rewriteRegistry(t, filepath.Join(root, "outside.md"), tableOf("| A-1 | Outside | open |"))
	rewriteRegistry(t, filepath.Join(base, "inside.md"), tableOf("| B-1 | Inside | open |"))

	m := refreshManager(t)
	m.SetPendingItemSourceBaseDir(base)
	setPendingSource(m, config.PendingItemSourceConfig{Path: filepath.Join("..", "outside.md")})
	res := m.RefreshPendingItems()
	if !res.Configured || !strings.Contains(res.Error, ErrPendingItemSourceEscapes.Error()) {
		t.Fatalf("escape result %+v", res)
	}
	if n := len(m.pendingItems.List(PendingItemFilter{})); n != 0 {
		t.Fatalf("%d items after a refused path", n)
	}

	// A relative path inside the base directory is read.
	setPendingSource(m, config.PendingItemSourceConfig{Path: "inside.md"})
	if res := m.RefreshPendingItems(); res.Error != "" || res.Added != 1 {
		t.Fatalf("inside result %+v", res)
	}
	itemByRef(t, m, "B-1")
}

func TestPendingItemRefreshIdempotent(t *testing.T) {
	m := refreshManager(t)
	path := writeRegistry(t, tableOf("| A-1 | One | open |", "| A-2 | Two | done |", "| A-3 | Three | open |"))
	setPendingSource(m, config.PendingItemSourceConfig{Path: path, StateColumn: "Status", OnMissingRow: config.PendingItemOnMissingRowDrop})
	first := m.RefreshPendingItems()
	wantRefresh(t, first, PendingItemRefreshResult{Configured: true, Rows: 3, Added: 3, Done: 1})
	snapshot := m.pendingItems.List(PendingItemFilter{})
	stored, err := os.ReadFile(m.pendingItems.path)
	if err != nil {
		t.Fatal(err)
	}

	second := m.RefreshPendingItems()
	wantRefresh(t, second, PendingItemRefreshResult{Configured: true, Rows: 3})
	if after := m.pendingItems.List(PendingItemFilter{}); !reflect.DeepEqual(snapshot, after) {
		t.Fatal("a second refresh of an unchanged file changed the registry")
	}
	if again, _ := os.ReadFile(m.pendingItems.path); string(again) != string(stored) {
		t.Fatal("a second refresh rewrote the registry file")
	}
}
