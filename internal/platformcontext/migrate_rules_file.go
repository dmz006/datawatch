// MigrateRulesFile migrates an existing per-project rules file by removing the
// datawatch platform context block from it. The platform context is owned by
// the daemon and re-appended at delivery time (see delivery.go); any copy baked
// into a user's rules file is stale by construction and must go.
//
// Only the platform context marked block (and its single separator blank line)
// is in scope — the daemon sections (see daemon_sections.go) are deliberately
// left untouched. The operation is idempotent: once the block is gone a second
// call is a no-op.
package platformcontext

import (
	"errors"
	"io/fs"
	"os"
)

// MigrateRulesFile removes the platform context block from the rules file at
// path. It returns changed=false with no error when the file does not exist or
// does not contain the block; in the no-block case the file is not rewritten.
// When a block is removed, the result is written back with the file's existing
// permission bits, and changed=true is returned.
func MigrateRulesFile(path string) (changed bool, err error) {
	original, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	content := original
	// RemoveSection removes one marked block (plus its separator blank line)
	// per call, so loop to a fixpoint: a file may carry more than one block.
	for {
		next := RemoveSection(string(content))
		if next == string(content) {
			break
		}
		content = []byte(next)
	}
	if string(content) == string(original) {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, content, info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}
