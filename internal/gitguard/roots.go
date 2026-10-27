package gitguard

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrOutsideAllowedRoots is returned by ResolveProjectDir when a project
// directory does not lie under any allowed project root.
var ErrOutsideAllowedRoots = errors.New("project directory is outside the allowed project roots")

// ResolveProjectDir is the project directory guard for a directory that came
// from a request or from a stored record. It cleans dir (an empty value, a
// NUL byte and a filesystem root are refused), then resolves '..' segments
// and symlinks, following a dangling link to the place it points at, and
// accepts the directory only when the resolved path is a root or lies under
// one of roots. Roots are resolved the same way, so a symlinked root works.
// Nothing is created. The returned path is the cleaned absolute path, which
// is what the caller should use from then on.
//
// An empty roots list refuses every directory: the check never fails open.
func ResolveProjectDir(dir string, roots []string) (string, error) {
	clean, err := CleanProjectDir(dir)
	if err != nil {
		return "", err
	}
	real, err := evalDeepest(clean)
	if err != nil {
		return "", fmt.Errorf("project dir %q: %w", dir, err)
	}
	var usable []string
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		rc, rerr := CleanProjectDir(root)
		if rerr != nil {
			continue
		}
		rr, rerr := evalDeepest(rc)
		if rerr != nil {
			continue
		}
		usable = append(usable, rc)
		if isWithin(rr, real) {
			return clean, nil
		}
	}
	if len(usable) == 0 {
		return "", fmt.Errorf("%w: %q (no allowed project root is configured; set session.workspace_root or server.allowed_project_roots)", ErrOutsideAllowedRoots, dir)
	}
	return "", fmt.Errorf("%w: %q (allowed roots: %s; add a root with server.allowed_project_roots)", ErrOutsideAllowedRoots, dir, strings.Join(usable, ", "))
}

// isWithin reports whether target is base or lies under it. Both must
// already be absolute and symlink-resolved.
func isWithin(base, target string) bool {
	back, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return back != ".." && !strings.HasPrefix(back, ".."+string(filepath.Separator))
}

// projectRoots is the daemon's source of allowed project roots, registered
// with SetProjectRoots. While none is registered (a library or test use that
// never started a daemon) the commit guard checks no roots.
var projectRoots atomic.Pointer[func() []string]

// SetProjectRoots registers the source of the allowed project roots the
// automatic-commit guard enforces. fn is read on every check, so a change of
// configuration applies without a restart. A nil fn removes the source.
func SetProjectRoots(fn func() []string) {
	if fn == nil {
		projectRoots.Store(nil)
		return
	}
	projectRoots.Store(&fn)
}

var (
	rootRefusedMu sync.Mutex
	rootRefused   = map[string]bool{}
)

// refuseOutsideRoots returns a *NonProjectError when a roots source is
// registered and dir does not lie under one of its roots. Unlike a
// non-project refusal it is not remembered: the operator can add a root and
// the next commit is allowed. Only the first refusal of a directory writes an
// audit line.
func refuseOutsideRoots(dir string) error {
	fn := projectRoots.Load()
	if fn == nil {
		return nil
	}
	_, err := ResolveProjectDir(dir, (*fn)())
	rootRefusedMu.Lock()
	defer rootRefusedMu.Unlock()
	if err == nil {
		delete(rootRefused, dir)
		return nil
	}
	reason := "directory is outside the allowed project roots (set server.allowed_project_roots)"
	if !rootRefused[dir] {
		rootRefused[dir] = true
		fmt.Printf("[audit] {\"event\":\"commit_refused_outside_project_roots\",\"dir\":%q,\"reason\":%q}\n", dir, reason)
	}
	return &NonProjectError{Dir: dir, Reason: reason}
}
