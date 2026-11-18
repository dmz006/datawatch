//go:build linux

// Platform liveness probe for the cache-folder sweep (Linux).
//
// cacheFolderLiveness walks /proc: for every live pid it reads
// /proc/<pid>/fd (the open handles) and /proc/<pid>/cwd (the working
// directory) and reports held=true as soon as any of them points inside
// the candidate folder. Processes that vanished between the directory
// listing and the read are simply not holders. If /proc itself cannot be
// read, the probe reports unknown (known=false) — the sweep then treats
// the folder as held and keeps it, which is the safe default.

package autonomous

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func cacheFolderLiveness(path string) (held, known bool) {
	procRoot, err := os.ReadDir("/proc")
	if err != nil {
		return false, false // no readable /proc: liveness unknown → keep
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, false
	}
	inside := func(p string) bool {
		if !filepath.IsAbs(p) {
			return false
		}
		return p == abs || strings.HasPrefix(p, abs+string(filepath.Separator))
	}

	foundAny := false
	for _, e := range procRoot {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // not a pid directory
		}
		pid := e.Name()
		foundAny = true
		if cwd, err := os.Readlink("/proc/" + pid + "/cwd"); err == nil && inside(cwd) {
			return true, true // a live process works inside the candidate
		}
		fds, err := os.ReadDir("/proc/" + pid + "/fd")
		if err != nil {
			continue // the process ended before we read its fds: not a holder
		}
		for _, fd := range fds {
			target, err := os.Readlink("/proc/" + pid + "/fd/" + fd.Name())
			if err != nil {
				continue
			}
			if inside(target) {
				return true, true // an open handle inside the candidate
			}
		}
	}
	if !foundAny {
		// /proc listed but held no pid entries: this is not a usable
		// liveness view — report unknown and let the sweep keep the
		// folder rather than claim no process holds it.
		return false, false
	}
	return false, true // every live pid checked: nothing holds the folder
}
