// Cache and cheap inputs for the project roll-up read.
//
// The roll-up is built from the Automaton summary index (Store.SummaryEntries
// and Store.Revision), never from full records. Three pieces keep a read cheap:
//
//   - rollupCache keeps the finished, encoded roll-up of a project, keyed by
//     (project, window days, store revision, a hash of the configured projects
//     including arcs, a hash of the Automation list). A repeat read with the
//     same key does no work. Any write to an Automaton bumps the store
//     revision, so the next read rebuilds. Concurrent cold reads of one key
//     share one build.
//   - rollupPointsMemo keeps the per-Automaton points of one window for one
//     store revision, so the cost report engine does not run on every read.
//   - rollupProofCache keeps the read-only merge proof per work branch and
//     Automaton record version, so no read runs git for a proof already known.
//
// Everything here is read-only: no session is signalled and no record is
// written.

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/costreport"
)

const (
	// rollupCacheMaxEntries bounds the number of cached roll-ups.
	rollupCacheMaxEntries = 64
	// rollupPointsMaxAge bounds how long memoized points are served when the
	// store revision does not move: spend can accrue without an Automaton write.
	rollupPointsMaxAge = 30 * time.Second
	// rollupPointsMaxWindows bounds the windows kept in the points memo.
	rollupPointsMaxWindows = 8
	// rollupProofNegativeTTL is how long "not merged" is remembered.
	rollupProofNegativeTTL = 60 * time.Second
	// rollupProofMaxEntries bounds the merge proof cache.
	rollupProofMaxEntries = 4096
	// rollupProofTimeout bounds one merge proof.
	rollupProofTimeout = 10 * time.Second
)

// rollupState holds the roll-up cache and its memos. The zero value is ready.
type rollupState struct {
	once   sync.Once
	cache  *rollupCache
	points *rollupPointsMemo
	proofs *rollupProofCache
}

func (r *rollupState) init() {
	r.once.Do(func() {
		r.cache = newRollupCache(rollupCacheMaxEntries)
		r.points = &rollupPointsMemo{byWindow: map[int]rollupPointsRec{}}
		r.proofs = newRollupProofCache(rollupProofMaxEntries)
	})
}

// rollupCacheKey identifies one finished roll-up.
type rollupCacheKey struct {
	project    string
	windowDays int
	revision   uint64
	config     string // hash of the configured projects, arcs included
	automation string // hash of the Automation list
}

type rollupFlight struct {
	done chan struct{}
	body []byte
}

// rollupCache is a bounded cache of encoded roll-ups with a single-flight
// guard for cold reads.
type rollupCache struct {
	mu      sync.Mutex
	max     int
	entries map[rollupCacheKey][]byte
	order   []rollupCacheKey // oldest first
	flights map[rollupCacheKey]*rollupFlight
}

func newRollupCache(max int) *rollupCache {
	if max < 1 {
		max = 1
	}
	return &rollupCache{max: max, entries: map[rollupCacheKey][]byte{}, flights: map[rollupCacheKey]*rollupFlight{}}
}

// get returns the cached body for key or builds it once. hit reports whether
// the body came from the cache or from another caller's build.
func (c *rollupCache) get(key rollupCacheKey, build func() []byte) (body []byte, hit bool) {
	c.mu.Lock()
	if b, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return b, true
	}
	if f, ok := c.flights[key]; ok {
		c.mu.Unlock()
		<-f.done
		return f.body, true
	}
	f := &rollupFlight{done: make(chan struct{})}
	c.flights[key] = f
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.flights, key)
		c.mu.Unlock()
		close(f.done)
	}()
	f.body = build()
	c.mu.Lock()
	c.store(key, f.body)
	c.mu.Unlock()
	return f.body, false
}

// store adds one entry. It drops entries of the same project and window under
// an older key (they can never be read again) and enforces the bound. Caller
// holds c.mu.
func (c *rollupCache) store(key rollupCacheKey, body []byte) {
	kept := c.order[:0]
	for _, k := range c.order {
		if k.project == key.project && k.windowDays == key.windowDays && k != key {
			delete(c.entries, k)
			continue
		}
		kept = append(kept, k)
	}
	c.order = kept
	if _, ok := c.entries[key]; !ok {
		c.order = append(c.order, key)
	}
	c.entries[key] = body
	for len(c.order) > c.max {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
}

// evictProject drops every cached roll-up of one project. Called when the
// project's configuration changes or the project is removed.
func (c *rollupCache) evictProject(project string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.order[:0]
	for _, k := range c.order {
		if k.project == project {
			delete(c.entries, k)
			continue
		}
		kept = append(kept, k)
	}
	c.order = kept
}

// len reports the number of cached roll-ups.
func (c *rollupCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// evictRollupProject drops the cached roll-ups of one project. Safe on a
// server that never served a roll-up.
func (s *Server) evictRollupProject(project string) {
	s.rollup.init()
	s.rollup.cache.evictProject(project)
}

// rollupConfigHash hashes the configured projects: their directories, arcs and
// every other field. A change to any project can change membership, so the
// whole map is hashed.
func rollupConfigHash(projects map[string]config.ProjectConfigEntry) string {
	b, err := json.Marshal(projects) // map keys marshal sorted
	if err != nil {
		return "unhashable"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// rollupAutomationHash hashes what the roll-up reads from the Automation list:
// the id, version, project and member Automaton ids of each Automation.
func rollupAutomationHash(automations []*autonomous.Automation) string {
	h := sha256.New()
	ids := make([]*autonomous.Automation, 0, len(automations))
	for _, a := range automations {
		if a != nil {
			ids = append(ids, a)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].ID < ids[j].ID })
	for _, a := range ids {
		_, _ = h.Write([]byte(a.ID))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(a.Project))
		_, _ = h.Write([]byte{0})
		b, _ := json.Marshal(a.Version)
		_, _ = h.Write(b)
		for _, m := range a.Members {
			_, _ = h.Write([]byte{1})
			_, _ = h.Write([]byte(m.PRDID))
		}
		_, _ = h.Write([]byte{2})
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// projectRollupBody returns the encoded roll-up of one project for a window:
// from the cache when the key is unchanged, else built once from the summary
// index. generated_at is the time of the build.
func (s *Server) projectRollupBody(mgr *autonomous.Manager, name string, entry config.ProjectConfigEntry, windowDays int) []byte {
	s.rollup.init()
	store := mgr.Store()
	automations := mgr.ListAutomations()
	key := rollupCacheKey{
		project:    name,
		windowDays: windowDays,
		revision:   store.Revision(), // read before the entries: a newer build is never keyed older
		config:     rollupConfigHash(s.cfg.Projects),
		automation: rollupAutomationHash(automations),
	}
	body, _ := s.rollup.cache.get(key, func() []byte {
		now := time.Now()
		entries := store.SummaryEntries()
		resp := buildProjectRollup(rollupInput{
			Project:         name,
			Dir:             entry.Dir,
			Arcs:            entry.Arcs,
			Entries:         entries,
			Automations:     automations,
			ExtraSessionIDs: mgr.DecomposeSessionIDs,
			Points:          s.rollupPoints(mgr, entries, key.revision, now, windowDays),
			WindowDays:      windowDays,
			Now:             now,
			Projects:        rollupProjectDirs(s.cfg.Projects),
			ArcHeld:         rollupArcHeld(s.cfg.Projects),
			Merged:          s.rollupMergedFn(mgr),
		})
		b, err := json.Marshal(resp)
		if err != nil {
			return []byte("{}\n")
		}
		return append(b, '\n')
	})
	return body
}

// rollupPointsRec is the points of one window at one store revision.
type rollupPointsRec struct {
	revision uint64
	at       time.Time
	points   map[string]float64
}

// rollupPointsMemo memoizes the per-Automaton points by store revision.
type rollupPointsMemo struct {
	mu       sync.Mutex
	byWindow map[int]rollupPointsRec
}

func (m *rollupPointsMemo) get(window int, revision uint64, now time.Time) (map[string]float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byWindow[window]
	if !ok || rec.revision != revision || now.Sub(rec.at) > rollupPointsMaxAge {
		return nil, false
	}
	return rec.points, true
}

func (m *rollupPointsMemo) put(window int, revision uint64, now time.Time, points map[string]float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byWindow[window]; !ok && len(m.byWindow) >= rollupPointsMaxWindows {
		m.byWindow = map[int]rollupPointsRec{}
	}
	m.byWindow[window] = rollupPointsRec{revision: revision, at: now, points: points}
}

// rollupPoints returns each Automaton's points in the window. The figure is
// computed once per store revision (and window) from the spend ledger through
// the cost report engine, with session ownership taken from the summary
// entries instead of every full record. Callers must not modify the result.
func (s *Server) rollupPoints(mgr *autonomous.Manager, entries []autonomous.SummaryEntry, revision uint64, now time.Time, windowDays int) map[string]float64 {
	s.rollup.init()
	if pts, ok := s.rollup.points.get(windowDays, revision, now); ok {
		return pts
	}
	pts := s.costRollupPoints(mgr, entries, now, windowDays)
	s.rollup.points.put(windowDays, revision, now, pts)
	return pts
}

// costRollupPoints prices every Automaton in the window through the cost report
// engine and keeps only the points figure. Automata with no point figure are
// absent. Ownership comes from the entries' session ids; the figure is the
// report's estimated_usage_points for each group_by=automaton row.
func (s *Server) costRollupPoints(mgr *autonomous.Manager, entries []autonomous.SummaryEntry, now time.Time, windowDays int) map[string]float64 {
	points := map[string]float64{}
	if s.costReportLedger == nil {
		return points
	}
	owners := map[string]costOwner{}
	byAutomaton := map[string]costOwner{}
	// Entries are newest first; walk oldest first so the earliest Automaton
	// owns a shared session, as the store-order walk does.
	for i := len(entries) - 1; i >= 0; i-- {
		e := &entries[i]
		o := costOwner{automaton: e.ID, automation: e.AutomationID}
		if _, ok := byAutomaton[e.ID]; !ok {
			byAutomaton[e.ID] = o
		}
		ids := e.SessionIDs
		if live := mgr.DecomposeSessionIDs(e.ID); len(live) > 0 {
			ids = append(append([]string(nil), ids...), live...)
		}
		for _, sid := range ids {
			if sid == "" {
				continue
			}
			if _, ok := owners[sid]; !ok {
				owners[sid] = o
			}
		}
	}
	src := managerCostSource{ledger: s.costReportLedger, owner: owners, ownerByAutomaton: byAutomaton, auto: mgr}
	billing := s.resolvedBilling()
	req := costreport.Request{
		GroupBy: costreport.GroupByAutomaton,
		Since:   now.Add(-time.Duration(windowDays) * 24 * time.Hour),
		Until:   now,
	}
	// The release source is not asked for group_by=automaton, so none is passed.
	rep, err := costreport.Build(s.cfg.CostReport, req, src, nil, s.exposedRate, billing, now)
	if err != nil || rep == nil {
		return points
	}
	for _, row := range rep.Rows {
		if row.EstimatedUsagePoints != nil {
			points[row.Key] = *row.EstimatedUsagePoints
		}
	}
	return points
}

// rollupProofRec is one remembered merge proof.
type rollupProofRec struct {
	merged  bool
	at      time.Time
	expires time.Time // zero: never (a merged branch stays merged)
}

// rollupProofCache remembers merge proofs per base repository, work branch and
// Automaton record version (its last update time). A merged result is kept
// until the record changes; a not-merged result for a short bounded time.
type rollupProofCache struct {
	mu  sync.Mutex
	max int
	m   map[string]rollupProofRec
}

func newRollupProofCache(max int) *rollupProofCache {
	return &rollupProofCache{max: max, m: map[string]rollupProofRec{}}
}

func rollupProofKey(repo string, e autonomous.SummaryEntry) string {
	return repo + "\x00" + e.GitBranch + "\x00" + e.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

// resolve returns the cached proof for key, or runs prove once and keeps it.
func (c *rollupProofCache) resolve(key string, now time.Time, prove func() (bool, time.Time)) (bool, time.Time) {
	c.mu.Lock()
	if rec, ok := c.m[key]; ok && (rec.expires.IsZero() || now.Before(rec.expires)) {
		c.mu.Unlock()
		return rec.merged, rec.at
	}
	c.mu.Unlock()

	merged, at := prove()
	rec := rollupProofRec{merged: merged, at: at}
	if !merged {
		rec.expires = now.Add(rollupProofNegativeTTL)
	}
	c.mu.Lock()
	if len(c.m) >= c.max {
		for k, r := range c.m {
			if !r.expires.IsZero() && !now.Before(r.expires) {
				delete(c.m, k)
			}
		}
		for k := range c.m {
			if len(c.m) < c.max {
				break
			}
			delete(c.m, k)
		}
	}
	c.m[key] = rec
	c.mu.Unlock()
	return merged, at
}

// rollupMergedFn is the read-only merge proof the merged-member decision uses:
// the Automaton's branch against the default branch of the base repository.
// When it is merged, the time is the commit date of the proving commit. Proofs
// are remembered (see rollupProofCache), so a branch whose proof is known runs
// no git.
func (s *Server) rollupMergedFn(mgr *autonomous.Manager) func(autonomous.SummaryEntry) (bool, time.Time) {
	s.rollup.init()
	cfg := mgr.Config()
	git := autonomous.NewMergeGitRunner()
	return func(e autonomous.SummaryEntry) (bool, time.Time) {
		if e.GitBranch == "" || cfg.WorktreeBaseRepo == "" {
			return false, time.Time{}
		}
		return s.rollup.proofs.resolve(rollupProofKey(cfg.WorktreeBaseRepo, e), time.Now(), func() (bool, time.Time) {
			ctx, cancel := context.WithTimeout(context.Background(), rollupProofTimeout)
			defer cancel()
			proof := autonomous.MergedMemberProof(ctx, git, cfg.WorktreeBaseRepo, e.GitBranch)
			if !proof.Merged {
				return false, time.Time{}
			}
			out, err := git.Run(ctx, cfg.WorktreeBaseRepo, "log", "-1", "--format=%cI", proof.Commit)
			if err != nil {
				return true, time.Time{}
			}
			at, err := time.Parse(time.RFC3339, strings.TrimSpace(out))
			if err != nil {
				return true, time.Time{}
			}
			return true, at
		})
	}
}
