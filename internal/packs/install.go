package packs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Install loads the pack folder at sourceDir, checks every task against the
// standard-task rules, and copies the folder to destRoot/<name>/<version>/.
//
// The first task that fails CheckTaskRules refuses the install, and the error
// from CheckTaskRules is returned unchanged. Nothing is copied in that case.
// An install is also refused when destRoot/<name>/<version>/ already exists,
// so an installed version is never overwritten, and when the pack name is not
// a single safe folder name.
//
// Symbolic links inside the pack are followed only when they resolve to a
// regular file inside sourceDir; any other link is refused, so the copy never
// pulls in content from outside the pack.
func Install(sourceDir, destRoot string) (*InstalledPack, error) {
	pack, err := LoadPackFolder(sourceDir)
	if err != nil {
		return nil, err
	}
	for _, task := range pack.Tasks {
		if err := CheckTaskRules(pack, task); err != nil {
			return nil, err
		}
	}
	if err := checkInstallName(pack.Name); err != nil {
		return nil, err
	}

	absRoot, err := filepath.Abs(destRoot)
	if err != nil {
		return nil, fmt.Errorf("pack install: %w", err)
	}
	dest := filepath.Join(absRoot, pack.Name, pack.Version)
	if _, err := os.Lstat(dest); err == nil {
		return nil, fmt.Errorf("pack install: %s is already installed at %s", pack.Name, dest)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("pack install: %w", err)
	}

	if err := copyPackFolder(pack.Dir, dest); err != nil {
		return nil, err
	}
	return &InstalledPack{
		Name:      pack.Name,
		Version:   pack.Version,
		Digest:    pack.Digest,
		SourceDir: pack.Dir,
		Dir:       dest,
	}, nil
}

// checkInstallName refuses a pack name that cannot be a single folder name
// under the install root.
func checkInstallName(name string) error {
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return fmt.Errorf("pack install: name %q is not a safe folder name", name)
	}
	return nil
}

// copyPackFolder copies the pack folder src to dest. It builds the copy in a
// temporary sibling folder and renames it into place, so a failed copy leaves
// no partial install behind.
func copyPackFolder(src, dest string) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("pack install: %w", err)
	}
	tmp, err := os.MkdirTemp(parent, ".install-*")
	if err != nil {
		return fmt.Errorf("pack install: %w", err)
	}
	if err := copyTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("pack install: %w", err)
	}
	return nil
}

// copyTree copies every file and directory under src into dst, which must
// already exist. Regular files keep their permission bits, so verification
// scripts stay executable. Reads go through a root on src and writes through a
// root on dst, so neither side can be led outside its folder.
func copyTree(src, dst string) error {
	from, err := openPackFiles(src)
	if err != nil {
		return fmt.Errorf("pack install: %w", err)
	}
	defer from.close()
	to, err := openDir(dst)
	if err != nil {
		return fmt.Errorf("pack install: %w", err)
	}
	defer func() { _ = to.Close() }()
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("pack install: %w", walkErr)
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("pack install: %w", err)
		}

		if d.IsDir() {
			return to.MkdirAll(rel, 0o755)
		}

		// resolve refuses a link that leaves the pack folder.
		name := rel
		if d.Type()&fs.ModeSymlink != 0 {
			if name, err = from.resolve(rel); err != nil {
				return fmt.Errorf("pack install: %w", err)
			}
		}
		info, err := from.root.Stat(name)
		if err != nil {
			return fmt.Errorf("pack install: %s: %w", rel, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("pack install: %s is not a regular file or folder", rel)
		}
		return copyFile(from.root, name, to, rel, info.Mode().Perm())
	})
}

// copyFile copies the regular file src under from to dst under to with the
// given permission bits.
func copyFile(from *os.Root, src string, to *os.Root, dst string, perm fs.FileMode) error {
	in, err := from.Open(src)
	if err != nil {
		return fmt.Errorf("pack install: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := to.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return fmt.Errorf("pack install: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("pack install: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("pack install: %w", err)
	}
	return nil
}
