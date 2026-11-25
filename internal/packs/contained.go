package packs

import (
	"fmt"
	"os"
	"path/filepath"
)

// openDir opens dir as a root. The trailing "/." makes anything but a
// directory fail at once; opening a named pipe by its own name would wait for a
// writer.
func openDir(dir string) (*os.Root, error) {
	return os.OpenRoot(dir + string(filepath.Separator) + ".")
}

// packFiles reads the files of one pack folder. Every read goes through a root
// on the folder, so nothing outside it is opened. A link that resolves inside
// the folder is followed, whether its target is written absolute or relative;
// any other link is refused.
type packFiles struct {
	// real is the folder with every link resolved.
	real string
	root *os.Root
}

func openPackFiles(dir string) (*packFiles, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	root, err := openDir(real)
	if err != nil {
		return nil, err
	}
	return &packFiles{real: real, root: root}, nil
}

func (f *packFiles) close() { _ = f.root.Close() }

// resolve returns the path, relative to the root, of the file the clean
// relative path rel names once links are followed.
func (f *packFiles) resolve(rel string) (string, error) {
	resolved, err := filepath.EvalSymlinks(filepath.Join(f.real, rel))
	if err != nil {
		return "", err
	}
	if !withinPackDir(f.real, resolved) {
		return "", fmt.Errorf("%s is a link that leaves the pack folder", rel)
	}
	return filepath.Rel(f.real, resolved)
}

// readRegular reads the regular file rel names.
func (f *packFiles) readRegular(rel string) ([]byte, error) {
	name, err := f.resolve(rel)
	if err != nil {
		return nil, err
	}
	info, err := f.root.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", rel)
	}
	return f.root.ReadFile(name)
}
