package packs

// WorkerAllowList is the set of tools and file scopes a worker profile
// permits. A task is clamped to it by ClampToProfile. The tool entries mirror
// the allow-list of workerprofile.ToolSelection; file scopes have no
// counterpart there, so the caller supplies them directly.
type WorkerAllowList struct {
	Tools      []string
	FileScopes []string
}

// ClampToProfile reduces a task's tools and file_scopes to the entries that
// allow also permits. It returns a copy of task with both lists narrowed to
// the intersection, in the task's original order, and the entries it dropped
// (tools first, then file scopes, each in the task's order). It never adds an
// entry the task did not already name, and it does not modify the input task.
//
// An empty allow list drops every entry. A list that is nil on the task stays
// nil in the result, and dropped is nil when nothing was removed.
func ClampToProfile(task PackTask, allow WorkerAllowList) (PackTask, []string) {
	tools, droppedTools := intersectEntries(task.Tools, allow.Tools)
	scopes, droppedScopes := intersectEntries(task.FileScopes, allow.FileScopes)

	var dropped []string
	dropped = append(dropped, droppedTools...)
	dropped = append(dropped, droppedScopes...)

	task.Tools = tools
	task.FileScopes = scopes
	return task, dropped
}

// intersectEntries returns the entries of want that appear in allowed, in
// want's order, and the entries of want that do not. A nil want yields a nil
// kept list; the returned kept slice is always a fresh slice, never an alias
// of want.
func intersectEntries(want, allowed []string) (kept, dropped []string) {
	if want == nil {
		return nil, nil
	}
	permit := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		permit[a] = struct{}{}
	}
	kept = make([]string, 0, len(want))
	for _, w := range want {
		if _, ok := permit[w]; ok {
			kept = append(kept, w)
		} else {
			dropped = append(dropped, w)
		}
	}
	return kept, dropped
}
