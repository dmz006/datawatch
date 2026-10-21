// BL427 — Persisted subscription store (Phase 3). Modelled on
// internal/session/discussion_subs.go (mutex, atomic temp-file+rename
// write, idempotent load of a missing file), with the file encrypted
// via internal/secfile exactly like
// internal/server/channel_routing.go. The design:
// docs/plans/2026-10-13-prd-lifecycle-events.md.

package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// SubscriptionStore persists lifecycle event subscriptions in a JSON
// file under the data dir, encrypted with the daemon's secfile key
// (nil key falls back to a plaintext file — matches secfile's
// semantics).
type SubscriptionStore struct {
	mu      sync.RWMutex
	path    string
	encKey  []byte
	subs    map[string]Subscription
	changed func() // resync hook (engine re-attaches sinks after mutations)
}

// NewSubscriptionStore loads path, or starts an empty store when the
// file does not exist (idempotent first boot). A corrupt file (failed
// decrypt or invalid JSON) is an error — silently starting empty would
// lose subscriptions and surface as "delivery randomly stopped".
func NewSubscriptionStore(path string, encKey []byte) (*SubscriptionStore, error) {
	s := &SubscriptionStore{path: path, encKey: encKey, subs: make(map[string]Subscription)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// OnChanged registers a callback fired (outside the store lock) after
// every successful mutation, so an engine can resync its sinks. Replace
// it with nil to detach.
func (s *SubscriptionStore) OnChanged(fn func()) {
	s.mu.Lock()
	s.changed = fn
	s.mu.Unlock()
}

func (s *SubscriptionStore) load() error {
	data, err := secfile.ReadFile(s.path, s.encKey)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // empty store is valid
		}
		return fmt.Errorf("read lifecycle subscription store: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var subs []Subscription
	if err := json.Unmarshal(data, &subs); err != nil {
		return fmt.Errorf("lifecycle subscription store %s is corrupt (undecryptable or invalid JSON): %w", s.path, err)
	}
	for _, sub := range subs {
		if sub.ID == "" {
			continue // never trust a record without an identity
		}
		s.subs[sub.ID] = sub
	}
	return nil
}

// saveLocked persists the current set. The caller holds s.mu.
func (s *SubscriptionStore) saveLocked() error {
	subs := make([]Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	sort.Slice(subs, func(i, j int) bool {
		if subs[i].CreatedAt.Equal(subs[j].CreatedAt) {
			return subs[i].ID < subs[j].ID
		}
		return subs[i].CreatedAt.Before(subs[j].CreatedAt)
	})
	data, err := json.MarshalIndent(subs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lifecycle subscription store: %w", err)
	}
	return secfile.WriteFile(s.path, data, 0o600, s.encKey)
}

// mutate runs f under the write lock; on nil error it persists and
// fires the changed callback (outside the lock, so a callback may call
// back into the store without deadlocking).
func (s *SubscriptionStore) mutate(f func(now time.Time) error) error {
	var (
		mutErr   error
		callback func()
	)
	s.mu.Lock()
	mutErr = f(time.Now())
	if mutErr == nil {
		mutErr = s.saveLocked()
		if mutErr == nil {
			callback = s.changed
		}
	}
	s.mu.Unlock()
	if callback != nil {
		callback()
	}
	return mutErr
}

// Add validates sub, assigns an ID (and CreatedAt if unset), persists
// it and returns the stored record.
func (s *SubscriptionStore) Add(sub Subscription) (Subscription, error) {
	var out Subscription
	err := s.mutate(func(now time.Time) error {
		if sub.ID == "" {
			sub.ID = newSubscriptionID()
		}
		if _, exists := s.subs[sub.ID]; exists {
			return fmt.Errorf("subscription ID %s already exists", sub.ID)
		}
		if sub.CreatedAt.IsZero() {
			sub.CreatedAt = now
		}
		if err := ValidateSubscription(sub, now); err != nil {
			return err
		}
		s.subs[sub.ID] = sub
		out = sub
		return nil
	})
	return out, err
}

// List returns copies of all subscriptions, optionally narrowed to one
// owner. An empty owner means all.
func (s *SubscriptionStore) List(owner string) []Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		if owner != "" && sub.Owner != owner {
			continue
		}
		out = append(out, sub)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Get returns a copy of the subscription with id.
func (s *SubscriptionStore) Get(id string) (Subscription, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sub, ok := s.subs[id]
	return sub, ok
}

// Delete removes the subscription with id. Not-found is an error so a
// CLI/REST caller can distinguish a typo from a successful delete.
func (s *SubscriptionStore) Delete(id string) error {
	return s.mutate(func(time.Time) error {
		if _, ok := s.subs[id]; !ok {
			return fmt.Errorf("subscription %s not found", id)
		}
		delete(s.subs, id)
		return nil
	})
}

// DeleteExpired removes every subscription whose ExpiresAt is at or
// before now and returns the removed IDs (for logging/audit). It is a
// no-op (nil, nil) when nothing is expired.
func (s *SubscriptionStore) DeleteExpired(now time.Time) ([]string, error) {
	var (
		removed  []string
		mutErr   error
		callback func()
	)
	s.mu.Lock()
	for id, sub := range s.subs {
		if sub.ExpiresAt != nil && !sub.ExpiresAt.After(now) {
			delete(s.subs, id)
			removed = append(removed, id)
		}
	}
	if len(removed) > 0 {
		mutErr = s.saveLocked()
		if mutErr == nil {
			sort.Strings(removed)
			callback = s.changed
		}
	}
	s.mu.Unlock()
	if callback != nil {
		callback()
	}
	return removed, mutErr
}

func newSubscriptionID() string { return "sub-" + newLifecycleEventID() }
