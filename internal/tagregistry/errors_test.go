package tagregistry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRegistryStoreAndCacheFailuresAreReported(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	boom := errors.New("boom")

	listFail := newFakeStore()
	listFail.failList = boom
	if _, err := New(Options{NodeName: "solo", Store: listFail, Clock: g.clock, NewID: g.ids}); !errors.Is(err, boom) {
		t.Errorf("New with a failing store list = %v, want the store error", err)
	}

	applyFail := newFakeStore()
	applyFail.failApply = boom
	cached := &fakeCache{ok: true, data: []byte("{}")}
	if _, err := New(Options{NodeName: "solo", Store: applyFail, Cache: cached, Clock: g.clock, NewID: g.ids}); !errors.Is(err, boom) {
		t.Errorf("New with a failing store apply = %v, want the store error", err)
	}

	made, err := g.keeper.Create(ctx, CreateInput{Name: "X"})
	if err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	g.kstore.failCount = boom
	if _, err := g.keeper.Delete(ctx, made.Tag.ID); !errors.Is(err, boom) || errors.Is(err, ErrTagInUse) {
		t.Errorf("delete with a failing count = %v, want the store error and not in-use", err)
	}
	g.kstore.failCount = nil

	g.kstore.failApply = boom
	if _, err := g.keeper.Create(ctx, CreateInput{Name: "Y"}); !errors.Is(err, boom) {
		t.Fatalf("create with a failing store = %v, want the store error", err)
	}
	if _, ok := tagByName(g.keeper.View().Tags, "Y"); ok {
		t.Errorf("a create the store refused still shows in the registry")
	}
	g.kstore.failApply = nil
}

func TestRegistryFailedSaveLeavesNoTrace(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	boom := errors.New("disk full")
	r := mustNew(t, Options{
		NodeName: "solo", Store: newFakeStore(),
		Cache: &fakeCache{failSave: boom}, Clock: g.clock, NewID: g.ids,
	})
	before := r.View().Revision
	if _, err := r.Create(ctx, CreateInput{Name: "Lost"}); !errors.Is(err, boom) {
		t.Fatalf("create with a failing cache = %v, want the save error", err)
	}
	v := r.View()
	if len(v.Tags) != 0 {
		t.Errorf("failed save left %d tags in the copy", len(v.Tags))
	}
	if v.Revision != before {
		t.Errorf("failed save moved the revision from %d to %d", before, v.Revision)
	}
}

func TestRegistryRealClockAndRandomIDDefaults(t *testing.T) {
	ctx := context.Background()
	r := mustNew(t, Options{NodeName: "solo", Store: newFakeStore()})
	a, err := r.Create(ctx, CreateInput{Name: "One"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b, err := r.Create(ctx, CreateInput{Name: "Two"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.Tag.ID == "" || a.Tag.ID == b.Tag.ID {
		t.Errorf("random ids are empty or repeat: %q and %q", a.Tag.ID, b.Tag.ID)
	}
	if len(a.Tag.ID) != 16 {
		t.Errorf("random id %q is %d characters, want 16", a.Tag.ID, len(a.Tag.ID))
	}
	if a.Tag.CreatedAt.IsZero() {
		t.Errorf("real clock gave a zero creation time")
	}
	if (realClock{}).Now().IsZero() {
		t.Errorf("realClock.Now is zero")
	}
	select {
	case <-(realClock{}).After(time.Millisecond):
	case <-time.After(2 * time.Second):
		t.Fatalf("realClock.After did not fire")
	}
}

func TestKeeperRefusesDeepCycle(t *testing.T) {
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
	leaf, err := g.keeper.Create(ctx, CreateInput{Name: "Leaf", ParentID: mid.Tag.ID})
	if err != nil {
		t.Fatalf("Create Leaf: %v", err)
	}
	_, err = g.keeper.ApplyPeerWrite(Write{
		Kind: WriteReparent, TagID: root.Tag.ID, ParentID: leaf.Tag.ID,
		BaseRevision: g.keeper.View().Revision,
	})
	if !errors.Is(err, ErrTagCycle) {
		t.Errorf("reparent under a grandchild = %v, want ErrTagCycle", err)
	}
}

func TestMergeNoteNamesParentPath(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	work, err := g.keeper.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("keeper Create Work: %v", err)
	}
	if _, err := g.keeper.Create(ctx, CreateInput{Name: "Sub", ParentID: work.Tag.ID}); err != nil {
		t.Fatalf("keeper Create Sub: %v", err)
	}
	pa, _ := syncedPeer(t, g, "a")
	g.links["a"].setDown(true)
	_ = pa.Sync(ctx)
	if _, err := pa.Create(ctx, CreateInput{Name: "sub", ParentID: work.Tag.ID}); err != nil {
		t.Fatalf("offline create: %v", err)
	}
	g.links["a"].setDown(false)
	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	notes := pa.View().Notes
	if len(notes) != 1 {
		t.Fatalf("notes = %+v, want one merge note", notes)
	}
	if notes[0].ParentPath != "Work" || notes[0].Name != "Sub" {
		t.Errorf("merge note = %+v, want parent path Work and name Sub", notes[0])
	}
}
