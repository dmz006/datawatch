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
