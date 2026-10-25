package tagregistry

// ApplyPeerWrite applies one write a peer forwarded to this daemon. Writes are
// applied in the order they arrive, under the registry lock. A peer daemon
// cannot keep the registry, so it refuses with ErrNotKeeper.
func (r *Registry) ApplyPeerWrite(w Write) (WriteResult, error) {
	if r.role == RolePeer {
		return WriteResult{}, ErrNotKeeper
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.applyAndCommitLocked(w)
}

// KeeperSnapshot returns the registry a peer pulls. A peer refuses with ErrNotKeeper.
func (r *Registry) KeeperSnapshot() (Snapshot, error) {
	if r.role == RolePeer {
		return Snapshot{}, ErrNotKeeper
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return Snapshot{
		Revision: r.st.Revision,
		Tags:     copyTags(r.st.Tags),
		Notes:    copyNotes(r.st.Notes),
	}, nil
}

// localWrite applies an edit the user made on this daemon. The edit is based on
// the current revision, so it never conflicts.
func (r *Registry) localWrite(w Write) (WriteResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w.BaseRevision = r.st.Revision
	return r.applyAndCommitLocked(w)
}
