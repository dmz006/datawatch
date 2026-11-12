package session

import (
	"fmt"
	"os"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// resolveWorkspaceDir joins a relative project directory under the workspace
// root and refuses any value that would land outside it ('..', an absolute
// path, or a symlink that leaves the root). The root is created when absent,
// as the joined directory was before.
func resolveWorkspaceDir(root, rel string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create workspace root: %w", err)
	}
	dir, err := gitguard.ResolveWithin(root, rel)
	if err != nil {
		return "", fmt.Errorf("project dir %q: %w", rel, err)
	}
	return dir, nil
}
