// Pending items — named pieces of work that have no Automaton yet. An
// Automation member can point at one; it holds the Automation at its cursor
// until an Automaton is attached. The daemon owns the registry (see
// pending_item_store.go); nothing here reads a document.

package autonomous

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// PendingItemState is the lifecycle state of a pending item.
type PendingItemState string

const (
	// PendingItemWaiting: no Automaton is attached yet.
	PendingItemWaiting PendingItemState = "waiting"
	// PendingItemAttached: an Automaton was attached (AutomatonID is set).
	PendingItemAttached PendingItemState = "attached"
	// PendingItemDone: the work is finished; this state is final.
	PendingItemDone PendingItemState = "done"
)

// PendingItemSource says where an item was recorded.
type PendingItemSource string

const (
	// PendingItemSourceInternal: created through the daemon's own surfaces.
	PendingItemSourceInternal PendingItemSource = "internal"
	// PendingItemSourceFile: mirrored from a configured file registry.
	PendingItemSourceFile PendingItemSource = "file"
)

// pendingItemTitleMax is the longest title, in characters.
const pendingItemTitleMax = 300

// PendingItem is one registry entry.
type PendingItem struct {
	ID          string            `json:"id"`
	Ref         string            `json:"ref,omitempty"`
	Title       string            `json:"title"`
	State       PendingItemState  `json:"state"`
	Source      PendingItemSource `json:"source"`
	AutomatonID string            `json:"automaton_id,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	AttachedAt  *time.Time        `json:"attached_at,omitempty"`
	DoneAt      *time.Time        `json:"done_at,omitempty"`
}

// PendingItemFilter narrows List; an empty field matches everything.
type PendingItemFilter struct {
	State  PendingItemState
	Source PendingItemSource
}

// validPendingItemState reports whether s is a known state.
func validPendingItemState(s PendingItemState) bool {
	switch s {
	case PendingItemWaiting, PendingItemAttached, PendingItemDone:
		return true
	}
	return false
}

// validPendingItemSource reports whether s is a known source.
func validPendingItemSource(s PendingItemSource) bool {
	return s == PendingItemSourceInternal || s == PendingItemSourceFile
}

// pendingItemMoveAllowed reports whether from -> to is a permitted move:
// waiting to attached, waiting to done, attached to done. Done is final and
// a move to the same state is not a move.
func pendingItemMoveAllowed(from, to PendingItemState) bool {
	switch from {
	case PendingItemWaiting:
		return to == PendingItemAttached || to == PendingItemDone
	case PendingItemAttached:
		return to == PendingItemDone
	}
	return false
}

// checkPendingItemMove returns a 409 error when from -> to is not allowed.
func checkPendingItemMove(from, to PendingItemState) error {
	if from == to {
		return nil
	}
	if !validPendingItemState(to) {
		return fmt.Errorf("400: unknown pending item state %q", to)
	}
	if !pendingItemMoveAllowed(from, to) {
		if from == PendingItemDone {
			return fmt.Errorf("409: pending item is done and cannot change state (asked for %s)", to)
		}
		return fmt.Errorf("409: pending item cannot move from %s to %s", from, to)
	}
	return nil
}

// normalizePendingItemTitle trims the title and enforces the rules: required
// and at most pendingItemTitleMax characters.
func normalizePendingItemTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("400: pending item title is required")
	}
	if n := utf8.RuneCountInString(title); n > pendingItemTitleMax {
		return "", fmt.Errorf("400: pending item title is %d characters, the limit is %d", n, pendingItemTitleMax)
	}
	return title, nil
}

// clonePendingItem returns a deep copy.
func clonePendingItem(p *PendingItem) *PendingItem {
	c := *p
	if p.AttachedAt != nil {
		t := *p.AttachedAt
		c.AttachedAt = &t
	}
	if p.DoneAt != nil {
		t := *p.DoneAt
		c.DoneAt = &t
	}
	return &c
}

// CreatePendingItem records a waiting item in the registry and audits it.
// ref is optional; the item's source is internal.
func (m *Manager) CreatePendingItem(title, ref, actor string) (*PendingItem, error) {
	if m.pendingItems == nil {
		return nil, fmt.Errorf("500: pending item registry is not available")
	}
	p, err := m.pendingItems.Create(title, ref, PendingItemSourceInternal)
	if err != nil {
		return nil, err
	}
	if actor == "" {
		actor = "operator"
	}
	m.controlAudit("pending_item_create", map[string]any{
		"pending_item_id": p.ID,
		"ref":             p.Ref,
		"title":           p.Title,
		"actor":           actor,
	})
	return p, nil
}

// GetPendingItem returns the pending item with id.
func (m *Manager) GetPendingItem(id string) (*PendingItem, error) {
	if m.pendingItems == nil {
		return nil, fmt.Errorf("500: pending item registry is not available")
	}
	p, ok := m.pendingItems.Get(id)
	if !ok {
		return nil, fmt.Errorf("404: pending item %s not found", id)
	}
	return p, nil
}

// ListPendingItems returns the pending items matching f, oldest first.
func (m *Manager) ListPendingItems(f PendingItemFilter) []*PendingItem {
	if m.pendingItems == nil {
		return nil
	}
	return m.pendingItems.List(f)
}
