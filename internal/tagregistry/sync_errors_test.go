package tagregistry

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// refusingKeeper is a KeeperClient that answers every call with a refusal, as a
// daemon that does not keep the registry does.
type refusingKeeper struct{}

func (refusingKeeper) FetchSnapshot(ctx context.Context) (Snapshot, error) {
	return Snapshot{}, ErrNotKeeper
}

func (refusingKeeper) Write(ctx context.Context, w Write) (WriteResult, error) {
	return WriteResult{}, ErrNotKeeper
}

func TestRegistryOnlineCreateRefusalIsNotPending(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	p, _ := syncedPeer(t, g, "a")
	_, err := p.Create(ctx, CreateInput{Name: "   "})
	if !errors.Is(err, ErrTagInvalid) {
		t.Fatalf("online create with an empty name = %v, want ErrTagInvalid", err)
	}
	for _, tag := range p.View().Tags {
		if tag.PendingSync {
			t.Errorf("a refused online create left pending tag %q", tag.Name)
		}
	}
}

func TestRegistryRefusedSnapshotIsRecorded(t *testing.T) {
	ctx := context.Background()
	g := newRig(t)
	p := mustNew(t, Options{
		NodeName: "a", KeeperName: "keeper", Store: newFakeStore(),
		Keeper: refusingKeeper{}, Clock: g.clock, NewID: g.ids,
	})
	err := p.Sync(ctx)
	if !errors.Is(err, ErrNotKeeper) {
		t.Fatalf("Sync against a refusing keeper = %v, want ErrNotKeeper", err)
	}
	v := p.View()
	if !strings.Contains(v.LastError, "does not keep") {
		t.Errorf("LastError = %q, want the refusal text", v.LastError)
	}
	if !v.LastSyncedAt.IsZero() {
		t.Errorf("a refused snapshot set LastSyncedAt to %v", v.LastSyncedAt)
	}
}

func TestRegistrySyncCacheFailureKeepsLastCopy(t *testing.T) {
	g := newRig(t)
	ctx := context.Background()
	if _, err := g.keeper.Create(ctx, CreateInput{Name: "Work"}); err != nil {
		t.Fatalf("keeper Create: %v", err)
	}
	boom := errors.New("disk full")
	cache := &fakeCache{}
	p, _ := g.peer("a", cache)
	cache.failSave = boom
	if err := p.Sync(ctx); !errors.Is(err, boom) {
		t.Fatalf("Sync with a failing cache = %v, want the save error", err)
	}
	if n := len(p.View().Tags); n != 0 {
		t.Errorf("failed sync left %d tags in the copy, want the last copy (none)", n)
	}
	if p.View().LastError == "" {
		t.Errorf("LastError is empty after a failed sync")
	}
}
