//go:build linux

package testisolation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

// Findings on the file system.
const (
	FindingRead    = "LISTED-OR-READ" // a decoy was listed or read
	FindingListed  = "LISTED"         // a root folder was listed
	FindingChanged = "CHANGED"        // a decoy was written, or its attributes or entries changed
	FindingRemoved = "REMOVED"        // a decoy is gone
	FindingWrote   = "WROTE"          // an entry was made or removed in the machine temp folder or the home
)

type times struct{ atime, mtime, ctime syscall.Timespec }

// Snapshot is the state of the decoys and roots before a run.
type Snapshot struct {
	decoys map[string]times
	roots  map[string]times
	listed []string                   // roots whose access time is checked
	made   []string                   // roots in which no entry may be made
	had    map[string]map[string]bool // their entries before the run
}

func statTimes(p string) (times, error) {
	var st syscall.Stat_t
	if err := syscall.Lstat(p, &st); err != nil {
		return times{}, &os.PathError{Op: "lstat", Path: p, Err: err}
	}
	return times{atime: st.Atim, mtime: st.Mtim, ctime: st.Ctim}, nil
}

// Plant creates the decoys under l and records them in l.Decoys.
//
// The names in the shared temp folder cover what a clean-up pass looks for:
// build cache and scan cache folders old enough to be removed, a daemon
// scratch name this run never made, and one name nothing looks for, which only
// a walk of the whole folder reaches.
func (l *Layout) Plant() error {
	names := map[string][]string{
		l.TempRoot: {DecoyPrefix + "other-session"},
		l.Shared: {
			DecoyPrefix + "other-session", "go-build424242", "trivy-scan-cache-424242",
			"datawatch-pi-agent-" + DecoyPrefix + "424242",
		},
		l.Home: {DecoyPrefix + "project", ".datawatch"},
	}
	l.Decoys = nil
	for _, root := range []string{l.TempRoot, l.Shared, l.Home} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return err
		}
		for _, n := range names[root] {
			d := filepath.Join(root, n)
			if _, err := os.Lstat(d); err == nil {
				return fmt.Errorf("decoy %s already exists: the guard only runs in an empty sandbox", d)
			}
			files := []string{"notes.txt", ".git/HEAD", "sub/deeper/data.json"}
			if n == ".datawatch" {
				files = []string{"config.yaml"}
			}
			for _, f := range files {
				p := filepath.Join(d, f)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(p, []byte("# planted by the test isolation guard\n"), 0o644); err != nil {
					return err
				}
			}
			l.Decoys = append(l.Decoys, d)
		}
	}
	return nil
}

// Snapshot backdates every decoy and root and records their times. It must be
// the last thing done to them before the run: from here on any read moves an
// access time, and any change a modification or change time.
func (l Layout) Snapshot() (*Snapshot, error) {
	if err := checkAccessTimes(l.Shared); err != nil {
		return nil, err
	}
	// Tests make their own entries in the shared temp folder.
	made := []string{l.Home, l.TempRoot}
	had := map[string]map[string]bool{}
	for _, root := range made {
		names, err := entryNames(root)
		if err != nil {
			return nil, err
		}
		had[root] = names
	}
	var all []string
	for _, d := range l.Decoys {
		if err := filepath.WalkDir(d, func(p string, _ fs.DirEntry, err error) error {
			all = append(all, p)
			return err
		}); err != nil {
			return nil, err
		}
	}
	// Deepest first, the roots last: listing a folder to find its entries
	// would move its access time again.
	sort.Sort(sort.Reverse(sort.StringSlice(all)))
	roots := []string{l.Shared, l.Home, l.TempRoot}
	old := time.Now().Add(-48 * time.Hour)
	for _, p := range append(append([]string(nil), all...), roots...) {
		if err := os.Chtimes(p, old, old); err != nil {
			return nil, err
		}
	}
	s := &Snapshot{
		decoys: map[string]times{}, roots: map[string]times{},
		listed: roots, made: made, had: had,
	}
	for _, p := range all {
		t, err := statTimes(p)
		if err != nil {
			return nil, err
		}
		s.decoys[p] = t
	}
	for _, p := range roots {
		t, err := statTimes(p)
		if err != nil {
			return nil, err
		}
		s.roots[p] = t
	}
	return s, nil
}

func entryNames(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	return names, nil
}

// checkAccessTimes fails unless listing a backdated folder in dir moves its
// access time; without that the guard would pass everything.
func checkAccessTimes(dir string) error {
	probe, err := os.MkdirTemp(dir, "atime-check-")
	if err != nil {
		return err
	}
	defer os.Remove(probe) //nolint:errcheck
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(probe, old, old); err != nil {
		return err
	}
	before, err := statTimes(probe)
	if err != nil {
		return err
	}
	if _, err := os.ReadDir(probe); err != nil {
		return err
	}
	after, err := statTimes(probe)
	if err != nil {
		return err
	}
	if before.atime == after.atime {
		return errors.New("this file system does not record access times (noatime), so reads cannot be detected")
	}
	return nil
}

// Check compares the file system with the snapshot. Call it once: after it has
// judged the times it lists the roots to name new entries.
func (s *Snapshot) Check() []Violation {
	var out []Violation
	add := func(what, p string) { out = append(out, Violation{What: what, Path: p}) }
	paths := make([]string, 0, len(s.decoys))
	for p := range s.decoys {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		was := s.decoys[p]
		now, err := statTimes(p)
		switch {
		case err != nil:
			add(FindingRemoved, p)
		case now.mtime != was.mtime || now.ctime != was.ctime:
			add(FindingChanged, p)
		case now.atime != was.atime:
			add(FindingRead, p)
		}
	}
	for _, p := range s.listed {
		now, err := statTimes(p)
		if err != nil {
			add(FindingRemoved, p)
			continue
		}
		if now.atime != s.roots[p].atime {
			add(FindingListed, p)
		}
	}
	for _, p := range s.made {
		now, err := statTimes(p)
		if err != nil || now.mtime == s.roots[p].mtime {
			continue
		}
		names, err := entryNames(p)
		var fresh []string
		for n := range names {
			if !s.had[p][n] {
				fresh = append(fresh, n)
			}
		}
		sort.Strings(fresh)
		switch {
		case err != nil:
			add(FindingWrote, p)
		case len(fresh) == 0:
			add(FindingWrote, p+" (an entry made and removed again, or removed)")
		default:
			for _, n := range fresh {
				add(FindingWrote, filepath.Join(p, n))
			}
		}
	}
	return out
}
