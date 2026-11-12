package tagregistry

import (
	"context"
	"errors"
	"testing"
)

func TestKeeperFieldConflictKeepsKeeperValue(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	made, err := g.keeper.Create(ctx, CreateInput{Name: "Topic"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	base := g.keeper.View().Revision
	first, err := g.keeper.ApplyPeerWrite(Write{Kind: WriteRename, TagID: made.Tag.ID, Name: "first", BaseRevision: base})
	if err != nil || first.Conflict || first.Tag.Name != "first" {
		t.Fatalf("first rename = %+v err %v, want applied as first", first, err)
	}
	second, err := g.keeper.ApplyPeerWrite(Write{Kind: WriteRename, TagID: made.Tag.ID, Name: "second", BaseRevision: base})
	if err != nil {
		t.Fatalf("second rename: %v", err)
	}
	if !second.Conflict || second.ConflictField != "name" {
		t.Errorf("second rename conflict = %v field %q, want conflict on name", second.Conflict, second.ConflictField)
	}
	if second.Tag.Name != "first" {
		t.Errorf("second rename returned %q, want the keeper value \"first\"", second.Tag.Name)
	}
	if got, _ := tagByName(g.keeper.View().Tags, "first"); got.ID != made.Tag.ID {
		t.Errorf("keeper does not hold the first rename")
	}
}

func TestKeeperDifferentFieldsBothApply(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	made, err := g.keeper.Create(ctx, CreateInput{Name: "Topic"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	p, _ := g.peer("a", nil)
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := g.keeper.Rename(ctx, made.Tag.ID, "Renamed on keeper"); err != nil {
		t.Fatalf("keeper Rename: %v", err)
	}
	res, err := p.Recolor(ctx, made.Tag.ID, "green")
	if err != nil {
		t.Fatalf("peer Recolor: %v", err)
	}
	if res.Conflict {
		t.Errorf("recolor reported a conflict on a field nobody else changed")
	}
	got, ok := tagByName(g.keeper.View().Tags, "Renamed on keeper")
	if !ok || got.Color != "green" {
		t.Errorf("keeper tag = %+v, want the keeper rename and the peer recolor", got)
	}
}

func TestKeeperPeerRenameConflictIsAdopted(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	made, err := g.keeper.Create(ctx, CreateInput{Name: "Topic"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	p, _ := g.peer("a", nil)
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := g.keeper.Rename(ctx, made.Tag.ID, "Keeper name"); err != nil {
		t.Fatalf("keeper Rename: %v", err)
	}
	res, err := p.Rename(ctx, made.Tag.ID, "Peer name")
	if err != nil {
		t.Fatalf("peer Rename: %v", err)
	}
	if !res.Conflict || res.ConflictField != "name" {
		t.Errorf("peer rename conflict = %v field %q, want conflict on name", res.Conflict, res.ConflictField)
	}
	if res.Tag.Name != "Keeper name" {
		t.Errorf("peer rename returned %q, want the keeper value", res.Tag.Name)
	}
	local, ok := tagByName(p.View().Tags, "Keeper name")
	if !ok || local.ID != made.Tag.ID {
		t.Errorf("peer copy after conflict = %+v, want the keeper value", local)
	}
}

func TestKeeperRefusesCycle(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	root, err := g.keeper.Create(ctx, CreateInput{Name: "Root"})
	if err != nil {
		t.Fatalf("Create Root: %v", err)
	}
	child, err := g.keeper.Create(ctx, CreateInput{Name: "Child", ParentID: root.Tag.ID})
	if err != nil {
		t.Fatalf("Create Child: %v", err)
	}
	p, _ := g.peer("a", nil)
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := p.Reparent(ctx, root.Tag.ID, child.Tag.ID); !errors.Is(err, ErrTagCycle) {
		t.Fatalf("cycle reparent = %v, want ErrTagCycle", err)
	}
	if got, _ := tagByName(g.keeper.View().Tags, "Root"); got.ParentID != "" {
		t.Errorf("keeper root moved to %q despite the refusal", got.ParentID)
	}
	if p.View().Stale {
		t.Errorf("a refused cycle marked the peer stale; the keeper did answer")
	}
}

func TestKeeperRefusesWritesOnPeerRole(t *testing.T) {
	g := newRig(t)
	p, _ := g.peer("a", nil)
	if _, err := p.ApplyPeerWrite(Write{Kind: WriteCreate, TagID: "x", Name: "x"}); !errors.Is(err, ErrNotKeeper) {
		t.Errorf("ApplyPeerWrite on a peer = %v, want ErrNotKeeper", err)
	}
	if _, err := p.KeeperSnapshot(); !errors.Is(err, ErrNotKeeper) {
		t.Errorf("KeeperSnapshot on a peer = %v, want ErrNotKeeper", err)
	}
}

func TestKeeperRetriedCreateIsIdempotent(t *testing.T) {
	g := newRig(t)
	w := Write{Kind: WriteCreate, TagID: "fixed-id", Name: "Dup", OriginNode: "a", Pending: false}
	first, err := g.keeper.ApplyPeerWrite(w)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := g.keeper.ApplyPeerWrite(w)
	if err != nil {
		t.Fatalf("retried create: %v", err)
	}
	if second.Revision != first.Revision {
		t.Errorf("retry moved the revision from %d to %d", first.Revision, second.Revision)
	}
	if n := len(g.keeper.View().Tags); n != 1 {
		t.Errorf("keeper holds %d tags after a retried create, want 1", n)
	}
}

func TestKeeperSnapshotCarriesOriginNode(t *testing.T) {
	g := newRig(t)
	if _, err := g.keeper.ApplyPeerWrite(Write{Kind: WriteCreate, TagID: "t1", Name: "Made elsewhere", OriginNode: "a"}); err != nil {
		t.Fatalf("ApplyPeerWrite: %v", err)
	}
	snap, err := g.keeper.KeeperSnapshot()
	if err != nil {
		t.Fatalf("KeeperSnapshot: %v", err)
	}
	if len(snap.Tags) != 1 || snap.Tags[0].OriginNode != "a" {
		t.Errorf("snapshot tags = %+v, want one tag with origin a", snap.Tags)
	}
	if snap.Revision != g.keeper.View().Revision {
		t.Errorf("snapshot revision = %d, keeper = %d", snap.Revision, g.keeper.View().Revision)
	}
}

func TestKeeperRejectsInvalidWrites(t *testing.T) {
	g := newRig(t)
	cases := []struct {
		name string
		w    Write
		want error
	}{
		{"unknown kind", Write{Kind: "bogus", TagID: "x"}, ErrTagInvalid},
		{"create without name", Write{Kind: WriteCreate, TagID: "x", Name: " "}, ErrTagInvalid},
		{"create without id", Write{Kind: WriteCreate, Name: "x"}, ErrTagInvalid},
		{"create under missing parent", Write{Kind: WriteCreate, TagID: "x", Name: "x", ParentID: "nope"}, ErrTagNotFound},
		{"rename missing", Write{Kind: WriteRename, TagID: "nope", Name: "x"}, ErrTagNotFound},
		{"rename to empty", Write{Kind: WriteRename, TagID: "nope", Name: ""}, ErrTagInvalid},
		{"recolor missing", Write{Kind: WriteRecolor, TagID: "nope"}, ErrTagNotFound},
		{"describe missing", Write{Kind: WriteDescribe, TagID: "nope"}, ErrTagNotFound},
		{"delete missing", Write{Kind: WriteDelete, TagID: "nope"}, ErrTagNotFound},
	}
	for _, c := range cases {
		if _, err := g.keeper.ApplyPeerWrite(c.w); !errors.Is(err, c.want) {
			t.Errorf("%s: error = %v, want %v", c.name, err, c.want)
		}
	}
	if n := len(g.keeper.View().Tags); n != 0 {
		t.Errorf("refused writes left %d tags behind", n)
	}
}
