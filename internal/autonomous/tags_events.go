// Tag change events. Each tag registry change (create, rename, reparent,
// recolor, delete) and each assignment change (an Automaton's tag list, a
// session's own tags) is reported through one callback so open clients can
// refresh their tag counts. The payload carries IDs only; a client that
// needs names or counts fetches them again.
//
// The server registers the callback with SetTagEventFn from main.go, so this
// package never imports the server package. The callback is the same path the
// PRD WS broadcast takes: the server turns each TagEvent into a tag_update
// frame on the hub.

package autonomous

// Tag event kinds, the kind field of a TagEvent.
const (
	TagEventCreated         = "created"
	TagEventRenamed         = "renamed"
	TagEventReparented      = "reparented"
	TagEventRecolored       = "recolored"
	TagEventDeleted         = "deleted"
	TagEventAssigned        = "assigned"
	TagEventSessionAssigned = "session_assigned"
	// TagEventRegistrySynced is sent when a sync from the tag registry keeper
	// changes the tags or the notes this daemon holds. It carries no ID.
	TagEventRegistrySynced = "registry_synced"
)

// TagEvent is one tag registry or assignment change. Fields a kind does not
// use are omitted from the JSON:
//
//   - created, renamed, reparented, recolored, deleted: TagID
//   - assigned: PRDID (the Automaton whose tag list changed)
//   - session_assigned: SessionID (the session whose own tags changed)
type TagEvent struct {
	Kind      string `json:"kind"`
	TagID     string `json:"tag_id,omitempty"`
	PRDID     string `json:"prd_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// SetTagEventFn registers the callback that receives tag events, or clears it
// when fn is nil. The callback runs synchronously on the goroutine that made
// the change, after the change is saved, so it must not block.
func (m *Manager) SetTagEventFn(fn func(TagEvent)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tagEventFn = fn
}

// emitTagEvent delivers ev to the registered callback, if one is set. Call it
// only after the change is saved, and never while holding m.mu.
func (m *Manager) emitTagEvent(ev TagEvent) {
	m.mu.Lock()
	fn := m.tagEventFn
	m.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}
