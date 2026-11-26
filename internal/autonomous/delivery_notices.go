// Delivery notices are the operator alerts about decision lines, one-time and
// subscription messages and supervisor wakes. The daemon starts the lifecycle
// engine and the coordinator before it sets the alert sink, so a notice is not
// handed to the sink directly:
//
//   - It is saved first, before the change it reports is saved, and removed
//     only once the sink has taken it.
//   - With no sink yet it waits, and SetAlertSink hands it over. A daemon that
//     stops first hands it over after its next start.
//   - A daemon that dies part-way may raise a notice twice, or one for a
//     change it never saved. It does not lose one it had saved.
//
// A notice carries ids and reason codes, never message or answer text.

package autonomous

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	deliveryNoticesFile = "delivery_notices.json"
	// maxHeldNotices bounds what waits for a sink; the alert store keeps no
	// more than this many alerts.
	maxHeldNotices = 500
	// heldNoticeAfterRestart marks a notice an earlier daemon run saved.
	heldNoticeAfterRestart = " (Raised before a daemon restart.)"
)

type deliveryNotice struct {
	Level string `json:"level"`
	Title string `json:"title"`
	Body  string `json:"body"`

	// staged: the change it reports is not saved yet, so it is not handed
	// over. Not stored: after a restart every stored notice is handed over.
	staged  bool
	earlier bool // stored by an earlier run
}

// noticeBox holds the notices the sink has not taken. The zero value is
// ready; the file is read on first use.
type noticeBox struct {
	mu      sync.Mutex
	loaded  bool
	pending []*deliveryNotice
	// flushing: one goroutine at a time hands notices over.
	flushing bool
}

// noticePath is "" for a manager with no store: notices are then held in
// memory only.
func (m *Manager) noticePath() string {
	if m.store == nil {
		return ""
	}
	return filepath.Join(m.store.dir, deliveryNoticesFile)
}

// loadNoticesLocked reads what an earlier run left. The caller holds the
// box's lock.
func (m *Manager) loadNoticesLocked() {
	if m.notices.loaded {
		return
	}
	m.notices.loaded = true
	path := m.noticePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[autonomous] delivery notices could not be read: %v", err)
		}
		return
	}
	var stored []*deliveryNotice
	if err := json.Unmarshal(data, &stored); err != nil {
		log.Printf("[autonomous] delivery notices could not be read: %v", err)
		return
	}
	for _, n := range stored {
		if n != nil {
			n.earlier = true
			m.notices.pending = append(m.notices.pending, n)
		}
	}
}

// saveNoticesLocked writes the pending notices. When that fails they are held
// in memory only, and the log says so.
func (m *Manager) saveNoticesLocked() {
	path := m.noticePath()
	if path == "" {
		return
	}
	err := func() error {
		if len(m.notices.pending) == 0 {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		data, err := json.Marshal(m.notices.pending)
		if err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}()
	if err != nil {
		log.Printf("[autonomous] delivery notices could not be saved; a restart would lose them: %v", err)
	}
}

// prepareNotice saves a notice before the change it reports is saved. The
// caller then calls done: true once the change is saved, which hands the
// notice over, or false when nothing changed, which drops it.
func (m *Manager) prepareNotice(level, title, body string) (done func(raise bool)) {
	box := &m.notices
	box.mu.Lock()
	m.loadNoticesLocked()
	var n *deliveryNotice
	mine := true
	for _, have := range box.pending {
		if have.Level == level && have.Title == title && have.Body == body {
			n, mine = have, false // the same notice is already owed
			break
		}
	}
	if n == nil {
		n = &deliveryNotice{Level: level, Title: title, Body: body, staged: true}
		box.pending = append(box.pending, n)
		if over := len(box.pending) - maxHeldNotices; over > 0 {
			log.Printf("[autonomous] delivery notices: %d oldest dropped, the alert sink has not taken them", over)
			box.pending = box.pending[over:]
		}
		m.saveNoticesLocked()
	}
	box.mu.Unlock()
	return func(raise bool) {
		box.mu.Lock()
		switch {
		case raise:
			n.staged = false
		case mine && n.staged:
			m.dropNoticeLocked(n)
		}
		box.mu.Unlock()
		if raise {
			m.flushNotices()
		}
	}
}

func (m *Manager) dropNoticeLocked(n *deliveryNotice) {
	for i, have := range m.notices.pending {
		if have == n {
			m.notices.pending = append(m.notices.pending[:i:i], m.notices.pending[i+1:]...)
			m.saveNoticesLocked()
			return
		}
	}
}

// deliveryAlert raises a notice that reports no saved change.
func (m *Manager) deliveryAlert(level, title, body string) {
	m.prepareNotice(level, title, body)(true)
}

// flushNotices hands every ready notice to the sink, oldest first. It does
// nothing without a sink, or while another call is doing it.
func (m *Manager) flushNotices() {
	box := &m.notices
	box.mu.Lock()
	if box.flushing {
		box.mu.Unlock()
		return
	}
	box.flushing = true
	m.loadNoticesLocked()
	box.mu.Unlock()
	finished := false
	defer func() {
		if !finished { // the sink panicked
			box.mu.Lock()
			box.flushing = false
			box.mu.Unlock()
		}
	}()
	for {
		sink := m.AlertSink()
		box.mu.Lock()
		var n *deliveryNotice
		if sink != nil {
			for _, have := range box.pending {
				if !have.staged {
					n = have
					break
				}
			}
		}
		if n == nil {
			// Cleared under the lock that showed nothing is ready, so a
			// notice made ready now starts its own pass.
			box.flushing, finished = false, true
			box.mu.Unlock()
			return
		}
		body := n.Body
		if n.earlier {
			body += heldNoticeAfterRestart
		}
		box.mu.Unlock()
		// Outside the lock: what the sink calls may raise a notice.
		sink(n.Level, n.Title, body)
		box.mu.Lock()
		m.dropNoticeLocked(n)
		box.mu.Unlock()
	}
}
