package tagregistry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// syncedPeer returns a peer that has pulled the keeper's registry once.
func syncedPeer(t *testing.T, g *rig, node string) (*Registry, *fakeStore) {
	t.Helper()
	p, st := g.peer(node, nil)
	if err := p.Sync(context.Background()); err != nil {
		t.Fatalf("first Sync of %s: %v", node, err)
	}
	return p, st
}

func TestStaleMarksPeerWhenKeeperGoesAway(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	if _, err := g.keeper.Create(ctx, CreateInput{Name: "Work"}); err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	p, _ := syncedPeer(t, g, "a")
	stamp := p.View().LastSyncedAt
	g.clock.Advance(time.Minute)

	g.links["a"].setDown(true)
	if err := p.Sync(ctx); !errors.Is(err, ErrKeeperUnreachable) {
		t.Fatalf("Sync with keeper down = %v, want unreachable", err)
	}
	v := p.View()
	if !v.Stale {
		t.Errorf("peer is not stale after the keeper went away")
	}
	if !v.LastSyncedAt.Equal(stamp) {
		t.Errorf("LastSyncedAt moved to %v, want the last good sync %v", v.LastSyncedAt, stamp)
	}
	if _, ok := tagByName(v.Tags, "Work"); !ok {
		t.Errorf("stale peer dropped its last copy")
	}
	if !strings.Contains(v.LastError, "unreachable") {
		t.Errorf("LastError = %q, want it to say the keeper is unreachable", v.LastError)
	}
}

func TestStaleClearsWhenKeeperReturns(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	if _, err := g.keeper.Create(ctx, CreateInput{Name: "Work"}); err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	p, _ := syncedPeer(t, g, "a")
	stamp := p.View().LastSyncedAt
	g.links["a"].setDown(true)
	_ = p.Sync(ctx)
	g.clock.Advance(2 * time.Minute)

	g.links["a"].setDown(false)
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("Sync after the keeper returned: %v", err)
	}
	v := p.View()
	if v.Stale {
		t.Errorf("peer still stale after the keeper returned")
	}
	if !v.LastSyncedAt.After(stamp) {
		t.Errorf("LastSyncedAt = %v, want it after %v", v.LastSyncedAt, stamp)
	}
	if v.LastError != "" {
		t.Errorf("LastError = %q after a clean sync, want empty", v.LastError)
	}
}

func TestStaleRefusesEditsAndDelete(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	made, err := g.keeper.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	p, _ := syncedPeer(t, g, "a")
	g.links["a"].setDown(true)
	_ = p.Sync(ctx)

	refusals := map[string]func() error{
		"rename": func() error { _, err := p.Rename(ctx, made.Tag.ID, "Other"); return err },
		"re-parent": func() error {
			_, err := p.Reparent(ctx, made.Tag.ID, "")
			return err
		},
		"recolor":  func() error { _, err := p.Recolor(ctx, made.Tag.ID, "red"); return err },
		"describe": func() error { _, err := p.Describe(ctx, made.Tag.ID, "text"); return err },
		"delete":   func() error { _, err := p.Delete(ctx, made.Tag.ID); return err },
	}
	for name, call := range refusals {
		err := call()
		if !errors.Is(err, ErrKeeperUnreachable) {
			t.Errorf("%s while stale = %v, want ErrKeeperUnreachable", name, err)
			continue
		}
		if !strings.Contains(err.Error(), "last copy") {
			t.Errorf("%s refusal %q does not say the last copy is shown", name, err)
		}
	}
	if _, ok := tagByName(g.keeper.View().Tags, "Work"); !ok {
		t.Errorf("a refused delete reached the keeper")
	}
}

func TestStaleFreshPeerRefusesEditsBeforeFirstSync(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	p, _ := g.peer("a", nil)
	if !p.View().Stale {
		t.Errorf("a peer that never synced is not stale")
	}
	made, err := p.Create(ctx, CreateInput{Name: "Offline"})
	if err != nil {
		t.Fatalf("create before any sync: %v", err)
	}
	if _, err := p.Rename(ctx, made.Tag.ID, "Other"); !errors.Is(err, ErrKeeperUnreachable) {
		t.Errorf("rename before any sync = %v, want ErrKeeperUnreachable", err)
	}
	if _, err := p.Rename(ctx, "no-such-tag", "Other"); !errors.Is(err, ErrTagNotFound) {
		t.Errorf("rename of an unknown tag = %v, want ErrTagNotFound", err)
	}
}

func TestStaleCreateIsPendingSync(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	p, st := syncedPeer(t, g, "a")
	g.links["a"].setDown(true)
	_ = p.Sync(ctx)

	res, err := p.Create(ctx, CreateInput{Name: "Offline", Color: "teal"})
	if err != nil {
		t.Fatalf("Create while stale: %v", err)
	}
	if !res.Tag.PendingSync {
		t.Errorf("offline create not marked pending sync")
	}
	got, ok := tagByName(p.View().Tags, "Offline")
	if !ok || !got.PendingSync || got.OriginNode != "a" {
		t.Errorf("peer copy = %+v, want the pending tag from node a", got)
	}
	if _, ok := tagByName(st.tags, "Offline"); !ok {
		t.Errorf("pending tag missing from the store")
	}
	if _, ok := tagByName(g.keeper.View().Tags, "Offline"); ok {
		t.Errorf("pending tag reached the keeper before the push")
	}
}

func TestStaleCreateThenPush(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	p, _ := syncedPeer(t, g, "a")
	g.links["a"].setDown(true)
	_ = p.Sync(ctx)
	made, err := p.Create(ctx, CreateInput{Name: "Offline"})
	if err != nil {
		t.Fatalf("Create while stale: %v", err)
	}

	g.links["a"].setDown(false)
	if err := p.Sync(ctx); err != nil {
		t.Fatalf("Sync after the keeper returned: %v", err)
	}
	got, ok := tagByName(p.View().Tags, "Offline")
	if !ok || got.PendingSync || got.ID != made.Tag.ID {
		t.Errorf("peer copy after push = %+v, want the same tag, no longer pending", got)
	}
	snap, err := g.keeper.KeeperSnapshot()
	if err != nil {
		t.Fatalf("KeeperSnapshot: %v", err)
	}
	kept, ok := tagByName(snap.Tags, "Offline")
	if !ok || kept.ID != made.Tag.ID || kept.OriginNode != "a" {
		t.Errorf("keeper after push = %+v, want the pushed tag from node a", kept)
	}
}

func TestStaleAssignedTagCannotBeDeletedOnPeer(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	made, err := g.keeper.Create(ctx, CreateInput{Name: "Work"})
	if err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	p, st := syncedPeer(t, g, "a")
	st.assign("auto-1", made.Tag.ID, false)
	if _, err := p.Delete(ctx, made.Tag.ID); !errors.Is(err, ErrTagInUse) {
		t.Errorf("peer delete of an assigned tag = %v, want ErrTagInUse", err)
	}
	if _, ok := tagByName(g.keeper.View().Tags, "Work"); !ok {
		t.Errorf("refused peer delete still reached the keeper")
	}
}

func TestStaleRejectedPushStaysPending(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	parent, err := g.keeper.Create(ctx, CreateInput{Name: "Parent"})
	if err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	p, _ := syncedPeer(t, g, "a")
	g.links["a"].setDown(true)
	_ = p.Sync(ctx)
	child, err := p.Create(ctx, CreateInput{Name: "Child", ParentID: parent.Tag.ID})
	if err != nil {
		t.Fatalf("Create under a stale parent: %v", err)
	}

	// The keeper removes the parent while the child waits. The next push is refused.
	g.links["a"].setDown(false)
	if _, err := g.keeper.Delete(ctx, parent.Tag.ID); err != nil {
		t.Fatalf("keeper Delete: %v", err)
	}
	err = p.Sync(ctx)
	if !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("first Sync = %v, want the refused push to report ErrTagNotFound", err)
	}
	got, ok := tagByName(p.View().Tags, "Child")
	if !ok || !got.PendingSync {
		t.Fatalf("refused child = %+v, want it kept pending", got)
	}
	if got.ParentID != "" {
		t.Errorf("child parent = %q after its parent was removed, want root", got.ParentID)
	}

	if err := p.Sync(ctx); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	pushed, ok := tagByName(p.View().Tags, "Child")
	if !ok || pushed.PendingSync || pushed.ID != child.Tag.ID {
		t.Errorf("child after retry = %+v, want it pushed", pushed)
	}
}
