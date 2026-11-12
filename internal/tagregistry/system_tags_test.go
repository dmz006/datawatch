package tagregistry

import (
	"context"
	"errors"
	"testing"
)

// placementTag is a placement tag name in the conventional "<axis>:<value>"
// form that internal/placementtags emits, here with the "lane" axis.
const placementTag = "lane:gpu"

// ctx is a background context for every test in this file. The links the
// rig builds are in-process, so no real I/O is ever waited on.
var ctx = context.Background()

// TestPlacementTags_SystemTagIdempotent covers EnsureSystemTag being
// create-or-return: a placement repeated on the same daemon, on two daemons,
// or over the whole registry lands on one tag with the system origin, and no
// repeated call makes a duplicate or changes the tag it found.
func TestPlacementTags_SystemTagIdempotent(t *testing.T) {
	g := newRig(t)

	first, err := g.keeper.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("EnsureSystemTag: %v", err)
	}
	if !first.Tag.System {
		t.Fatalf("created tag is not marked system: %+v", first.Tag)
	}
	second, err := g.keeper.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("second EnsureSystemTag: %v", err)
	}
	if second.Tag.ID != first.Tag.ID {
		t.Fatalf("second ensure made a new tag %q, want the existing %q", second.Tag.ID, first.Tag.ID)
	}
	if snap := keeperSnap(t, g); len(snap.Tags) != 1 {
		t.Fatalf("keeper holds %d tags, want the one shared tag", len(snap.Tags))
	}

	// An operator create with the name a system tag already holds may make its
	// own tag, but it must not touch the system tag, which stays system.
	other, err := g.keeper.Create(ctx, CreateInput{Name: placementTag})
	if err != nil {
		t.Fatalf("Create with the system tag's name: %v", err)
	}
	if other.Tag.ID == first.Tag.ID {
		t.Fatalf("operator create returned the system tag; a new tag is allowed")
	}
	if got := byID(t, keeperSnap(t, g).Tags, first.Tag.ID); got.Name != placementTag || !got.System {
		t.Fatalf("operator create changed the existing tag's origin: %+v", got)
	}
	if other.Tag.System {
		t.Fatalf("operator create is marked system: %+v", other.Tag)
	}
	if n := countNamed(t, keeperSnap(t, g).Tags, placementTag); n != 2 {
		t.Fatalf("keeper holds %d tags named %q, want the two distinct ones", n, placementTag)
	}

	// A different daemon ensures the same placement: the keeper returns the
	// tag it already holds and the peer's copy names that id.
	pb, _ := syncedPeer(t, g, "b")
	remote, err := pb.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("peer EnsureSystemTag: %v", err)
	}
	if remote.Tag.ID != first.Tag.ID {
		t.Fatalf("peer got a new tag %q, want the keeper's %q", remote.Tag.ID, first.Tag.ID)
	}
	view := pb.View()
	if len(view.Tags) != 2 {
		t.Fatalf("peer copy does not carry the keeper's tag; view = %+v", view)
	}
	if shared := byName(t, view.Tags, placementTag, 0); shared.ID != first.Tag.ID {
		t.Fatalf("peer copy sees a different id for the shared tag: %+v", shared)
	}
	pb2, _ := syncedPeer(t, g, "b2")
	if view := pb2.View(); view.Tags[0].ID != first.Tag.ID {
		t.Fatalf("a third daemon sees a different id for the shared tag; view = %+v", view)
	}

	// A peer that is still offline ensures the placement twice: one pending
	// tag on that daemon, and the same id both times.
	pa, _ := g.peer("a", nil)
	g.links["a"].setDown(true)
	local1, err := pa.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("offline EnsureSystemTag: %v", err)
	}
	if !local1.Tag.System || !local1.Tag.PendingSync {
		t.Fatalf("offline ensure did not keep a pending system tag: %+v", local1.Tag)
	}
	local2, err := pa.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("second offline EnsureSystemTag: %v", err)
	}
	if local2.Tag.ID != local1.Tag.ID {
		t.Fatalf("offline peer made a second pending tag %q, want %q", local2.Tag.ID, local1.Tag.ID)
	}
	if view := pa.View(); len(tagNamed(view.Tags, placementTag)) != 1 {
		t.Fatalf("offline peer holds %d tags named %q, want one", len(tagNamed(view.Tags, placementTag)), placementTag)
	}
}

// TestSystemTag_RenameRefused covers a rename of a system tag being refused on
// the keeper and on a peer, with the tag unchanged and the refusal matching
// ErrTagSystem.
func TestSystemTag_RenameRefused(t *testing.T) {
	g := newRig(t)
	sys, err := g.keeper.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("EnsureSystemTag: %v", err)
	}
	baseRev := keeperSnap(t, g).Revision

	if _, err := g.keeper.Rename(ctx, sys.Tag.ID, "lane:cpu"); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("keeper rename was not refused with ErrTagSystem: %v", err)
	}
	snap := keeperSnap(t, g)
	if n := countNamed(t, snap.Tags, "lane:cpu"); n != 0 {
		t.Fatalf("a renamed tag was made: %+v", snap.Tags)
	}
	if got := byID(t, snap.Tags, sys.Tag.ID); got.Name != placementTag || !got.System {
		t.Fatalf("refused rename changed the tag: %+v", got)
	}
	if snap.Revision != baseRev {
		t.Fatalf("refused rename moved the revision from %d to %d", baseRev, snap.Revision)
	}

	pb, _ := syncedPeer(t, g, "b")
	if _, err := pb.EnsureSystemTag(ctx, placementTag); err != nil {
		t.Fatalf("peer EnsureSystemTag: %v", err)
	}
	if _, err := pb.Rename(ctx, sys.Tag.ID, "lane:cpu"); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("peer rename was not refused with ErrTagSystem: %v", err)
	}
	if got := byID(t, keeperSnap(t, g).Tags, sys.Tag.ID); got.Name != placementTag {
		t.Fatalf("peer rename reached the keeper and changed the tag: %+v", got)
	}
	if view := pb.View(); byNameAll(view.Tags, "lane:cpu") != 0 {
		t.Fatalf("refused rename left a tag on the peer; view = %+v", view.Tags)
	}
}

// TestSystemTag_ReparentRefused covers a system tag being refused under a
// parent, keeping it at the root, on the keeper and seen the same on a peer.
func TestSystemTag_ReparentRefused(t *testing.T) {
	g := newRig(t)
	sys, err := g.keeper.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("EnsureSystemTag: %v", err)
	}
	parent, err := g.keeper.Create(ctx, CreateInput{Name: "lane"})
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	baseRev := keeperSnap(t, g).Revision

	if _, err := g.keeper.Reparent(ctx, sys.Tag.ID, parent.Tag.ID); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("keeper re-parent was not refused with ErrTagSystem: %v", err)
	}
	got := byID(t, keeperSnap(t, g).Tags, sys.Tag.ID)
	if got.ParentID != "" || !got.System {
		t.Fatalf("refused re-parent changed the tag: %+v", got)
	}
	if snap := keeperSnap(t, g); snap.Revision != baseRev {
		t.Fatalf("refused re-parent moved the revision from %d to %d", baseRev, snap.Revision)
	}

	// Moving it back to the root is still refused, because it is the system
	// tag's own parent that may change nothing.
	if _, err := g.keeper.Reparent(ctx, sys.Tag.ID, ""); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("keeper re-parent to the root was not refused: %v", err)
	}

	pb, _ := syncedPeer(t, g, "b")
	if _, err := pb.EnsureSystemTag(ctx, placementTag); err != nil {
		t.Fatalf("peer EnsureSystemTag: %v", err)
	}
	pid := byName(t, pb.View().Tags, "lane", 0).ID
	if _, err := pb.Reparent(ctx, sys.Tag.ID, pid); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("peer re-parent was not refused with ErrTagSystem: %v", err)
	}
	if got := byID(t, keeperSnap(t, g).Tags, sys.Tag.ID); got.ParentID != "" {
		t.Fatalf("peer re-parent moved the tag: %+v", got)
	}
}

// TestSystemTag_DeleteRefused covers a system tag being undeletable, whether
// or not things carry it, on the keeper and on a peer.
func TestSystemTag_DeleteRefused(t *testing.T) {
	g := newRig(t)
	sys, err := g.keeper.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("EnsureSystemTag: %v", err)
	}

	if _, err := g.keeper.Delete(ctx, sys.Tag.ID); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("keeper delete was not refused with ErrTagSystem: %v", err)
	}
	if _, ok := tagByName(keeperSnap(t, g).Tags, placementTag); !ok {
		t.Fatalf("refused delete removed the tag")
	}

	// A tag in use is still refused as a system tag, not as in use: the
	// origin, not the load, is the reason it stays.
	g.kstore.assign("auto-1", sys.Tag.ID, false)
	if _, err := g.keeper.Delete(ctx, sys.Tag.ID); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("keeper delete of an assigned system tag was not refused with ErrTagSystem: %v", err)
	}
	if _, ok := tagByName(keeperSnap(t, g).Tags, placementTag); !ok {
		t.Fatalf("refused delete removed the tag it still held assignments on")
	}

	pb, st := syncedPeer(t, g, "b")
	if _, err := pb.EnsureSystemTag(ctx, placementTag); err != nil {
		t.Fatalf("peer EnsureSystemTag: %v", err)
	}
	st.assign("auto-2", sys.Tag.ID, false)
	if _, err := pb.Delete(ctx, sys.Tag.ID); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("peer delete was not refused with ErrTagSystem: %v", err)
	}
	if _, ok := tagByName(pb.View().Tags, placementTag); !ok {
		t.Fatalf("refused delete removed the tag on the peer")
	}
	if _, err := g.keeper.Delete(ctx, sys.Tag.ID); !errors.Is(err, ErrTagSystem) {
		t.Fatalf("keeper still refused a second time for the wrong reason: %v", err)
	}
}

// TestSystemTag_OriginKeptThroughPeerMerge covers the offline path: a peer
// that ensures the placement while the keeper is down keeps a pending system
// tag, and when it reaches a keeper that already holds the placement the
// pending tag merges into that tag. The keeper's tag, and nothing else, is
// what survives; its origin stays the system origin.
func TestSystemTag_OriginKeptThroughPeerMerge(t *testing.T) {
	g := newRig(t)
	pa, _ := g.peer("a", nil)
	if err := pa.Sync(ctx); err != nil {
		t.Fatalf("first Sync of a: %v", err)
	}

	// The keeper holds the placement from a, and a names that tag.
	keeperSide, err := pa.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("EnsureSystemTag: %v", err)
	}
	kept := byID(t, keeperSnap(t, g).Tags, keeperSide.Tag.ID)
	if !kept.System {
		t.Fatalf("keeper tag lost its system origin: %+v", kept)
	}
	stays := kept
	if got := keeperSnap(t, g); len(tagNamed(got.Tags, placementTag)) != 1 {
		t.Fatalf("keeper holds %d tags named %q, want one", len(tagNamed(got.Tags, placementTag)), placementTag)
	}

	// b loses the keeper and ensures the same placement offline.
	pb, sb := g.peer("b", nil)
	g.links["b"].setDown(true)
	pending, err := pb.EnsureSystemTag(ctx, placementTag)
	if err != nil {
		t.Fatalf("offline EnsureSystemTag on b: %v", err)
	}
	if !pending.Tag.System || !pending.Tag.PendingSync {
		t.Fatalf("b's pending tag is not a system tag: %+v", pending.Tag)
	}
	pendingID := pending.Tag.ID
	if pendingID == keeperSide.Tag.ID {
		t.Fatal("b made the keeper's id, not its own pending tag")
	}
	sb.assign("auto-b", pendingID, true)

	// The keeper returns and b pushes its pending tag.
	g.links["b"].setDown(false)
	if err := pb.Sync(ctx); err != nil {
		t.Fatalf("Sync after the keeper returned: %v", err)
	}

	// b's copy holds exactly the keeper's tag.
	view := pb.View()
	if n := len(tagNamed(view.Tags, placementTag)); n != 1 {
		t.Fatalf("b holds %d tags named %q after the merge, want one", n, placementTag)
	}
	merged := byName(t, view.Tags, placementTag, 0)
	if merged.ID != keeperSide.Tag.ID {
		t.Fatalf("b kept %q, want the keeper's %q; view = %+v", merged.ID, keeperSide.Tag.ID, view.Tags)
	}
	if !merged.System {
		t.Fatalf("the merged tag on b lost its system origin: %+v", merged)
	}
	for _, tag := range view.Tags {
		if tag.ID == pendingID {
			t.Fatalf("b still holds the replaced tag %q; view = %+v", pendingID, view.Tags)
		}
	}

	// b's work now sits on the keeper's tag, and the keeper's tag is exactly
	// the one it had before the merge.
	tags, primary := sb.tagsOf("auto-b")
	if len(tags) != 1 || tags[0] != keeperSide.Tag.ID || primary != keeperSide.Tag.ID {
		t.Fatalf("b's assignment did not move to the keeper's tag: tags = %v, primary = %q", tags, primary)
	}
	after := keeperSnap(t, g)
	got := byID(t, after.Tags, keeperSide.Tag.ID)
	if got.Name != stays.Name || got.ID != stays.ID || !got.System || got.UpdatedAt.Before(stays.UpdatedAt) {
		t.Fatalf("the merge changed the keeper's tag: before = %+v, after = %+v", stays, got)
	}
	if n := len(tagNamed(after.Tags, placementTag)); n != 1 {
		t.Fatalf("the merge made a second tag on the keeper; tags = %+v", after.Tags)
	}

	// Both peers agree on the one tag now.
	_ = pa.Sync(ctx)
	sharedPA := byName(t, pa.View().Tags, placementTag, 0)
	if sharedPA.ID != keeperSide.Tag.ID || !sharedPA.System {
		t.Fatalf("a's copy does not hold the shared tag: %+v", sharedPA)
	}
}

// --- small helpers for this file -------------------------------------------

// keeperSnap returns the keeper's current snapshot, failing the test if the
// keeper cannot produce one.
func keeperSnap(t *testing.T, g *rig) Snapshot {
	t.Helper()
	snap, err := g.keeper.KeeperSnapshot()
	if err != nil {
		t.Fatalf("KeeperSnapshot: %v", err)
	}
	return snap
}

// byID returns the tag with the given id from tags, failing the test otherwise.
func byID(t *testing.T, tags []Tag, id string) Tag {
	t.Helper()
	for _, tag := range tags {
		if tag.ID == id {
			return tag
		}
	}
	t.Fatalf("no tag %q in %+v", id, tags)
	return Tag{}
}

// byName returns the wantNth tag named name from tags, failing the test if
// fewer than wantNth+1 match.
func byName(t *testing.T, tags []Tag, name string, wantNth int) Tag {
	t.Helper()
	seen := 0
	for _, tag := range tags {
		if tag.Name == name {
			if seen == wantNth {
				return tag
			}
			seen++
		}
	}
	t.Fatalf("tag %q not in %+v", name, tags)
	return Tag{}
}

// byNameAll counts tags named name in tags.
func byNameAll(tags []Tag, name string) int {
	n := 0
	for _, tag := range tags {
		if tag.Name == name {
			n++
		}
	}
	return n
}

// countNamed is byNameAll with a failure hook, for messages that need context.
func countNamed(t *testing.T, tags []Tag, name string) int {
	t.Helper()
	return byNameAll(tags, name)
}

// tagNamed collects the tags named name in tags.
func tagNamed(tags []Tag, name string) []Tag {
	var out []Tag
	for _, tag := range tags {
		if tag.Name == name {
			out = append(out, tag)
		}
	}
	return out
}
