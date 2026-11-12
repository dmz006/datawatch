package autonomous

import (
	"path"
	"path/filepath"
)

// completionMarkerFile is the file a worker writes into its worktree root to
// signal that it has finished.
const completionMarkerFile = "DATAWATCH_COMPLETE"

// daemonMarkerFiles lists every session marker file the daemon itself defines
// at the worktree root, as slash paths relative to that root: the completion
// marker plus the scratch files isBookkeepingPath already treats as daemon
// bookkeeping (the checkpoint note and the advisory tree lock).
func daemonMarkerFiles() []string {
	return []string{completionMarkerFile, "CHECKPOINT.md", treeLockRelPath}
}

// isDaemonMarkerPath reports whether p names one of the daemon's own marker
// files. Only an exact match against a root-relative path counts, after path
// cleaning; a file with the same name in a subfolder is not a marker.
//
// These are the only files the daemon may skip or remove when it deals with a
// worktree's contents. Every other file belongs to the Automaton's work.
func isDaemonMarkerPath(p string) bool {
	if p == "" {
		return false
	}
	c := path.Clean(filepath.ToSlash(p))
	for _, m := range daemonMarkerFiles() {
		if c == m {
			return true
		}
	}
	return false
}
