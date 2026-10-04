// Package pathsafe validates a single untrusted field (a name or id) before
// it becomes one path segment, e.g. filepath.Join(dir, name+".yaml"). It
// does NOT replace a root-scoped traversal guard for a full caller-supplied
// path (see internal/server/bl333_file_service.go's checkPathTraversal for
// that) -- this is the narrower, cheaper check for "a record name must not
// smuggle a directory traversal."
//
// BL394 security review (docs/plans/2026-10-03-bl394-security-findings-review.md
// §3b) found this exact gap in three places independently: a persona name
// (internal/council), a skill-registry name (internal/skills), and a smoke
// run id (internal/server) -- all took a name/id from a JSON request body
// and joined it into a path with no check at all. This package is the
// shared fix so a fourth call site doesn't repeat the same mistake.
package pathsafe

import (
	"fmt"
	"strings"
)

// ValidateRecordName returns an error if name is not safe to use as a
// single path segment. Rejects empty, ".", "..", any path separator, any
// ".." substring, and null bytes.
func ValidateRecordName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%q is not a valid name", name)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("%q must not contain a path separator", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("%q must not contain \"..\"", name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("%q must not contain a null byte", name)
	}
	return nil
}
