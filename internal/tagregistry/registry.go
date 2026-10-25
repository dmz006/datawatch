package tagregistry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultSyncEvery is the sync interval used when Options.SyncEvery is unset.
// It matches the default of autonomous.tags.registry_sync_seconds.
const DefaultSyncEvery = 30 * time.Second

// Options configures a Registry.
type Options struct {
	// NodeName is this daemon's federation name. It is the origin node of the
	// tags this daemon creates.
	NodeName string
	// KeeperName is autonomous.tags.registry_keeper. See RoleFor.
	KeeperName string
	// SyncEvery is the interval between peer syncs. Zero means DefaultSyncEvery.
	SyncEvery time.Duration
	// Store is the local tag store. Required.
	Store Store
	// Keeper reaches the keeper. Required for a peer, unused otherwise.
	Keeper KeeperClient
	// Cache keeps the last copy between restarts. Nil keeps no copy.
	Cache Cache
	// Clock supplies the time. Nil uses the system clock.
	Clock Clock
	// NewID makes a tag or note id. Nil uses random ids.
	NewID func() string
}

// state is the registry as this daemon holds it. It is what the cache stores.
type state struct {
	Revision  int64                       `json:"revision"`
	Tags      []Tag                       `json:"tags"`
	FieldRevs map[string]map[string]int64 `json:"field_revs,omitempty"`
	Notes     []Note                      `json:"notes,omitempty"`
	Dismissed []string                    `json:"dismissed,omitempty"`
	// LastSyncedAt is when the keeper last answered with a snapshot.
	LastSyncedAt time.Time `json:"last_synced_at,omitempty"`
}

// Registry is one daemon's view of the shared tag registry. It is safe for
// concurrent use.
type Registry struct {
	role   Role
	node   string
	keeper string
	every  time.Duration
	store  Store
	client KeeperClient
	cache  Cache
	clock  Clock
	newID  func() string
	notify chan struct{}

	// opMu serializes writes and syncs. It is held across keeper calls.
	opMu sync.Mutex
	// mu guards st, reachable and lastErr. It is never held across a keeper call.
	mu        sync.Mutex
	st        state
	reachable bool
	lastErr   string
}

// New builds a registry for the role the options give, and loads the cache
// when one was saved. A cache that cannot be read or decoded is an error.
func New(opts Options) (*Registry, error) {
	if opts.Store == nil {
		return nil, errors.New("tag registry: store is required")
	}
	role := RoleFor(opts.NodeName, opts.KeeperName)
	if role == RolePeer && opts.Keeper == nil {
		return nil, fmt.Errorf("tag registry: peer %q needs a keeper client", opts.NodeName)
	}
	r := &Registry{
		role:      role,
		node:      opts.NodeName,
		keeper:    opts.KeeperName,
		every:     opts.SyncEvery,
		store:     opts.Store,
		client:    opts.Keeper,
		cache:     opts.Cache,
		clock:     opts.Clock,
		newID:     opts.NewID,
		notify:    make(chan struct{}, 1),
		reachable: role != RolePeer,
	}
	if r.every <= 0 {
		r.every = DefaultSyncEvery
	}
	if r.clock == nil {
		r.clock = realClock{}
	}
	if r.newID == nil {
		r.newID = randomID
	}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

// load reads the cache when one exists and makes the store match it. With no
// cache the state starts from the store's tags.
func (r *Registry) load() error {
	if r.cache != nil {
		data, ok, err := r.cache.Load()
		if err != nil {
			return fmt.Errorf("tag registry: read cache: %w", err)
		}
		if ok {
			if err := json.Unmarshal(data, &r.st); err != nil {
				return fmt.Errorf("tag registry: decode cache: %w", err)
			}
			return r.store.ApplySnapshot(copyTags(r.st.Tags))
		}
	}
	tags, err := r.store.ListTags()
	if err != nil {
		return fmt.Errorf("tag registry: list tags: %w", err)
	}
	r.st.Tags = tags
	return nil
}

// Role reports what this daemon does for the registry.
func (r *Registry) Role() Role { return r.role }

// View returns the registry as this daemon shows it.
func (r *Registry) View() View {
	r.mu.Lock()
	defer r.mu.Unlock()
	return View{
		Role:         r.role,
		Keeper:       r.keeper,
		Revision:     r.st.Revision,
		Tags:         copyTags(r.st.Tags),
		Notes:        copyNotes(r.st.Notes),
		Stale:        !r.reachable,
		LastSyncedAt: r.st.LastSyncedAt,
		LastError:    r.lastErr,
	}
}

// Notify asks the sync loop to sync now. It is called on a tag event and never
// blocks.
func (r *Registry) Notify() {
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

// Run syncs a peer now, then every sync interval and whenever Notify is
// called, until ctx is done. A failed sync is recorded and shows as stale in
// View, so Run does not stop on it. On a standalone or keeper daemon Run returns
// at once.
func (r *Registry) Run(ctx context.Context) {
	if r.role != RolePeer {
		return
	}
	for {
		_ = r.Sync(ctx)
		select {
		case <-ctx.Done():
			return
		case <-r.clock.After(r.every):
		case <-r.notify:
		}
	}
}

// mutateLocked runs change against the state and saves the result. When the
// change or the save fails, the state goes back to what it was, so a failed
// write leaves no trace in this daemon's copy. Callers hold mu.
func (r *Registry) mutateLocked(change func() error) error {
	prev := r.st.clone()
	if err := change(); err != nil {
		r.st = prev
		return err
	}
	if err := r.commitLocked(); err != nil {
		r.st = prev
		return err
	}
	return nil
}

// clone returns a deep copy of the state, so a failed change can be undone.
func (s state) clone() state {
	out := s
	out.Tags = copyTags(s.Tags)
	out.Notes = copyNotes(s.Notes)
	out.Dismissed = append([]string(nil), s.Dismissed...)
	if s.FieldRevs != nil {
		out.FieldRevs = make(map[string]map[string]int64, len(s.FieldRevs))
		for id, stamps := range s.FieldRevs {
			cp := make(map[string]int64, len(stamps))
			for f, rev := range stamps {
				cp[f] = rev
			}
			out.FieldRevs[id] = cp
		}
	}
	return out
}

// commitLocked mirrors the tags to the store and saves the state. Callers hold mu.
func (r *Registry) commitLocked() error {
	if err := r.store.ApplySnapshot(copyTags(r.st.Tags)); err != nil {
		return fmt.Errorf("tag registry: apply tags to store: %w", err)
	}
	return r.saveLocked()
}

// saveLocked writes the state to the cache, if there is one. Callers hold mu.
func (r *Registry) saveLocked() error {
	if r.cache == nil {
		return nil
	}
	data, err := json.Marshal(r.st)
	if err != nil {
		return fmt.Errorf("tag registry: encode cache: %w", err)
	}
	if err := r.cache.Save(data); err != nil {
		return fmt.Errorf("tag registry: write cache: %w", err)
	}
	return nil
}

// setReachable records whether the keeper answered the last call.
func (r *Registry) setReachable(ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reachable = ok
}

// recordErr keeps the text of the last sync error for View.
func (r *Registry) recordErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr = ""
	if err != nil {
		r.lastErr = err.Error()
	}
}

// isReachable reports whether the keeper answered the last call.
func (r *Registry) isReachable() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reachable
}

// revision returns the registry revision this daemon last saw.
func (r *Registry) revision() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.st.Revision
}

// DismissNote removes a note from this daemon's view. A dismissed note does not
// come back when the keeper's snapshot is applied again.
func (r *Registry) DismissNote(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, n := range r.st.Notes {
		if n.ID == id {
			return r.mutateLocked(func() error {
				r.st.Notes = append(r.st.Notes[:i], r.st.Notes[i+1:]...)
				r.st.Dismissed = append(r.st.Dismissed, id)
				return nil
			})
		}
	}
	return fmt.Errorf("%w: %q", ErrNoteNotFound, id)
}

// addNoteLocked adds a note unless it is already held or was dismissed. Callers hold mu.
func (r *Registry) addNoteLocked(n Note) {
	for _, have := range r.st.Notes {
		if have.ID == n.ID {
			return
		}
	}
	for _, id := range r.st.Dismissed {
		if id == n.ID {
			return
		}
	}
	r.st.Notes = append(r.st.Notes, n)
}

// randomID returns a random 16-hex-digit id.
func randomID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("tag registry: random id: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// copyTags returns a copy of a tag list, so callers cannot change the state.
func copyTags(in []Tag) []Tag {
	if in == nil {
		return nil
	}
	return append([]Tag(nil), in...)
}

// copyNotes returns a copy of a note list.
func copyNotes(in []Note) []Note {
	if in == nil {
		return nil
	}
	return append([]Note(nil), in...)
}
