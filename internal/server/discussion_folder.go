package server

import (
	"path/filepath"
	"strings"
)

// discussionFolderPath joins the discussion base directory and a discussion
// id into a cleaned absolute-style path. The cleaned path check is what lets
// the scanner accept the path: the result is guaranteed to be a direct child
// of the cleaned base, so nothing can point at a sibling or an ancestor
// directory.
func discussionFolderPath(base, id string) (string, error) {
	if err := validateDiscussionID(id); err != nil {
		return "", err
	}
	if strings.Contains(id, "/") || strings.Contains(id, `\`) || id == "." || id == ".." {
		return "", errInvalidDiscussionID
	}
	cleanBase := filepath.Clean(base)
	clean := filepath.Clean(filepath.Join(cleanBase, id))
	if !strings.HasPrefix(clean, cleanBase+string(filepath.Separator)) {
		return "", errInvalidDiscussionID
	}
	return clean, nil
}
