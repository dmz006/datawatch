// BL358 — Discussion push/subscribe store.
// Persists subscriptions so that when memory_discussion_write fires for a
// discussion, the written content is delivered to all subscribed sessions
// via send_input.

package session

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// DiscussionSub is a subscription: a session receives new entries from a discussion.
type DiscussionSub struct {
	DiscussionID string `json:"discussion_id"`
	SessionName  string `json:"session_name"`
}

// DiscussionSubStore persists discussion subscriptions to a JSON file.
type DiscussionSubStore struct {
	mu   sync.RWMutex
	path string
	subs []DiscussionSub
}

// NewDiscussionSubStore loads or creates a subscription store from path.
func NewDiscussionSubStore(path string) (*DiscussionSubStore, error) {
	s := &DiscussionSubStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DiscussionSubStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // empty store is valid
		}
		return fmt.Errorf("read discussion sub store: %w", err)
	}
	var subs []DiscussionSub
	if err := json.Unmarshal(data, &subs); err != nil {
		return fmt.Errorf("parse discussion sub store: %w", err)
	}
	s.subs = subs
	return nil
}

func (s *DiscussionSubStore) save() error {
	data, err := json.MarshalIndent(s.subs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal discussion sub store: %w", err)
	}
	// Atomic write: temp file then rename.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write discussion sub store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename discussion sub store: %w", err)
	}
	return nil
}

// Subscribe adds a subscription (idempotent — duplicate silently ignored).
func (s *DiscussionSubStore) Subscribe(discussionID, sessionName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		if sub.DiscussionID == discussionID && sub.SessionName == sessionName {
			return nil // already subscribed
		}
	}
	s.subs = append(s.subs, DiscussionSub{
		DiscussionID: discussionID,
		SessionName:  sessionName,
	})
	return s.save()
}

// Unsubscribe removes a subscription.
func (s *DiscussionSubStore) Unsubscribe(discussionID, sessionName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := s.subs[:0]
	for _, sub := range s.subs {
		if sub.DiscussionID == discussionID && sub.SessionName == sessionName {
			continue
		}
		filtered = append(filtered, sub)
	}
	s.subs = filtered
	return s.save()
}

// GetSubscribers returns all session names subscribed to the given discussion.
func (s *DiscussionSubStore) GetSubscribers(discussionID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var names []string
	for _, sub := range s.subs {
		if sub.DiscussionID == discussionID {
			names = append(names, sub.SessionName)
		}
	}
	return names
}

// List returns all subscriptions.
func (s *DiscussionSubStore) List() []DiscussionSub {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DiscussionSub, len(s.subs))
	copy(out, s.subs)
	return out
}
