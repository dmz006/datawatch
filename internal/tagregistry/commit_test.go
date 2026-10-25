package tagregistry

import (
	"context"
	"errors"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestRegistryFailedSaveStandaloneCreateLeavesStoreUntouched(t *testing.T) {
	g := newRig(t)
	boom := errors.New("disk full")
	st := newFakeStore()
	r := mustNew(t, Options{
		NodeName: "solo", Store: st,
		Cache: &fakeCache{failSave: boom}, Clock: g.clock, NewID: g.ids,
	})
	if _, err := r.Create(context.Background(), CreateInput{Name: "Lost"}); !errors.Is(err, boom) {
		t.Fatalf("create with a failing cache = %v, want the save error", err)
	}
	if len(st.tags) != 0 || st.applies != 0 {
		t.Errorf("store holds %d tags after a failed save (applies %d), want none", len(st.tags), st.applies)
	}
}

// removedOnKeeperFixture makes a peer that holds a tag with an assignment, and
// a keeper that has deleted that tag. The peer's cache is returned so a test
// can make its save fail.
func removedOnKeeperFixture(t *testing.T, g *rig) (*Registry, *fakeStore, *fakeCache, string) {
	t.Helper()
	ctx := context.Background()
	work, err := g.keeper.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	cache := &fakeCache{}
	p, st := g.peer("a", cache)
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	st.assign("item", work.Tag.ID, true)
	if _, err := g.keeper.Delete(ctx, work.Tag.ID); err != nil {
		t.Fatalf("keeper Delete: %v", err)
	}
	return p, st, cache, work.Tag.ID
}

func TestRegistryFailedSaveKeepsAssignmentsOnRemoval(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	boom := errors.New("disk full")
	p, st, cache, workID := removedOnKeeperFixture(t, g)

	cache.failSave = boom
	if err := p.Sync(ctx); !errors.Is(err, boom) {
		t.Fatalf("Sync with a failing cache = %v, want the save error", err)
	}
	if got, _ := st.tagsOf("item"); !containsString(got, workID) {
		t.Errorf("failed sync removed the assignment; item tags = %v", got)
	}
	if _, ok := tagByName(p.View().Tags, "Work"); !ok {
		t.Errorf("failed sync dropped the tag from the copy")
	}
	if n := len(p.View().Notes); n != 0 {
		t.Errorf("failed sync recorded %d notes, want none", n)
	}

	cache.failSave = nil
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("retry Sync: %v", err)
	}
	if got, _ := st.tagsOf("item"); containsString(got, workID) {
		t.Errorf("retried sync left the assignment; item tags = %v", got)
	}
	notes := p.View().Notes
	if len(notes) != 1 || notes[0].Kind != NoteRemovedOnKeeper {
		t.Errorf("retried sync notes = %+v, want one removed-on-keeper note", notes)
	}
}

func TestRegistryFailedStoreStepRestoresStateAndCache(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	boom := errors.New("store locked")
	p, st, cache, workID := removedOnKeeperFixture(t, g)

	st.failRemove = boom
	if err := p.Sync(ctx); !errors.Is(err, boom) {
		t.Fatalf("Sync with a failing store = %v, want the store error", err)
	}
	if got, _ := st.tagsOf("item"); !containsString(got, workID) {
		t.Errorf("failed store step removed the assignment; item tags = %v", got)
	}
	if n := len(p.View().Notes); n != 0 {
		t.Errorf("failed store step recorded %d notes, want none", n)
	}

	// A daemon that restarts now reads the cache. It must show the last copy,
	// without the removal that did not commit.
	again := mustNew(t, Options{
		NodeName: "a", KeeperName: "keeper", Store: newFakeStore(),
		Keeper: g.links["a"], Cache: cache, Clock: g.clock, NewID: g.ids,
	})
	if _, ok := tagByName(again.View().Tags, "Work"); !ok {
		t.Errorf("cache reload after a failed store step lost the tag")
	}
	if n := len(again.View().Notes); n != 0 {
		t.Errorf("cache reload after a failed store step has %d notes, want none", n)
	}

	st.failRemove = nil
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("retry Sync: %v", err)
	}
	if got, _ := st.tagsOf("item"); containsString(got, workID) {
		t.Errorf("retried sync left the assignment; item tags = %v", got)
	}
	if n := len(p.View().Notes); n != 1 {
		t.Errorf("retried sync recorded %d notes, want one", n)
	}
}

func TestRegistryFailedStoreMirrorRestoresCache(t *testing.T) {
	g := newRig(t)
	boom := errors.New("store locked")
	st := newFakeStore()
	cache := &fakeCache{}
	r := mustNew(t, Options{
		NodeName: "solo", Store: st, Cache: cache, Clock: g.clock, NewID: g.ids,
	})
	st.failApply = boom
	if _, err := r.Create(context.Background(), CreateInput{Name: "Lost"}); !errors.Is(err, boom) {
		t.Fatalf("create with a failing store = %v, want the store error", err)
	}
	again := mustNew(t, Options{
		NodeName: "solo", Store: newFakeStore(), Cache: cache, Clock: g.clock, NewID: g.ids,
	})
	if n := len(again.View().Tags); n != 0 {
		t.Errorf("cache reload after a failed store mirror has %d tags, want none", n)
	}
}

func TestMergeFailedSaveThenRetryRecordsOneNote(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	boom := errors.New("disk full")
	work, err := g.keeper.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("keeper Create Work: %v", err)
	}
	cache := &fakeCache{}
	pa, st := g.peer("a", cache)
	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	g.links["a"].setDown(true)
	_ = pa.Sync(ctx)
	pending, err := pa.Create(ctx, CreateInput{Name: "sub", ParentID: work.Tag.ID})
	if err != nil {
		t.Fatalf("offline create: %v", err)
	}
	st.assign("item", pending.Tag.ID, true)
	if _, err := g.keeper.Create(ctx, CreateInput{Name: "Sub", ParentID: work.Tag.ID}); err != nil {
		t.Fatalf("keeper Create Sub: %v", err)
	}

	// The keeper merges the push, then the peer cannot save the merge.
	g.links["a"].setDown(false)
	cache.failSave = boom
	if err := pa.Sync(ctx); !errors.Is(err, boom) {
		t.Fatalf("Sync with a failing cache = %v, want the save error", err)
	}
	if got, _ := st.tagsOf("item"); !containsString(got, pending.Tag.ID) {
		t.Errorf("failed merge rewrote the assignment before the commit; item tags = %v", got)
	}

	cache.failSave = nil
	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("retry Sync: %v", err)
	}
	kept, ok := tagByName(g.keeper.View().Tags, "Sub")
	if !ok {
		t.Fatalf("keeper lost its Sub tag")
	}
	got, _ := st.tagsOf("item")
	if containsString(got, pending.Tag.ID) || !containsString(got, kept.ID) {
		t.Errorf("retried merge item tags = %v, want the kept id %q only", got, kept.ID)
	}
	if n := len(g.keeper.View().Notes); n != 1 {
		t.Errorf("keeper has %d merge notes after a retried push, want one", n)
	}
	if n := len(pa.View().Notes); n != 1 {
		t.Errorf("peer has %d merge notes after a retried push, want one", n)
	}
}

func TestRegistryRefusalsMatchAutonomousErrors(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	root, err := g.keeper.Create(ctx, CreateInput{Name: "Root"})
	if err != nil {
		t.Fatalf("Create Root: %v", err)
	}
	mid, err := g.keeper.Create(ctx, CreateInput{Name: "Mid", ParentID: root.Tag.ID})
	if err != nil {
		t.Fatalf("Create Mid: %v", err)
	}
	_, err = g.keeper.Reparent(ctx, root.Tag.ID, mid.Tag.ID)
	if !errors.Is(err, autonomous.ErrTagCycle) || !errors.Is(err, ErrTagCycle) {
		t.Errorf("cycle refusal = %v, want the autonomous ErrTagCycle", err)
	}

	g.kstore.assign("item", mid.Tag.ID, false)
	_, err = g.keeper.Delete(ctx, mid.Tag.ID)
	var inUse *autonomous.TagInUseError
	if !errors.Is(err, autonomous.ErrTagInUse) || !errors.As(err, &inUse) {
		t.Fatalf("in-use refusal = %v, want the autonomous TagInUseError", err)
	}
	if inUse.Count != 1 || inUse.TagID != mid.Tag.ID {
		t.Errorf("in-use refusal = %+v, want count 1 for %q", inUse, mid.Tag.ID)
	}
	var alias *InUseError
	if !errors.As(err, &alias) {
		t.Errorf("InUseError alias does not match the refusal")
	}
}
