// Readable names for raw capacity-holder ids. The ledger (internal/capacity)
// keys leases and waiters by raw ids — a bare task id, "verify:<taskID>" or
// "plan:<prdID>" (and operator sessions under their session id) — which are
// opaque to an operator. This package resolves those raw ids into operator-
// readable sentences ("Automate X / Wire up Y (bbbbbbbb)") using titles from
// the PRD store, behind a short-lived snapshot cache so a polled API call
// resolves instantly without re-reading the store per holder.
package autonomous

import (
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// HolderKind classifies a capacity holder.
type HolderKind string

const (
	// HolderKindTask: a bare task id holding its worker lease.
	HolderKindTask HolderKind = "task"
	// HolderKindVerifier: "verify:<taskID>", the task being verified.
	HolderKindVerifier HolderKind = "verifier"
	// HolderKindDecompose: "plan:<prdID>" (and "decompose:<prdID>"), the PRD's planner.
	HolderKindDecompose HolderKind = "decompose"
	// HolderKindSession: anything else, normally an operator session id.
	HolderKindSession HolderKind = "session"
)

// titleLimit is the per-title rune budget in HolderName.Readable; longer
// titles are truncated in the middle with an ellipsis (Full keeps the text).
const titleLimit = 60

// defaultTitleTTL is how long a resolved name (and a snapshot refresh) is
// re-used before the store is consulted again.
const defaultTitleTTL = 3 * time.Second

// defaultTitleCacheCap hard-bounds the cache; when it is reached the cache
// is dropped wholesale rather than grown (entries are cheap to re-derive).
const defaultTitleCacheCap = 256

// TaskRef is one task's title plus the PRD it belongs to.
type TaskRef struct {
	PRDID string
	Title string
}

// TitleSnapshot is a single point-in-time view of the titles the resolver
// needs, taken once from the PRD store per refresh.
type TitleSnapshot struct {
	PRDs  map[string]string  // prdID -> title
	Tasks map[string]TaskRef // taskID -> {PRDID, title}
}

// TitleLookupFn returns one TitleSnapshot. It runs under the store's own
// lock, never under the manager's mutex, so Resolve can be called while the
// manager is busy and never block on resolving.
type TitleLookupFn func() TitleSnapshot

// HolderName is the resolved, operator-readable form of a raw holder id.
type HolderName struct {
	ID        string     // raw holder id, unchanged
	Kind      HolderKind // task | verifier | decompose | session
	TaskTitle string     // the task's full title ("", when there is none)
	PRDID     string     // the PRD the holder belongs to (when known)
	PRDTitle  string     // the PRD's full title ("" when unknown)
	ShortID   string     // first 8 chars of the task or PRD id
	Readable  string     // short form; per-title limit, middle ellipsis
	Full      string     // same sentence, titles never truncated
}

// TitleResolverOptions tune a TitleResolver.
type TitleResolverOptions struct {
	// Lookup supplies the title snapshot; nil gives every holder an
	// "(untitled …)" readable.
	Lookup TitleLookupFn
	// SessionName gives a known name for a holder that is a bare session id
	// ("" or nil = unknown, the short id is shown instead).
	SessionName func(id string) string
	// Clock is the time source for cache expiry (tests inject a fake).
	Clock capacity.Clock
	// TTL is the cache life (default defaultTitleTTL).
	TTL time.Duration
	// MaxEntries hard-bounds the cache (default defaultTitleCacheCap).
	MaxEntries int
}

type realTitleClock struct{}

func (realTitleClock) Now() time.Time                         { return time.Now() }
func (realTitleClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type titleCacheEntry struct {
	name   HolderName
	issued time.Time
}

// TitleResolver resolves raw holder ids to HolderName values. Safe for
// concurrent use.
type TitleResolver struct {
	lookup      TitleLookupFn
	sessionName func(string) string
	clock       capacity.Clock
	ttl         time.Duration
	maxEntries  int

	mu       sync.Mutex
	cache    map[string]titleCacheEntry
	snapshot TitleSnapshot
	snapAt   time.Time
}

// NewTitleResolver builds a resolver over an injected title lookup.
func NewTitleResolver(opts TitleResolverOptions) *TitleResolver {
	if opts.Lookup == nil {
		empty := TitleSnapshot{PRDs: map[string]string{}, Tasks: map[string]TaskRef{}}
		opts.Lookup = func() TitleSnapshot { return empty }
	}
	if opts.Clock == nil {
		opts.Clock = realTitleClock{}
	}
	if opts.TTL <= 0 {
		opts.TTL = defaultTitleTTL
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = defaultTitleCacheCap
	}
	return &TitleResolver{
		lookup:      opts.Lookup,
		sessionName: opts.SessionName,
		clock:       opts.Clock,
		ttl:         opts.TTL,
		maxEntries:  opts.MaxEntries,
		cache:       map[string]titleCacheEntry{},
	}
}

// NewTitleResolverFor wires a resolver over the manager's PRD store. The
// lookup copies titles under the store lock; it never takes m.mu, so the
// resolver never blocks on (or blocks) the manager.
func (m *Manager) NewTitleResolverFor(opts TitleResolverOptions) *TitleResolver {
	if opts.Lookup == nil {
		opts.Lookup = m.TitlesLookup
	}
	return NewTitleResolver(opts)
}

// TitlesLookup returns a TitleSnapshot of the PRD and task titles stored on
// the manager's PRDs (the same source TaskVisibility reads).
func (m *Manager) TitlesLookup() TitleSnapshot {
	s := TitleSnapshot{PRDs: map[string]string{}, Tasks: map[string]TaskRef{}}
	if m.store == nil {
		return s
	}
	for _, p := range m.store.ListPRDs() {
		m.readLocked(func() {
			s.PRDs[p.ID] = p.Title
			for i := range p.Story {
				for j := range p.Story[i].Tasks {
					t := &p.Story[i].Tasks[j]
					s.Tasks[t.ID] = TaskRef{PRDID: p.ID, Title: t.Title}
				}
			}
		})
	}
	return s
}

// Resolve turns one raw holder id into a HolderName. Cache hits within the
// TTL (and snapshot refreshes within the TTL) cost no store lookups at all,
// so a poll that resolves many holders re-reads the store at most once.
func (r *TitleResolver) Resolve(holderID string) HolderName {
	r.mu.Lock()
	now := r.clock.Now()
	if e, ok := r.cache[holderID]; ok && now.Sub(e.issued) < r.ttl {
		name := e.name
		r.mu.Unlock()
		return name
	}
	snapValid := !r.snapAt.IsZero() && now.Sub(r.snapAt) < r.ttl
	r.mu.Unlock()

	if !snapValid {
		snap := r.lookup() // store lock only; the manager's mutex is not held
		r.mu.Lock()
		r.snapshot = snap
		r.snapAt = now
		r.mu.Unlock()
	}

	r.mu.Lock()
	snap := r.snapshot
	r.mu.Unlock()

	name := resolveHolder(holderID, snap, r.sessionName)

	r.mu.Lock()
	if len(r.cache) >= r.maxEntries {
		r.cache = map[string]titleCacheEntry{}
	}
	r.cache[holderID] = titleCacheEntry{name: name, issued: r.clock.Now()}
	r.mu.Unlock()
	return name
}

// resolveHolder maps one holder id against one snapshot. Pure, so testable
// without a store.
func resolveHolder(holderID string, snap TitleSnapshot, sessionName func(string) string) HolderName {
	// "verify:<taskID>": the task in verification holds one slot under this id.
	if rest, ok := strings.CutPrefix(holderID, "verify:"); ok {
		return verifierHolder(rest, snap)
	}
	// "plan:<prdID>" / "decompose:<prdID>": the PRD's planner lease.
	for _, prefix := range []string{"plan:", "decompose:"} {
		if rest, ok := strings.CutPrefix(holderID, prefix); ok {
			return decomposeHolder(rest, snap)
		}
	}
	// A bare id that is a known task: the worker lease.
	if ref, ok := snap.Tasks[holderID]; ok {
		return taskHolder(holderID, ref, snap)
	}
	// Everything else is a session (operator, one-shot decompose, …).
	name := ""
	if sessionName != nil {
		name = sessionName(holderID)
	}
	short := shortID(holderID)
	if name == "" {
		name = short
	}
	return HolderName{
		ID: holderID, Kind: HolderKindSession, ShortID: short,
		Readable: name, Full: name,
	}
}

func taskHolder(taskID string, ref TaskRef, snap TitleSnapshot) HolderName {
	short := shortID(taskID)
	prdTitle := snap.PRDs[ref.PRDID]
	return HolderName{
		ID: taskID, Kind: HolderKindTask,
		TaskTitle: ref.Title, PRDID: ref.PRDID,
		PRDTitle: prdTitle,
		ShortID:  short,
		Readable: taskHolderReadable(short, ref.Title, ref.PRDID, prdTitle),
		Full:     taskHolderFull(short, ref.Title, ref.PRDID, prdTitle),
	}
}

func verifierHolder(taskID string, snap TitleSnapshot) HolderName {
	short := shortID(taskID)
	ref := snap.Tasks[taskID] // zero value when unknown
	prdTitle := ""
	if ref.PRDID != "" {
		prdTitle = snap.PRDs[ref.PRDID]
	}
	var readable, full string
	if strings.TrimSpace(ref.Title) == "" {
		full = "Verifying: (untitled " + short + ")"
		readable = full
	} else {
		full = "Verifying: " + ref.Title + " (" + short + ")"
		readable = "Verifying: " + shortTitle(ref.Title) + " (" + short + ")"
	}
	return HolderName{
		ID: "verify:" + taskID, Kind: HolderKindVerifier,
		TaskTitle: ref.Title,
		PRDID:     ref.PRDID, PRDTitle: prdTitle,
		ShortID:  short,
		Readable: readable,
		Full:     full,
	}
}

func decomposeHolder(prdID string, snap TitleSnapshot) HolderName {
	short := shortID(prdID)
	var readable, full string
	if strings.TrimSpace(snap.PRDs[prdID]) == "" {
		full = "Planning: (untitled " + short + ")"
		readable = full
	} else {
		full = "Planning: " + snap.PRDs[prdID] + " (" + short + ")"
		readable = "Planning: " + shortTitle(snap.PRDs[prdID]) + " (" + short + ")"
	}
	return HolderName{
		ID: prdID, Kind: HolderKindDecompose,
		PRDID: prdID, PRDTitle: snap.PRDs[prdID],
		ShortID:  short,
		Readable: readable,
		Full:     full,
	}
}

// taskHolderReadable words the task line under the per-title limit:
// "<PRD title> / <task title> (<short id>)".
func taskHolderReadable(short, taskTitle, prdID, prdTitle string) string {
	prdPart := shortTitle(titleOrUntitled(prdTitle, shortID(prdID)))
	if strings.TrimSpace(taskTitle) == "" {
		return prdPart + " / (untitled " + short + ")"
	}
	return prdPart + " / " + shortTitle(taskTitle) + " (" + short + ")"
}

func taskHolderFull(short, taskTitle, prdID, prdTitle string) string {
	prdPart := titleOrUntitled(prdTitle, shortID(prdID))
	if strings.TrimSpace(taskTitle) == "" {
		return prdPart + " / (untitled " + short + ")"
	}
	return prdPart + " / " + taskTitle + " (" + short + ")"
}

// titleOrUntitled falls back to "(untitled <short id>)" when a title is
// missing (deleted or archived PRD, unknown task).
func titleOrUntitled(title, shortID string) string {
	if strings.TrimSpace(title) == "" {
		return "(untitled " + shortID + ")"
	}
	return title
}

// shortTitle applies the per-title limit with a middle ellipsis.
func shortTitle(title string) string {
	r := []rune(title)
	if len(r) <= titleLimit {
		return title
	}
	head := (titleLimit - 1) / 2
	tail := titleLimit - 1 - head
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// shortID is the first 8 chars of an id.
func shortID(id string) string {
	r := []rune(id)
	if len(r) < 8 {
		return id
	}
	return string(r[:8])
}
