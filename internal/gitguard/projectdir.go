package gitguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// projectMarkers are the entries that make a directory a project. A directory
// holding none of them (and no git repository) is not somewhere an automatic
// commit may run.
var projectMarkers = []string{
	"go.mod", "package.json", "pyproject.toml", "Cargo.toml",
	"Makefile", "AGENT.md", ".git", ".datawatch",
}

// NonProjectError is returned when an automatic commit is refused because the
// directory is not a project. Callers must not retry it: the directory will
// not become a project by trying again.
type NonProjectError struct {
	Dir    string
	Reason string
}

func (e *NonProjectError) Error() string {
	return fmt.Sprintf("automatic commit refused in %s: %s", e.Dir, e.Reason)
}

// IsNonProject reports whether err is, or wraps, a NonProjectError.
func IsNonProject(err error) bool {
	var np *NonProjectError
	return errors.As(err, &np)
}

// NonProjectReason returns why dir must not receive automatic commits, or ""
// when it may. It refuses (a) exactly the user's home directory, (b) a
// filesystem root, and (c) a directory with no git repository and no project
// marker. A repository below the home directory is fine. homeResolver nil
// means os.UserHomeDir.
func NonProjectReason(dir string, homeResolver func() (string, error)) string {
	if homeResolver == nil {
		homeResolver = os.UserHomeDir
	}
	abs, err := normalizePath(dir)
	if err != nil {
		return "directory path cannot be resolved"
	}
	if filepath.Dir(abs) == abs {
		return "directory is a filesystem root"
	}
	if home, herr := homeResolver(); herr == nil {
		if homeAbs, nerr := normalizePath(home); nerr == nil && homeAbs == abs {
			return "directory is the user's home directory"
		}
	}
	for _, m := range projectMarkers {
		if _, serr := os.Lstat(filepath.Join(abs, m)); serr == nil {
			return ""
		}
	}
	return "directory has no git repository and no project marker"
}

var (
	refusedMu   sync.Mutex
	refusedDirs = map[string]string{}
)

// RefuseNonProject checks dir and returns a *NonProjectError when automatic
// commits are not allowed there, nil otherwise. When the daemon has registered
// its allowed project roots (SetProjectRoots), a project directory outside
// every root is refused as well. The first refusal for a
// directory writes one audit line; later calls return the same error without
// logging again.
func RefuseNonProject(dir string) error {
	return refuseNonProject(dir, nil)
}

func refuseNonProject(dir string, homeResolver func() (string, error)) error {
	refusedMu.Lock()
	defer refusedMu.Unlock()
	if reason, seen := refusedDirs[dir]; seen {
		return &NonProjectError{Dir: dir, Reason: reason}
	}
	reason := NonProjectReason(dir, homeResolver)
	if reason == "" {
		// A project, but is it under an allowed root? Not remembered: the
		// operator can add a root and the next commit is allowed.
		return refuseOutsideRoots(dir)
	}
	refusedDirs[dir] = reason
	fmt.Printf("[audit] {\"event\":\"commit_refused_non_project\",\"dir\":%q,\"reason\":%q}\n", dir, reason)
	return &NonProjectError{Dir: dir, Reason: reason}
}
