package tagregistry

import (
	"context"
	"time"
)

// Store is the local tag store on this daemon. The autonomous manager adapts
// its own tag and assignment records to it.
type Store interface {
	// ListTags returns every tag record the store holds.
	ListTags() ([]Tag, error)
	// ApplySnapshot replaces the tag records with tags.
	ApplySnapshot(tags []Tag) error
	// AssignmentCount returns how many Automata and sessions carry the tag.
	AssignmentCount(tagID string) (int, error)
	// RewriteAssignments moves every assignment of fromID to toID. Each item
	// keeps its tags unique, and a primary tag that names fromID moves too. It
	// returns the number of items changed.
	RewriteAssignments(fromID, toID string) (int, error)
	// RemoveAssignments drops every assignment of tagID, and clears a primary
	// tag that names it. It returns the number of items changed.
	RemoveAssignments(tagID string) (int, error)
}

// KeeperClient reaches the keeper daemon over federation. A transport failure
// is reported as an error matching ErrKeeperUnreachable. A refusal by the
// keeper, such as a missing tag or a cycle, is returned as the matching
// sentinel error.
type KeeperClient interface {
	// FetchSnapshot returns the keeper's registry.
	FetchSnapshot(ctx context.Context) (Snapshot, error)
	// Write forwards one edit to the keeper.
	Write(ctx context.Context, w Write) (WriteResult, error)
}

// Cache keeps the last copy of a peer's registry between restarts. The
// registry encodes its state and the cache stores the bytes. FileCache is the
// implementation that writes the autonomous data directory.
type Cache interface {
	// Load returns the saved copy. ok is false when nothing has been saved.
	Load() (data []byte, ok bool, err error)
	// Save replaces the saved copy.
	Save(data []byte) error
}

// Clock gives the current time and a timer for the sync interval.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// realClock is the Clock that reads the system time.
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
