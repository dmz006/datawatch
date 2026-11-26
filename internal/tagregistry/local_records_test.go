package tagregistry

// The daemon writes tag records in two ways: through the registry, and
// directly (auto tags). These tests use the real autonomous manager, the real
// cache file and a temp directory, with made-up data, to hold the rule that
// the registry never drops a record it did not delete itself.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// sandbox is one daemon's tag data on disk.
type sandbox struct {
	t   *testing.T
	dir string
}

func newSandbox(t *testing.T) sandbox { return sandbox{t: t, dir: t.TempDir()} }

func (s sandbox) cache() FileCache { return FileCache{Path: filepath.Join(s.dir, "tag-registry.json")} }

// manager opens the autonomous manager on the sandbox with auto tags on.
func (s sandbox) manager() *autonomous.Manager {
	s.t.Helper()
	m, err := autonomous.NewManager(filepath.Join(s.dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		s.t.Fatalf("NewManager: %v", err)
	}
	on := true
	m.SetAutoTagsDaemon(func() config.AutoTagsConfig { return config.AutoTagsConfig{Enabled: &on} })
	return m
}

// start builds the registry as the daemon does at boot.
func (s sandbox) start(m *autonomous.Manager) *Registry {
	s.t.Helper()
	r, err := New(Options{NodeName: "solo", Store: AutonomousStore{Book: m}, Cache: s.cache()})
	if err != nil {
		s.t.Fatalf("New: %v", err)
	}
	return r
}

// automation makes an Automation with n new member Automata, which makes the
// daemon write auto tags straight to the tag records.
func (s sandbox) automation(m *autonomous.Manager, name string, n int) []string {
	s.t.Helper()
	var ids []string
	var members []autonomous.AutomationMember
	for i := 0; i < n; i++ {
		prd, err := m.Store().CreatePRD("Add a made-up feature to "+name, "/made-up", "", "", "")
		if err != nil {
			s.t.Fatalf("CreatePRD: %v", err)
		}
		ids = append(ids, prd.ID)
		members = append(members, autonomous.AutomationMember{PRDID: prd.ID})
	}
	if _, err := m.CreateAutomation(autonomous.AutomationInput{Name: name, Members: members}, "operator"); err != nil {
		s.t.Fatalf("CreateAutomation: %v", err)
	}
	return ids
}

func tagNames(m *autonomous.Manager) map[string]string {
	out := map[string]string{}
	for _, t := range m.ListTags() {
		out[t.ID] = t.Name
	}
	return out
}

// wantAllTagsResolve fails when a tag of before is gone or when an Automaton
// carries a tag ID with no record.
func wantAllTagsResolve(t *testing.T, m *autonomous.Manager, before map[string]string, prds []string) {
	t.Helper()
	now := tagNames(m)
	for id, name := range before {
		if _, ok := now[id]; !ok {
			t.Errorf("tag %s %q was dropped", id, name)
		}
	}
	for _, prdID := range prds {
		p, ok := m.Store().GetPRD(prdID)
		if !ok {
			t.Fatalf("Automaton %s is gone", prdID)
		}
		if len(p.TagIDs) == 0 {
			t.Errorf("Automaton %s carries no tags", prdID)
		}
		for _, id := range p.TagIDs {
			if _, ok := now[id]; !ok {
				t.Errorf("Automaton %s points to missing tag %s", prdID, id)
			}
		}
	}
}

func TestStartDropsNoTagMadeOutsideTheRegistry(t *testing.T) {
	s := newSandbox(t)
	m := s.manager()
	r := s.start(m)
	// One registry write saves the cache.
	if _, err := r.Create(context.Background(), CreateInput{Name: "operator tag"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	prds := s.automation(m, "made-up history", 2)
	before := tagNames(m)
	if len(before) < 3 {
		t.Fatalf("sandbox made %d tags, want the operator tag and auto tags: %v", len(before), before)
	}

	// Restart: new manager and new registry over the same files.
	m2 := s.manager()
	r2 := s.start(m2)
	wantAllTagsResolve(t, m2, before, prds)
	if got := len(r2.View().Tags); got != len(before) {
		t.Errorf("registry view after the restart holds %d tags, want %d", got, len(before))
	}
	// And a second restart, from the cache the first one left.
	m3 := s.manager()
	s.start(m3)
	wantAllTagsResolve(t, m3, before, prds)
}

func TestRegistryWriteDropsNoTagMadeOutsideTheRegistry(t *testing.T) {
	s := newSandbox(t)
	m := s.manager()
	r := s.start(m)
	made, err := r.Create(context.Background(), CreateInput{Name: "operator tag"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	prds := s.automation(m, "made-up history", 2)
	before := tagNames(m)

	// No restart: any later registry write.
	if _, err := r.Recolor(context.Background(), made.Tag.ID, "#123456"); err != nil {
		t.Fatalf("Recolor: %v", err)
	}
	wantAllTagsResolve(t, m, before, prds)
}

// savingCache runs during once, while a registry write is between reading the
// records and writing them back.
type savingCache struct {
	FileCache
	during func()
}

func (c *savingCache) Save(data []byte) error {
	if f := c.during; f != nil {
		c.during = nil
		f()
	}
	return c.FileCache.Save(data)
}

func TestRegistryWriteDropsNoTagMadeWhileItRuns(t *testing.T) {
	s := newSandbox(t)
	m := s.manager()
	cache := &savingCache{FileCache: s.cache()}
	r, err := New(Options{NodeName: "solo", Store: AutonomousStore{Book: m}, Cache: cache})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var made string
	cache.during = func() {
		// A direct write on another goroutine's path. The manager must not
		// block on the registry here, so the write runs on its own goroutine
		// and only its record is waited for.
		done := make(chan struct{})
		go func() {
			defer close(done)
			tg, err := m.EnsureSystemTag("made while a write runs")
			if err != nil {
				t.Errorf("EnsureSystemTag: %v", err)
				return
			}
			made = tg.ID
		}()
		waitForRecord(t, m, "made while a write runs")
		t.Cleanup(func() { <-done })
	}
	if _, err := r.Create(context.Background(), CreateInput{Name: "operator tag"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	found := false
	for _, name := range tagNames(m) {
		found = found || name == "made while a write runs"
	}
	if !found {
		t.Errorf("the tag made during the write was dropped (id %q)", made)
	}
}

// waitForRecord waits until the manager holds a tag record with name.
func waitForRecord(t *testing.T, m *autonomous.Manager, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, have := range tagNames(m) {
			if have == name {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Errorf("the tag %q was never written", name)
}

func TestPeerSyncKeepsAutoTagsAndTheirAssignments(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	if _, err := g.keeper.Create(ctx, CreateInput{Name: "shared"}); err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	s := newSandbox(t)
	m := s.manager()
	link := &fakeKeeper{reg: g.keeper}
	opts := func(m *autonomous.Manager) Options {
		return Options{NodeName: "a", KeeperName: "keeper", Store: AutonomousStore{Book: m}, Keeper: link, Cache: s.cache(), Clock: g.clock}
	}
	// Auto tags made before the first sync and after it.
	prds := s.automation(m, "made before the first sync", 2)
	p := mustNew(t, opts(m))
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	prds = append(prds, s.automation(m, "made after the first sync", 2)...)
	before := tagNames(m)
	for i := 0; i < 2; i++ {
		if err := p.Sync(ctx); err != nil {
			t.Fatalf("Sync: %v", err)
		}
	}
	wantAllTagsResolve(t, m, before, prds)

	// A restart, then another sync.
	m2 := s.manager()
	p2 := mustNew(t, opts(m2))
	if err := p2.Sync(ctx); err != nil {
		t.Fatalf("Sync after restart: %v", err)
	}
	wantAllTagsResolve(t, m2, before, prds)
}

func autoTagNamed(t *testing.T, m *autonomous.Manager, name string) *autonomous.Tag {
	t.Helper()
	for _, tg := range m.ListTags() {
		if tg.Name == name {
			return tg
		}
	}
	t.Fatalf("no tag named %q in %v", name, tagNames(m))
	return nil
}

func TestRegistryShowsAndEditsAutoTags(t *testing.T) {
	s := newSandbox(t)
	m := s.manager()
	r := s.start(m)
	if _, err := r.Create(context.Background(), CreateInput{Name: "operator tag"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	s.automation(m, "made-up history", 1)
	id := autoTagNamed(t, m, "automation:made-up history").ID
	if _, ok := tagByName(r.View().Tags, "automation:made-up history"); !ok {
		t.Errorf("the registry view does not show the auto tag")
	}
	if _, err := r.Recolor(context.Background(), id, "#abcdef"); err != nil {
		t.Errorf("recolor an auto tag: %v", err)
	}
}

// An auto tag the daemon renames or deletes must stay renamed or deleted: the
// saved registry copy must not bring the old record back at the next start.
func TestStartDoesNotUndoAnAutoTagRenameOrDelete(t *testing.T) {
	s := newSandbox(t)
	m := s.manager()
	s.automation(m, "first name", 1)
	s.automation(m, "to be deleted", 1)
	r := s.start(m)
	// The saved copy now holds both automation tags.
	if _, err := r.Create(context.Background(), CreateInput{Name: "operator tag"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	gone := autoTagNamed(t, m, "automation:to be deleted").ID
	renamed := autoTagNamed(t, m, "automation:first name").ID
	for _, a := range m.ListAutomations() {
		switch a.Name {
		case "first name":
			if _, err := m.UpdateAutomation(a.ID, a.Version, autonomous.AutomationInput{Name: "second name"}, "operator"); err != nil {
				t.Fatalf("UpdateAutomation: %v", err)
			}
		case "to be deleted":
			if err := m.DeleteAutomation(a.ID); err != nil {
				t.Fatalf("DeleteAutomation: %v", err)
			}
		}
	}
	if _, ok := tagNames(m)[gone]; ok {
		t.Fatalf("setup: the daemon did not delete the tag of the deleted Automation")
	}

	m2 := s.manager()
	s.start(m2)
	if name, ok := tagNames(m2)[gone]; ok {
		t.Errorf("the start brought back the deleted auto tag %q", name)
	}
	if got := tagNames(m2)[renamed]; got != "automation:second name" {
		t.Errorf("the renamed auto tag is %q after the start, want %q", got, "automation:second name")
	}
}
