// Built-in pack entry point. The standard-tasks pack ships inside the
// binary (embedded by internal/packs/builtinpacks); LoadBuiltin turns that
// embedded folder back into a *Pack loaded through the same path an
// installed pack folder takes.
package packs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	builtinpacks "github.com/dmz006/datawatch/internal/packs/builtinpacks"
)

// builtinPackRoot is the root of the embedded pack folder inside
// builtinpacks.FS.
const builtinPackRoot = "builtin"

// builtinExtractPrefix is the MkdirTemp prefix LoadBuiltin uses for the
// on-disk copy of the embedded folder; earlier extractions sharing it are
// pruned on each call.
const builtinExtractPrefix = "datawatch-builtin-pack-"

// LoadBuiltin loads the built-in pack of the standard tasks and Automaton
// templates from the copy embedded in the binary. It writes the embedded
// pack folder out to a fresh directory under the OS temp folder and reads it
// back through LoadPackFolder, so the returned pack carries the same digest
// and Dir an installed pack folder would and CheckTaskRules can be run
// against it.
//
// The written folder is left in place on a successful load: LoadPackFolder
// records its path on Pack.Dir and the rule checks resolve verification
// scripts against it. Each call removes the extractions left by earlier
// calls so repeated loads do not accumulate folders.
func LoadBuiltin() (*Pack, error) {
	dir, err := os.MkdirTemp("", builtinExtractPrefix)
	if err != nil {
		return nil, fmt.Errorf("builtin pack: %w", err)
	}
	if err := extractEmbedded(builtinpacks.FS, dir); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("builtin pack: %w", err)
	}
	pruneBuiltinExtractions(dir)

	p, err := LoadPackFolder(dir)
	if err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("builtin pack: %w", err)
	}
	return p, nil
}

// extractEmbedded writes every regular file under the root of fsys into
// dest, creating subdirectories as needed.
func extractEmbedded(fsys fs.FS, dest string) error {
	root, err := fs.Sub(fsys, builtinPackRoot)
	if err != nil {
		return fmt.Errorf("embedded folder: %w", err)
	}
	return fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return fmt.Errorf("reading %q: %w", path, err)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return fmt.Errorf("writing %q: %w", path, err)
		}
		return nil
	})
}

// pruneBuiltinExtractions removes the on-disk copies LoadBuiltin left in
// earlier calls, keeping keep. Errors are dropped on purpose: pruning is a
// best-effort cleanup, never a load requirement.
func pruneBuiltinExtractions(keep string) {
	dir, err := filepath.Abs(filepath.Dir(keep))
	if err != nil {
		return
	}
	keepBase := filepath.Base(keep)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), builtinExtractPrefix) || e.Name() == keepBase {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
}
