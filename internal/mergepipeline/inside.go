package mergepipeline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The project's files that merge steps read and write are opened through a
// root handle, so the operating system holds each use to the project.

// errLeavesProject says a file's links lead out of the project.
var errLeavesProject = errors.New("leads outside the project")

// openInside opens dir through a root handle and gives rel's name under it.
// Links that stay in the project are followed here first, absolute ones too,
// which the handle alone refuses. The caller closes the handle.
func openInside(dir, rel string) (*os.Root, string, error) {
	base, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	if base, err = filepath.EvalSymlinks(base); err != nil {
		return nil, "", err
	}
	name := filepath.FromSlash(rel)
	switch found, err := filepath.EvalSymlinks(filepath.Join(base, name)); {
	case errors.Is(err, fs.ErrNotExist): // the handle judges the name as given
	case err != nil:
		return nil, "", err
	default:
		in, rerr := filepath.Rel(base, found)
		if rerr != nil || !filepath.IsLocal(in) {
			return nil, "", fmt.Errorf("%s %w", rel, errLeavesProject)
		}
		name = in
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, "", err
	}
	return root, name, nil
}

// readFileInside reads rel under dir without leaving dir.
func readFileInside(dir, rel string) ([]byte, error) {
	root, name, err := openInside(dir, rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(name)
}

// makefileLinksOut reports whether the makefile make reads first in dir is a
// link that leaves the project. The link is never followed.
func makefileLinksOut(dir string) (string, bool) {
	for _, name := range []string{"GNUmakefile", "makefile", "Makefile"} {
		fi, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if fi.Mode()&fs.ModeSymlink == 0 {
			return "", false
		}
		_, err = readFileInside(dir, name)
		return name, errors.Is(err, errLeavesProject) || linkLeavesDangling(dir, name)
	}
	return "", false
}

// linkLeavesDangling reports whether the link name in dir, which names no
// existing file, names a place outside dir by its text.
func linkLeavesDangling(dir, name string) bool {
	base, err := filepath.Abs(dir)
	if err != nil {
		return true
	}
	if base, err = filepath.EvalSymlinks(base); err != nil {
		return true
	}
	target, err := os.Readlink(filepath.Join(base, name))
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	rel, err := filepath.Rel(base, filepath.Clean(target))
	return err != nil || !filepath.IsLocal(rel)
}

// makefileLinkNote says why a step that needs a make target did not run: the
// project's makefile is a link out of the project, and the link is not followed.
func makefileLinkNote(name, target string) string {
	return "the project's " + name + " links outside the project and was not followed, so the " + target + " target the step needs cannot be checked"
}

// writeFileInside writes rel under dir without leaving dir. A file that is
// there keeps its mode; perm is for a new one.
func writeFileInside(dir, rel string, data []byte, perm os.FileMode) error {
	root, name, err := openInside(dir, rel)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.WriteFile(name, data, perm)
}
