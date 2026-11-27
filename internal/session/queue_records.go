// The work queue over records, and the queue as another process sees it.
//
// Over records (delivery job 4) each item is one record in the request store,
// sealed on a secure install. Only the daemon opens them. The CLI and the
// standalone MCP server use a remote queue, which asks the daemon and opens no
// file.

package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// QueueRecords keeps queue items one record each. The daemon passes the
// delivery request store, which seals them on a secure install; this package
// does not import it.
type QueueRecords interface {
	LoadQueue() ([]json.RawMessage, error)
	SaveQueueItem(id string, data []byte) error
	DeleteQueueItem(id string) error
}

// QueueCall sends one request to the daemon and returns the body of a 2xx
// answer.
type QueueCall func(method, path string, body any) ([]byte, error)

// ErrQueueNotReady: the queue moved into the request store and the store is
// not open. The queue is unreadable, never empty.
var ErrQueueNotReady = errors.New("the work queue cannot be read yet: it is kept in the request store, which is not open")

// NewQueueStoreOn loads a queue store over records instead of one file.
func NewQueueStoreOn(records QueueRecords) (*QueueStore, error) {
	s := NewDeferredQueueStore()
	if err := s.MoveTo(func() (QueueRecords, error) { return records, nil }); err != nil {
		return nil, err
	}
	return s, nil
}

// NewDeferredQueueStore is a queue that is not open yet: every change fails
// until MoveTo gives it its records.
func NewDeferredQueueStore() *QueueStore {
	return &QueueStore{items: map[string]*QueueItem{}, deferred: true}
}

// NewRemoteQueueStore is the queue of a running daemon.
func NewRemoteQueueStore(call QueueCall) *QueueStore {
	return &QueueStore{items: map[string]*QueueItem{}, remote: call}
}

// MoveTo switches the queue to records. open runs while no change can be made,
// so what it imports is what the queue holds. When it fails, or the records
// cannot be loaded whole, the queue stays as it was.
func (s *QueueStore) MoveTo(open func() (QueueRecords, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := open()
	if err != nil {
		return err
	}
	raws, err := records.LoadQueue()
	if err != nil {
		return fmt.Errorf("read queue store: %w", err)
	}
	items, saved := map[string]*QueueItem{}, map[string][]byte{}
	for _, raw := range raws {
		it := &QueueItem{}
		if err := json.Unmarshal(raw, it); err != nil || it.ID == "" {
			return fmt.Errorf("parse queue store: a record is not a queue item")
		}
		data, err := json.Marshal(it)
		if err != nil {
			return err
		}
		items[it.ID], saved[it.ID] = it, data
	}
	s.items, s.saved, s.records, s.deferred = items, saved, records, false
	return nil
}

// saveRecords writes the items that changed. What cannot be saved is undone,
// so a change that was reported as failed did not happen.
func (s *QueueStore) saveRecords() error {
	var failed error
	for id, it := range s.items {
		data, err := json.Marshal(it)
		if err == nil && bytes.Equal(data, s.saved[id]) {
			continue
		}
		if err == nil && failed == nil {
			if err = s.records.SaveQueueItem(id, data); err == nil {
				s.saved[id] = data
				continue
			}
		}
		if failed == nil {
			failed = err
		}
		s.restore(id)
	}
	for id := range s.saved {
		if _, ok := s.items[id]; ok {
			continue
		}
		if failed == nil {
			if err := s.records.DeleteQueueItem(id); err != nil {
				failed = err
			} else {
				delete(s.saved, id)
				continue
			}
		}
		s.restore(id)
	}
	return failed
}

func (s *QueueStore) restore(id string) {
	data, ok := s.saved[id]
	if !ok {
		delete(s.items, id)
		return
	}
	it := &QueueItem{}
	if json.Unmarshal(data, it) == nil {
		s.items[id] = it
	}
}

// ---- the remote queue -------------------------------------------------------

// send posts to the daemon and decodes the answer into out, when given.
func (s *QueueStore) send(method, path string, body, out any) error {
	answer, err := s.remote(method, path, body)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("unreadable answer from the daemon: %w", err)
	}
	return nil
}

func (s *QueueStore) remoteItem(path string, body any) (*QueueItem, error) {
	var it *QueueItem
	if err := s.send(http.MethodPost, path, body, &it); err != nil {
		return nil, err
	}
	return it, nil
}

// ListErr is List that says when the queue cannot be read.
func (s *QueueStore) ListErr(role, state string) ([]*QueueItem, error) {
	if s.remote == nil {
		s.mu.Lock()
		deferred := s.deferred
		s.mu.Unlock()
		if deferred {
			return nil, ErrQueueNotReady
		}
		return s.List(role, state), nil
	}
	q := url.Values{}
	if role != "" {
		q.Set("role", role)
	}
	if state != "" {
		q.Set("state", state)
	}
	path := "/api/queue"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out []*QueueItem
	if err := s.send(http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
