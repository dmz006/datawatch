package session

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// QueueItemState represents the state of a queue item.
type QueueItemState string

const (
	QueueStatePending  QueueItemState = "pending"
	QueueStateClaimed  QueueItemState = "claimed"
	QueueStateComplete QueueItemState = "complete"
	QueueStateFailed   QueueItemState = "failed"
)

// QueueItem is a single work item in the queue.
type QueueItem struct {
	ID          string         `json:"id"`
	Role        string         `json:"role"`                     // role that can claim this item
	Payload     map[string]any `json:"payload"`                  // arbitrary JSON payload
	State       QueueItemState `json:"state"`
	ClaimedBy   string         `json:"claimed_by,omitempty"`     // session FullID that claimed it
	LeaseExpiry time.Time      `json:"lease_expiry,omitempty"`   // when the claim expires
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	CompletedAt time.Time      `json:"completed_at,omitempty"`
	Error       string         `json:"error,omitempty"`          // set on fail
	Result      map[string]any `json:"result,omitempty"`         // set on complete
}

// QueueStore is a durable work queue backed by a JSON file.
// After every mutation the state is persisted atomically.
type QueueStore struct {
	mu    sync.Mutex
	path  string
	items map[string]*QueueItem
}

// NewQueueStore creates or loads a queue store from path.
func NewQueueStore(path string) (*QueueStore, error) {
	s := &QueueStore{
		path:  path,
		items: make(map[string]*QueueItem),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *QueueStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read queue store: %w", err)
	}
	var items []*QueueItem
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("parse queue store: %w", err)
	}
	for _, it := range items {
		s.items[it.ID] = it
	}
	return nil
}

func (s *QueueStore) save() error {
	items := make([]*QueueItem, 0, len(s.items))
	for _, it := range s.items {
		items = append(items, it)
	}
	// stable order by creation time
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// Push adds a new item to the queue and returns it.
func (s *QueueStore) Push(role string, payload map[string]any) (*QueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := randomID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if payload == nil {
		payload = map[string]any{}
	}
	it := &QueueItem{
		ID:        id,
		Role:      role,
		Payload:   payload,
		State:     QueueStatePending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.items[id] = it
	return it, s.save()
}

// Claim atomically claims the oldest pending item for the given role.
// Returns (nil, nil) if no items are available.
// claimedBy is the session FullID. leaseSeconds is how long the claim is held.
func (s *QueueStore) Claim(role, claimedBy string, leaseSeconds int) (*QueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if leaseSeconds <= 0 {
		leaseSeconds = 300
	}
	now := time.Now()

	// Collect candidates: pending items for this role where lease is not active.
	var candidates []*QueueItem
	for _, it := range s.items {
		if it.Role != role {
			continue
		}
		if it.State == QueueStatePending {
			candidates = append(candidates, it)
		} else if it.State == QueueStateClaimed && !it.LeaseExpiry.IsZero() && now.After(it.LeaseExpiry) {
			// expired lease — eligible for re-claim
			candidates = append(candidates, it)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	// oldest first
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
	})
	it := candidates[0]
	it.State = QueueStateClaimed
	it.ClaimedBy = claimedBy
	it.LeaseExpiry = now.Add(time.Duration(leaseSeconds) * time.Second)
	it.UpdatedAt = now
	return it, s.save()
}

// Complete marks a claimed item as done.
func (s *QueueStore) Complete(id string, result map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.items[id]
	if !ok {
		return fmt.Errorf("queue item %q not found", id)
	}
	if it.State != QueueStateClaimed {
		return fmt.Errorf("queue item %q is not claimed (state=%s)", id, it.State)
	}
	now := time.Now()
	it.State = QueueStateComplete
	it.CompletedAt = now
	it.UpdatedAt = now
	if result != nil {
		it.Result = result
	}
	it.LeaseExpiry = time.Time{}
	return s.save()
}

// Fail marks a claimed item as failed.
func (s *QueueStore) Fail(id, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	it, ok := s.items[id]
	if !ok {
		return fmt.Errorf("queue item %q not found", id)
	}
	if it.State != QueueStateClaimed {
		return fmt.Errorf("queue item %q is not claimed (state=%s)", id, it.State)
	}
	now := time.Now()
	it.State = QueueStateFailed
	it.Error = errMsg
	it.UpdatedAt = now
	it.LeaseExpiry = time.Time{}
	return s.save()
}

// List returns all items, optionally filtered by role and/or state.
// Empty role or state means "any".
func (s *QueueStore) List(role, state string) []*QueueItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []*QueueItem
	for _, it := range s.items {
		if role != "" && it.Role != role {
			continue
		}
		if state != "" && string(it.State) != state {
			continue
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Get returns an item by ID.
func (s *QueueStore) Get(id string) (*QueueItem, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	return it, ok
}

// ExpireLeases checks for claimed items past their lease expiry and resets
// them to pending. Returns the number of items reset.
func (s *QueueStore) ExpireLeases() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	n := 0
	for _, it := range s.items {
		if it.State == QueueStateClaimed && !it.LeaseExpiry.IsZero() && now.After(it.LeaseExpiry) {
			it.State = QueueStatePending
			it.ClaimedBy = ""
			it.LeaseExpiry = time.Time{}
			it.UpdatedAt = now
			n++
		}
	}
	if n > 0 {
		_ = s.save()
	}
	return n
}

// Delete removes an item (any state).
func (s *QueueStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.items[id]; !ok {
		return fmt.Errorf("queue item %q not found", id)
	}
	delete(s.items, id)
	return s.save()
}
