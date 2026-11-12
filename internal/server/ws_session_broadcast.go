// Bounded session-list broadcast (see the header of ws_session_stream.go
// for the wire behaviour).
//
// A state change must not marshal or copy the whole session list. The
// callers select the default-view sessions with the cheap state / UpdatedAt
// check while the store lock is held (session.Manager.SnapshotFiltered),
// snapshot only those, and hand them to broadcastSessionList together with
// the set of every held session ID. Everything below works on that
// default view:
//
//   - legacy clients get one bounded `sessions` frame, built only if such a
//     client is connected (broadcastLazy);
//   - window and delta clients get a session_state per session whose
//     fingerprint changed, and a session_removed per session the manager no
//     longer holds. A session that merely aged out of the window drops out
//     of the snapshot silently, so it appears as changed if it re-enters;
//   - with no client connected nothing is fingerprinted and the snapshot is
//     dropped, so a client that joins later is never diffed against stale
//     state.

package server

import (
	"sort"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// sessionRowStateData is session_state for window clients: one summary row,
// the shape GET /api/sessions?view=summary returns.
type sessionRowStateData struct {
	Session sessionSummaryRow `json:"session"`
}

func (h *Hub) isWindowClient(c *client) bool {
	list, _ := h.sessionModes(c)
	return list == sessionListWindow
}

// isLegacyClient is a client on the legacy `sessions` frame: it neither
// announced window nor chose delta.
func (h *Hub) isLegacyClient(c *client) bool {
	list, _ := h.sessionModes(c)
	return list != sessionListWindow && list != sessionListDelta
}

// isStreamClient is a window or delta client: no `sessions` frames, one
// session_state per change.
func (h *Hub) isStreamClient(c *client) bool {
	return !h.isLegacyClient(c)
}

// hasLegacyClient reports whether any connected client needs the legacy
// `sessions` frame.
func (h *Hub) hasLegacyClient() bool { return h.hasClient(h.isLegacyClient) }

// anyClientWants is hasClient for the Run goroutine's own use; want may
// take client locks but not h.mu.
func (h *Hub) anyClientWants(want func(*client) bool) bool {
	if want == nil {
		return h.ClientCount() > 0
	}
	return h.hasClient(want)
}

// broadcastLazy queues a frame that Run builds only if a client matches
// want, and counts in chanStats like broadcastTo.
func (h *Hub) broadcastLazy(build func() []byte, want func(*client) bool) {
	h.broadcast <- hubMessage{build: build, want: want}
}

// setSessionViewSource wires the one-pass default-view snapshot. Must be
// called before Run starts; nil leaves the hub without a list source, which
// holds no sessions (see defaultViewSnapshot).
func (h *Hub) setSessionViewSource(fn func(keep func(session.State, time.Time) bool) ([]*session.Session, map[string]struct{})) {
	h.viewSourceFn = fn
}

// defaultViewSnapshot copies the default-view sessions now and returns them
// with the set of held session IDs. Without a list source (a bare hub) it
// holds nothing, so legacy clients still get their bounded frame, with zero
// sessions.
func (h *Hub) defaultViewSnapshot() ([]*session.Session, map[string]struct{}) {
	if h.viewSourceFn == nil {
		return nil, map[string]struct{}{}
	}
	now, window := time.Now(), h.recentWindow()
	return h.viewSourceFn(func(st session.State, updated time.Time) bool {
		return stateInDefaultView(st, updated, now, window)
	})
}

// resetSessionSnapshot forgets the diff state. Called when no client is
// connected, so the next client is diffed against nothing instead of a
// snapshot that has not seen the changes made while it was away.
func (h *Hub) resetSessionSnapshot() {
	h.snapMu.Lock()
	h.snapshot = nil
	h.heldPrev = nil
	h.snapMu.Unlock()
}

// seedSnapshotIfIdle baselines the diff state from the current default view
// when no pass has run since the last client-less period. A client that
// joins as window or delta has just been given the current list, so the
// first change after it joins must stream only that change, not the whole
// view again.
func (h *Hub) seedSnapshotIfIdle() {
	h.snapMu.Lock()
	defer h.snapMu.Unlock()
	if h.heldPrev != nil {
		return
	}
	view, held := h.defaultViewSnapshot()
	if held == nil {
		return
	}
	next := make(map[string]uint64, len(view))
	for _, sess := range view {
		if fp, ok := sessionFingerprint(sess); ok {
			next[sess.FullID] = fp
		}
	}
	h.snapshot, h.heldPrev = next, held
}

// BroadcastSessionView snapshots the default-view sessions and broadcasts
// them (the synchronous path of NotifyStateChange and NotifyNeedsInput).
// It does no list work when no client needs it.
func (h *Hub) BroadcastSessionView() {
	if h.ClientCount() == 0 {
		h.resetSessionSnapshot()
		return
	}
	view, held := h.defaultViewSnapshot()
	if held == nil {
		return
	}
	h.broadcastSessionList(view, held)
}

// legacySessionsFrame returns the builder for the bounded legacy `sessions`
// frame: view (already default-view copies) with long text cut, plus
// window_minutes, total and more.
func (h *Hub) legacySessionsFrame(view []*session.Session, total int) func() []byte {
	window := h.recentWindow()
	textChars := h.summaryTextChars()
	return func() []byte {
		h.legacyFrameBuilds.Add(1)
		recs := make([]sessionListRecord, 0, len(view))
		for _, sess := range view {
			rec := sessionListRecord{}
			rec.Session, rec.TruncatedFields = summarizeSession(sess, textChars)
			recs = append(recs, rec)
		}
		return encodeSessionsFrame(sessionsRecordsData{
			Sessions: recs, Version: h.version,
			WindowMinutes: int(window / time.Minute), Total: total, More: total > len(view),
		})
	}
}

// broadcastSessionList is the single fan-out for a session-list change.
// view is the default-view sessions (copies, safe to marshal); held is the
// set of every session ID the manager holds. See the file comment.
func (h *Hub) broadcastSessionList(view []*session.Session, held map[string]struct{}) {
	h.broadcastLazy(h.legacySessionsFrame(view, len(held)), h.isLegacyClient)

	// The lock is held while queueing so frames reach the hub in the same
	// order as snapshot updates.
	h.snapMu.Lock()
	defer h.snapMu.Unlock()
	if !h.hasClient(h.isStreamClient) {
		// Only legacy clients (or none) need this change, and they need no
		// diff. Keep no snapshot: a window or delta client that joins later
		// is seeded from the default view (seedSnapshotIfIdle).
		h.snapshot, h.heldPrev = nil, nil
		return
	}
	next := make(map[string]uint64, len(view))
	var changed []*session.Session
	for _, sess := range view {
		if sess == nil {
			continue
		}
		fp, ok := sessionFingerprint(sess)
		if !ok {
			continue
		}
		next[sess.FullID] = fp
		if old, seen := h.snapshot[sess.FullID]; !seen || old != fp {
			changed = append(changed, sess)
		}
	}
	// Deleted = held at the last pass or streamed, and not held now. A
	// session that only left the default view is still held: no frame.
	var removed []string
	for id := range h.heldPrev {
		if _, ok := held[id]; !ok {
			removed = append(removed, id)
		}
	}
	for id := range h.snapshot {
		if _, ok := held[id]; ok {
			continue
		}
		if _, dup := h.heldPrev[id]; !dup {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	h.snapshot, h.heldPrev = next, held

	for _, sess := range changed {
		h.sendSessionState(sess, h.isStreamClient)
	}
	for _, id := range removed {
		h.broadcastTo(MsgSessionRemoved, SessionRemovedData{SessionID: id}, h.isStreamClient)
	}
}
