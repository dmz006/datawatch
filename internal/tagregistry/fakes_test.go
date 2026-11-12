package tagregistry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeItem is one Automaton or session as the fake store sees it.
type fakeItem struct {
	tags    []string
	primary string
}

// fakeStore is an in-memory Store with assignments on named items. Each fail
// field, when set, makes the matching method return that error.
type fakeStore struct {
	mu      sync.Mutex
	tags    []Tag
	items   map[string]*fakeItem
	applies int

	failList, failApply, failCount, failRewrite, failRemove error
}

func newFakeStore() *fakeStore {
	return &fakeStore{items: map[string]*fakeItem{}}
}

func (s *fakeStore) ListTags() ([]Tag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failList != nil {
		return nil, s.failList
	}
	return append([]Tag(nil), s.tags...), nil
}

func (s *fakeStore) ApplySnapshot(tags []Tag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failApply != nil {
		return s.failApply
	}
	s.applies++
	s.tags = append([]Tag(nil), tags...)
	return nil
}

// assign gives an item a tag, optionally as its primary.
func (s *fakeStore) assign(item, tagID string, primary bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.items[item]
	if it == nil {
		it = &fakeItem{}
		s.items[item] = it
	}
	it.tags = append(it.tags, tagID)
	if primary {
		it.primary = tagID
	}
}

// tagsOf returns an item's tags and primary tag.
func (s *fakeStore) tagsOf(item string) ([]string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.items[item]
	if it == nil {
		return nil, ""
	}
	return append([]string(nil), it.tags...), it.primary
}

func (s *fakeStore) AssignmentCount(tagID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failCount != nil {
		return 0, s.failCount
	}
	n := 0
	for _, it := range s.items {
		if containsString(it.tags, tagID) {
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) RewriteAssignments(fromID, toID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRewrite != nil {
		return 0, s.failRewrite
	}
	changed := 0
	for _, it := range s.items {
		if !containsString(it.tags, fromID) {
			continue
		}
		changed++
		var out []string
		for _, t := range it.tags {
			if t == fromID {
				t = toID
			}
			if !containsString(out, t) {
				out = append(out, t)
			}
		}
		it.tags = out
		if it.primary == fromID {
			it.primary = toID
		}
	}
	return changed, nil
}

func (s *fakeStore) RemoveAssignments(tagID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRemove != nil {
		return 0, s.failRemove
	}
	changed := 0
	for _, it := range s.items {
		if !containsString(it.tags, tagID) {
			continue
		}
		changed++
		var out []string
		for _, t := range it.tags {
			if t != tagID {
				out = append(out, t)
			}
		}
		it.tags = out
		if it.primary == tagID {
			it.primary = ""
		}
	}
	return changed, nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// fakeCache is an in-memory Cache that counts saves. err fails Load and
// failSave fails Save.
type fakeCache struct {
	mu       sync.Mutex
	data     []byte
	ok       bool
	saves    int
	err      error
	failSave error
}

func (c *fakeCache) Load() ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, false, c.err
	}
	return append([]byte(nil), c.data...), c.ok, nil
}

func (c *fakeCache) Save(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failSave != nil {
		return c.failSave
	}
	c.saves++
	c.data = append([]byte(nil), data...)
	c.ok = true
	return nil
}

// fakeClock is a Clock whose time moves only when Advance is called. A timer
// from After fires when the clock passes its deadline.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []clockWaiter
}

type clockWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, clockWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves the clock forward and fires the timers that are due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	var pending []clockWaiter
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
			continue
		}
		pending = append(pending, w)
	}
	c.waiters = pending
}

// fakeKeeper is a KeeperClient that talks to a keeper Registry in this process.
// While down it fails every call the way a dead network would.
type fakeKeeper struct {
	mu      sync.Mutex
	reg     *Registry
	down    bool
	fetches int
	// fetched, when set, receives one value per snapshot fetch.
	fetched chan struct{}
}

func (k *fakeKeeper) setDown(down bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.down = down
}

func (k *fakeKeeper) isDown() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.down
}

func (k *fakeKeeper) FetchSnapshot(ctx context.Context) (Snapshot, error) {
	if k.isDown() {
		return Snapshot{}, fmt.Errorf("%w: keeper is down", ErrKeeperUnreachable)
	}
	k.mu.Lock()
	k.fetches++
	fetched := k.fetched
	k.mu.Unlock()
	if fetched != nil {
		select {
		case fetched <- struct{}{}:
		default:
		}
	}
	return k.reg.KeeperSnapshot()
}

func (k *fakeKeeper) Write(ctx context.Context, w Write) (WriteResult, error) {
	if k.isDown() {
		return WriteResult{}, fmt.Errorf("%w: keeper is down", ErrKeeperUnreachable)
	}
	return k.reg.ApplyPeerWrite(w)
}

// rig is one keeper and any number of peers that share a clock and an id source.
// Each peer has its own fakeKeeper link, so one peer can lose the keeper while
// another still reaches it.
type rig struct {
	t      *testing.T
	clock  *fakeClock
	keeper *Registry
	kstore *fakeStore
	links  map[string]*fakeKeeper
	nextID int
}

// newRig builds a keeper named "keeper".
func newRig(t *testing.T) *rig {
	t.Helper()
	g := &rig{t: t, clock: newFakeClock()}
	g.kstore = newFakeStore()
	g.keeper = mustNew(t, Options{
		NodeName:   "keeper",
		KeeperName: "keeper",
		Store:      g.kstore,
		Clock:      g.clock,
		NewID:      g.ids,
	})
	g.links = map[string]*fakeKeeper{}
	return g
}

// ids returns ids that are unique across every registry of the rig, so a
// restarted peer never reuses an id it already holds.
func (g *rig) ids() string {
	g.nextID++
	return fmt.Sprintf("id-%03d", g.nextID)
}

// peer builds a peer named node with the given cache, which may be nil.
func (g *rig) peer(node string, cache Cache) (*Registry, *fakeStore) {
	g.t.Helper()
	st := newFakeStore()
	link := &fakeKeeper{reg: g.keeper}
	g.links[node] = link
	return mustNew(g.t, Options{
		NodeName:   node,
		KeeperName: "keeper",
		Store:      st,
		Keeper:     link,
		Cache:      cache,
		Clock:      g.clock,
		NewID:      g.ids,
	}), st
}

// standalone builds a daemon with no keeper.
func (g *rig) standalone(store Store) *Registry {
	g.t.Helper()
	return mustNew(g.t, Options{NodeName: "solo", Store: store, Clock: g.clock, NewID: g.ids})
}

func mustNew(t *testing.T, opts Options) *Registry {
	t.Helper()
	r, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

// tagByName returns the tag with the given name from a view, or false.
func tagByName(tags []Tag, name string) (Tag, bool) {
	for _, t := range tags {
		if t.Name == name {
			return t, true
		}
	}
	return Tag{}, false
}
