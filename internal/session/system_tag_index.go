package session

import (
	"sync"
	"time"
)

// buildSystemTagIndex builds, in one pass over the active and archived sessions,
// a map from Automaton ID (Session.PRDID) to the system tags of that
// Automaton's sessions. The active sessions are read from the live store; the
// archive is decoded once, with an extra function that collects tags and
// never keeps the session, so no archived record is retained in memory.
//
// Entries keep repeats and are in no particular order: the consumer
// (autonomous.Manager.InheritedTagsForPRD) de-duplicates and sorts. Sessions
// with an empty PRDID are skipped. It does no caching: each call reads the
// store again. Readers go through the cache (cachedSystemTagIndex).
func (m *Manager) buildSystemTagIndex() map[string][]string {
	idx := map[string][]string{}
	collect := func(sess *Session) {
		if sess == nil || sess.PRDID == "" {
			return
		}
		idx[sess.PRDID] = append(idx[sess.PRDID], sess.SystemTags...)
	}
	for _, sess := range m.store.List() {
		collect(sess)
	}
	m.ListArchivedSessions(SessionFilter{}, func(sess *Session) bool {
		collect(sess)
		return false // collect only; nothing is returned or retained
	}, "", time.Time{}, 0)
	return idx
}

// defaultInheritedTagsCacheTTL is the cache lifetime used when no positive TTL
// has been set.
const defaultInheritedTagsCacheTTL = 15 * time.Second

// systemTagIndexCache holds the built index, when it was built, and the
// single-flight state. Its mutex is dedicated: a build never holds m.mu, and
// the cache lock is never held while building.
type systemTagIndexCache struct {
	mu       sync.Mutex
	idx      map[string][]string
	builtAt  time.Time
	ttl      time.Duration    // 0 = default, <0 = caching off
	clock    func() time.Time // injectable clock; nil = time.Now
	inflight *systemTagIndexBuild
	storeGen uint64 // Store.tagGeneration() when idx was built
	gen      uint64 // bumped by invalidation so a stale in-flight build is not stored
}

type systemTagIndexBuild struct {
	done chan struct{}
	idx  map[string][]string
}

func (c *systemTagIndexCache) nowLocked() time.Time {
	if c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

// SetInheritedTagsCacheTTL sets the lifetime of the cached system tag index.
// A positive d is the lifetime, zero means the default (15 seconds), and a
// negative d turns caching off: every call builds, though concurrent callers
// still share one in-flight build.
func (m *Manager) SetInheritedTagsCacheTTL(d time.Duration) {
	m.tagIdx.mu.Lock()
	defer m.tagIdx.mu.Unlock()
	m.tagIdx.ttl = d
}

// InheritedTagsCacheTTL reports the currently configured lifetime of the
// cached system tag index: the set value when one was, or the default
// (15 seconds) when none was. A negative result means caching is off.
func (m *Manager) InheritedTagsCacheTTL() time.Duration {
	m.tagIdx.mu.Lock()
	defer m.tagIdx.mu.Unlock()
	if m.tagIdx.ttl == 0 {
		return defaultInheritedTagsCacheTTL
	}
	return m.tagIdx.ttl
}

// InvalidateSystemTagIndex drops the cached index so the next read rebuilds.
func (m *Manager) InvalidateSystemTagIndex() {
	m.tagIdx.mu.Lock()
	defer m.tagIdx.mu.Unlock()
	m.tagIdx.idx = nil
	m.tagIdx.gen++
}

// cachedSystemTagIndex returns the shared cached index, building it when the
// cache is empty or stale. Concurrent callers on a stale cache share one
// build. The returned map is owned by the cache and must not be mutated.
func (m *Manager) cachedSystemTagIndex() map[string][]string {
	c := &m.tagIdx
	c.mu.Lock()
	ttl := c.ttl
	if ttl == 0 {
		ttl = defaultInheritedTagsCacheTTL
	}
	storeGen := m.store.tagGeneration()
	if ttl > 0 && c.idx != nil && c.storeGen == storeGen && c.nowLocked().Sub(c.builtAt) < ttl {
		idx := c.idx
		c.mu.Unlock()
		return idx
	}
	if b := c.inflight; b != nil {
		c.mu.Unlock()
		<-b.done
		return b.idx
	}
	b := &systemTagIndexBuild{done: make(chan struct{})}
	c.inflight = b
	gen := c.gen
	c.mu.Unlock()

	b.idx = m.buildSystemTagIndex()

	c.mu.Lock()
	if ttl > 0 && c.gen == gen {
		c.idx = b.idx
		c.builtAt = c.nowLocked()
		c.storeGen = storeGen // read before the build, so a change during it leaves the cache stale
	}
	c.inflight = nil
	c.mu.Unlock()
	close(b.done)
	return b.idx
}

// SystemTagIndexSnapshot returns the current index as a read-only lookup:
// prdID to the system tags of that Automaton's sessions (repeats kept, no
// order). Each lookup returns a fresh copy, so callers cannot mutate the
// cache. A list handler calls this once per list call.
func (m *Manager) SystemTagIndexSnapshot() func(prdID string) []string {
	idx := m.cachedSystemTagIndex()
	return func(prdID string) []string {
		return append([]string{}, idx[prdID]...)
	}
}

// SystemTagSnapshot implements autonomous.SystemTagSnapshotter: it is
// SystemTagIndexSnapshot under the name the reader side looks for.
func (m *Manager) SystemTagSnapshot() func(prdID string) []string {
	return m.SystemTagIndexSnapshot()
}
