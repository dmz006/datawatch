package tagregistry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitSignal fails the test if ch does not receive within two seconds.
func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestRegistryRoleFromKeeperSetting(t *testing.T) {
	cases := []struct {
		node, keeper string
		want         Role
	}{
		{"a", "", RoleStandalone},
		{"keeper", "keeper", RoleKeeper},
		{"a", "keeper", RolePeer},
	}
	for _, c := range cases {
		if got := RoleFor(c.node, c.keeper); got != c.want {
			t.Errorf("RoleFor(%q, %q) = %q, want %q", c.node, c.keeper, got, c.want)
		}
	}
}

func TestRegistryStandaloneSyncIsNoop(t *testing.T) {
	g := newRig(t)
	store := newFakeStore()
	cache := &fakeCache{}
	r := mustNew(t, Options{NodeName: "solo", Store: store, Cache: cache, Clock: g.clock, NewID: g.ids})
	if r.Role() != RoleStandalone {
		t.Fatalf("role = %q, want standalone", r.Role())
	}
	if err := r.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if store.applies != 0 {
		t.Errorf("standalone sync wrote the store %d times, want 0", store.applies)
	}
	if cache.saves != 0 {
		t.Errorf("standalone sync saved the cache %d times, want 0", cache.saves)
	}
	if v := r.View(); v.Stale {
		t.Errorf("standalone view is stale, want current")
	}
	done := make(chan struct{})
	go func() {
		r.Run(context.Background())
		close(done)
	}()
	waitSignal(t, done, "standalone Run to return at once")
}

func TestRegistryStandaloneWritesAreLocal(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	store := newFakeStore()
	r := mustNew(t, Options{NodeName: "solo", Store: store, Clock: g.clock, NewID: g.ids})
	made, err := r.Create(ctx, CreateInput{Name: "Solo"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if made.Tag.PendingSync {
		t.Errorf("standalone create marked pending sync")
	}
	if _, err := r.Rename(ctx, made.Tag.ID, "Solo two"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := r.Delete(ctx, made.Tag.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.tags) != 0 {
		t.Errorf("store holds %d tags after delete, want 0", len(store.tags))
	}
}

func TestRegistryPeerConvergesAfterRenameAndReparent(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	work, err := g.keeper.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	projects, err := g.keeper.Create(ctx, CreateInput{Name: "Projects"})
	if err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	p, _ := g.peer("a", nil)
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if _, err := g.keeper.Rename(ctx, projects.Tag.ID, "Projects 2026"); err != nil {
		t.Fatalf("keeper Rename: %v", err)
	}
	if _, err := g.keeper.Reparent(ctx, projects.Tag.ID, work.Tag.ID); err != nil {
		t.Fatalf("keeper Reparent: %v", err)
	}
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	v := p.View()
	got, ok := tagByName(v.Tags, "Projects 2026")
	if !ok {
		t.Fatalf("peer lacks the renamed tag; tags = %+v", v.Tags)
	}
	if got.ParentID != work.Tag.ID {
		t.Errorf("peer parent = %q, want %q", got.ParentID, work.Tag.ID)
	}
	if _, ok := tagByName(v.Tags, "Projects"); ok {
		t.Errorf("peer still holds the old name")
	}
	if v.Revision != g.keeper.View().Revision {
		t.Errorf("peer revision = %d, keeper = %d", v.Revision, g.keeper.View().Revision)
	}
	if v.Stale {
		t.Errorf("peer is stale after a successful sync")
	}
}

func TestRegistryDropsAssignmentsOfTagDeletedOnKeeper(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	old, err := g.keeper.Create(ctx, CreateInput{Name: "Old"})
	if err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	p, st := g.peer("a", nil)
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	st.assign("auto-1", old.Tag.ID, true)
	if _, err := g.keeper.Delete(ctx, old.Tag.ID); err != nil {
		t.Fatalf("keeper Delete: %v", err)
	}
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	tags, primary := st.tagsOf("auto-1")
	if len(tags) != 0 || primary != "" {
		t.Errorf("assignments after keeper delete = %v primary %q, want none", tags, primary)
	}
	v := p.View()
	if _, ok := tagByName(v.Tags, "Old"); ok {
		t.Errorf("peer still holds the deleted tag")
	}
	var found bool
	for _, n := range v.Notes {
		if n.Kind == NoteRemovedOnKeeper && n.Name == "Old" && n.TagID == old.Tag.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("no removed-on-keeper note in %+v", v.Notes)
	}
}

func TestRegistryCacheReloadsAfterRestart(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	if _, err := g.keeper.Create(ctx, CreateInput{Name: "Work"}); err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	cache := &fakeCache{}
	p1, _ := g.peer("a", cache)
	if err := p1.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	before := p1.View()

	link := g.links["a"]
	link.setDown(true)
	store := newFakeStore()
	p2 := mustNew(t, Options{
		NodeName: "a", KeeperName: "keeper", Store: store,
		Keeper: link, Cache: cache, Clock: g.clock, NewID: g.ids,
	})
	if len(store.tags) != 1 || store.tags[0].Name != "Work" {
		t.Errorf("store after restart = %+v, want the cached tag", store.tags)
	}
	v := p2.View()
	if _, ok := tagByName(v.Tags, "Work"); !ok {
		t.Fatalf("restarted peer lacks the cached tag")
	}
	if !v.Stale {
		t.Errorf("restarted peer is not stale before its first sync")
	}
	if !v.LastSyncedAt.Equal(before.LastSyncedAt) {
		t.Errorf("LastSyncedAt = %v, want %v", v.LastSyncedAt, before.LastSyncedAt)
	}
	if v.Revision != before.Revision {
		t.Errorf("revision = %d, want %d", v.Revision, before.Revision)
	}
	if err := p2.Sync(ctx); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("Sync with keeper down = %v, want unreachable", err)
	}
	link.setDown(false)
	if err := p2.Sync(ctx); err != nil {
		t.Fatalf("Sync after keeper returns: %v", err)
	}
	if p2.View().Stale {
		t.Errorf("peer still stale after the keeper returned")
	}
}

func TestRegistryCacheCorruptOrUnreadableIsError(t *testing.T) {
	g := newRig(t)
	bad := &fakeCache{ok: true, data: []byte("{not json")}
	if _, err := New(Options{NodeName: "solo", Store: newFakeStore(), Cache: bad, Clock: g.clock, NewID: g.ids}); err == nil {
		t.Errorf("New with a corrupt cache returned no error")
	}
	broken := &fakeCache{err: errors.New("disk gone")}
	if _, err := New(Options{NodeName: "solo", Store: newFakeStore(), Cache: broken, Clock: g.clock, NewID: g.ids}); err == nil {
		t.Errorf("New with an unreadable cache returned no error")
	}
}

func TestRegistryNewValidatesOptions(t *testing.T) {
	if _, err := New(Options{NodeName: "a"}); err == nil {
		t.Errorf("New without a store returned no error")
	}
	if _, err := New(Options{NodeName: "a", KeeperName: "keeper", Store: newFakeStore()}); err == nil {
		t.Errorf("peer New without a keeper client returned no error")
	}
}

func TestRegistryRunStopsOnContextCancel(t *testing.T) {
	g := newRig(t)
	p, _ := g.peer("a", nil)
	fetched := make(chan struct{}, 4)
	g.links["a"].fetched = fetched
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	waitSignal(t, fetched, "the first sync")
	cancel()
	waitSignal(t, done, "Run to return after cancel")
}

func TestRegistryNotifyRunsSync(t *testing.T) {
	g := newRig(t)
	p, _ := g.peer("a", nil)
	fetched := make(chan struct{}, 4)
	g.links["a"].fetched = fetched
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	waitSignal(t, fetched, "the first sync")
	p.Notify()
	waitSignal(t, fetched, "a sync after Notify")
	cancel()
	waitSignal(t, done, "Run to return after cancel")
}

func TestRegistryKeeperLocalWrites(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	a, err := g.keeper.Create(ctx, CreateInput{Name: "Alpha", Color: "red"})
	if err != nil {
		t.Fatalf("Create Alpha: %v", err)
	}
	b, err := g.keeper.Create(ctx, CreateInput{Name: "Beta", ParentID: a.Tag.ID})
	if err != nil {
		t.Fatalf("Create Beta: %v", err)
	}
	if _, err := g.keeper.Rename(ctx, a.Tag.ID, "   "); !errors.Is(err, ErrTagInvalid) {
		t.Errorf("empty rename = %v, want ErrTagInvalid", err)
	}
	if _, err := g.keeper.Recolor(ctx, a.Tag.ID, "blue"); err != nil {
		t.Fatalf("Recolor: %v", err)
	}
	if _, err := g.keeper.Describe(ctx, a.Tag.ID, "first"); err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if _, err := g.keeper.Reparent(ctx, a.Tag.ID, b.Tag.ID); !errors.Is(err, ErrTagCycle) {
		t.Errorf("cycle reparent = %v, want ErrTagCycle", err)
	}
	if _, err := g.keeper.Delete(ctx, a.Tag.ID); !errors.Is(err, ErrTagHasChildren) {
		t.Errorf("delete with children = %v, want ErrTagHasChildren", err)
	}
	if _, err := g.keeper.Delete(ctx, "missing"); !errors.Is(err, ErrTagNotFound) {
		t.Errorf("delete missing = %v, want ErrTagNotFound", err)
	}
	got, ok := tagByName(g.keeper.View().Tags, "Alpha")
	if !ok || got.Color != "blue" || got.Description != "first" {
		t.Errorf("Alpha after edits = %+v, want blue with description", got)
	}
	if _, err := g.keeper.Delete(ctx, b.Tag.ID); err != nil {
		t.Fatalf("Delete Beta: %v", err)
	}
	g.kstore.assign("auto-9", a.Tag.ID, false)
	if _, err := g.keeper.Delete(ctx, a.Tag.ID); !errors.Is(err, ErrTagInUse) {
		t.Errorf("delete assigned tag = %v, want ErrTagInUse", err)
	}
	var inUse *InUseError
	_, err = g.keeper.Delete(ctx, a.Tag.ID)
	if !errors.As(err, &inUse) || inUse.Count != 1 {
		t.Errorf("delete assigned tag error = %v, want InUseError with count 1", err)
	} else if !strings.Contains(err.Error(), "assigned to 1 automata or sessions") {
		t.Errorf("InUseError text = %q", err.Error())
	}
}

func TestRegistryDismissUnknownNoteIsError(t *testing.T) {
	g := newRig(t)
	if err := g.keeper.DismissNote("nope"); !errors.Is(err, ErrNoteNotFound) {
		t.Errorf("DismissNote unknown = %v, want ErrNoteNotFound", err)
	}
}

func TestRegistryFileCacheRoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp(".", "filecache-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	c := FileCache{Path: filepath.Join(dir, "sub", "tags.json")}
	if _, ok, err := c.Load(); ok || err != nil {
		t.Fatalf("Load of a missing cache = ok %v err %v, want not ok and no error", ok, err)
	}
	if err := c.Save([]byte("one")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := c.Save([]byte("two")); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	data, ok, err := c.Load()
	if err != nil || !ok || string(data) != "two" {
		t.Errorf("Load = %q ok %v err %v, want \"two\"", data, ok, err)
	}
}
