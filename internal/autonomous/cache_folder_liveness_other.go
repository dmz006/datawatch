//go:build !linux

// Platform liveness probe for the cache-folder sweep (non-Linux).
//
// Without /proc there is no practical way to enumerate the open file
// handles or working directories of the processes running on this host,
// so this probe reports liveness unknown for every candidate
// (known=false). The sweep treats unknown as held by a live process and
// keeps the folder — the safe default: nothing is ever removed on a
// platform whose processes cannot be inspected.

package autonomous

func cacheFolderLiveness(path string) (held, known bool) {
	_ = path
	return false, false // liveness unknown → the sweep keeps the folder
}
