package autonomous

// AutomatonName returns the stored name of an Automaton by id. It reads under
// the store lock and copies nothing else, so it is cheap enough for a list
// of questions. The second result is false for an unknown id or an Automaton
// with no stored title.
func (a *API) AutomatonName(id string) (string, bool) {
	if a == nil || a.M == nil || a.M.Store() == nil {
		return "", false
	}
	st := a.M.Store()
	var name string
	st.WithLock(func() {
		if p, ok := st.prds[id]; ok && p != nil {
			name = p.Title
		}
	})
	return name, name != ""
}

// TaskName returns the stored name of a task by id; the second result is
// false for an unknown id or a task with no stored title.
func (a *API) TaskName(id string) (string, bool) {
	if a == nil || a.M == nil || a.M.Store() == nil {
		return "", false
	}
	t, ok := a.M.Store().TaskSnapshot(id)
	if !ok || t.Title == "" {
		return "", false
	}
	return t.Title, true
}
