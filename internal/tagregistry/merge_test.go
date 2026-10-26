package tagregistry

import (
	"context"
	"testing"
)

// mergeScene sets up the two-node case: node a creates "Work" while the keeper
// is unreachable, and node b creates "work" under the same root while the
// keeper is reachable. The returned id is the pending tag of node a.
func mergeScene(t *testing.T, g *rig) (pa, pb *Registry, sa, sb *fakeStore, pendingID string) {
	t.Helper()
	ctx := context.Background()
	pa, sa = syncedPeer(t, g, "a")
	pb, sb = syncedPeer(t, g, "b")

	g.links["a"].setDown(true)
	_ = pa.Sync(ctx)
	made, err := pa.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("offline create on a: %v", err)
	}
	if _, err := pb.Create(ctx, CreateInput{Name: "work"}); err != nil {
		t.Fatalf("online create on b: %v", err)
	}
	g.links["a"].setDown(false)
	return pa, pb, sa, sb, made.Tag.ID
}

func TestMergeSameNameCreatedOnTwoNodes(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	pa, _, sa, _, pending := mergeScene(t, g)
	sa.assign("auto-1", pending, true)

	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("Sync after the keeper returned: %v", err)
	}
	snap, err := g.keeper.KeeperSnapshot()
	if err != nil {
		t.Fatalf("KeeperSnapshot: %v", err)
	}
	if len(snap.Tags) != 1 {
		t.Fatalf("keeper holds %d tags, want the one merged tag", len(snap.Tags))
	}
	kept := snap.Tags[0]
	if kept.ID == pending {
		t.Fatalf("keeper kept the pending id; the merge should keep its own tag")
	}

	tags, primary := sa.tagsOf("auto-1")
	if len(tags) != 1 || tags[0] != kept.ID || primary != kept.ID {
		t.Errorf("assignments after merge = %v primary %q, want [%s] primary %s", tags, primary, kept.ID, kept.ID)
	}

	v := pa.View()
	if _, ok := tagByName(v.Tags, "work"); !ok {
		t.Fatalf("merged tag missing from the peer copy")
	}
	for _, tag := range v.Tags {
		if tag.ID == pending {
			t.Errorf("replaced tag %q still on the peer", pending)
		}
	}
	if len(v.Notes) != 1 {
		t.Fatalf("notes = %+v, want one merge note", v.Notes)
	}
	n := v.Notes[0]
	if n.Kind != NoteMerged || n.KeptID != kept.ID || n.ReplacedID != pending {
		t.Errorf("merge note = %+v, want merged, kept %s, replaced %s", n, kept.ID, pending)
	}
	if len(n.Nodes) != 2 || n.Nodes[0] != "a" || n.Nodes[1] != "b" {
		t.Errorf("merge note nodes = %v, want [a b]", n.Nodes)
	}
	if n.MergedAt.IsZero() {
		t.Errorf("merge note has no merged_at time")
	}
}

func TestMergeChildOfMergedParentFollowsKeptID(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	pa, _ := syncedPeer(t, g, "a")
	pb, _ := syncedPeer(t, g, "b")

	g.links["a"].setDown(true)
	_ = pa.Sync(ctx)
	parent, err := pa.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("offline parent: %v", err)
	}
	if _, err := pa.Create(ctx, CreateInput{Name: "Sub", ParentID: parent.Tag.ID}); err != nil {
		t.Fatalf("offline child: %v", err)
	}
	if _, err := pb.Create(ctx, CreateInput{Name: "WORK"}); err != nil {
		t.Fatalf("online create on b: %v", err)
	}
	g.links["a"].setDown(false)

	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	snap, err := g.keeper.KeeperSnapshot()
	if err != nil {
		t.Fatalf("KeeperSnapshot: %v", err)
	}
	kept, ok := tagByName(snap.Tags, "WORK")
	if !ok {
		t.Fatalf("keeper lost the kept tag; tags = %+v", snap.Tags)
	}
	sub, ok := tagByName(snap.Tags, "Sub")
	if !ok {
		t.Fatalf("pending child was not pushed; tags = %+v", snap.Tags)
	}
	if sub.ParentID != kept.ID {
		t.Errorf("child parent on keeper = %q, want the kept tag %q", sub.ParentID, kept.ID)
	}
}

func TestMergeNoteShownOnceAcrossSnapshots(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	pa, pb, _, _, _ := mergeScene(t, g)
	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("merge Sync on a: %v", err)
	}
	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("second Sync on a: %v", err)
	}
	if n := len(pa.View().Notes); n != 1 {
		t.Errorf("node a holds %d notes after repeated syncs, want 1", n)
	}
	if err := pb.Sync(ctx); err != nil {
		t.Fatalf("Sync on b: %v", err)
	}
	if n := len(pb.View().Notes); n != 1 {
		t.Errorf("node b holds %d notes, want the merge note from the snapshot", n)
	}
}

func TestMergeDismissedNoteStaysHidden(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	pa, _, _, _, _ := mergeScene(t, g)
	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("merge Sync: %v", err)
	}
	notes := pa.View().Notes
	if len(notes) != 1 {
		t.Fatalf("notes = %d, want 1 before dismissal", len(notes))
	}
	if err := pa.DismissNote(notes[0].ID); err != nil {
		t.Fatalf("DismissNote: %v", err)
	}
	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("Sync after dismissal: %v", err)
	}
	if n := len(pa.View().Notes); n != 0 {
		t.Errorf("dismissed note came back: %d notes", n)
	}
}

func TestMergeOnlineCreateIsNotMerged(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	pa, _ := syncedPeer(t, g, "a")
	pb, _ := syncedPeer(t, g, "b")
	if _, err := pb.Create(ctx, CreateInput{Name: "Work"}); err != nil {
		t.Fatalf("online create on b: %v", err)
	}
	if _, err := pa.Create(ctx, CreateInput{Name: "work"}); err != nil {
		t.Fatalf("online create on a: %v", err)
	}
	snap, err := g.keeper.KeeperSnapshot()
	if err != nil {
		t.Fatalf("KeeperSnapshot: %v", err)
	}
	if len(snap.Tags) != 2 {
		t.Errorf("keeper holds %d tags, want 2: an online create is not a merge", len(snap.Tags))
	}
	if len(snap.Notes) != 0 {
		t.Errorf("keeper recorded %d notes for an online create", len(snap.Notes))
	}
}
