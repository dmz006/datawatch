package server

import (
	"path/filepath"
	"strings"
)

// discussionFolderForID resolves a discussion id to the folder that stores
// it, under this server's configured data directory. It refuses every id
// validateDiscussionID rejects, and, as a second line, any id that could
// escape the cleaned base directory: ".", "..", any slash or backslash, or
// any id whose cleaned joined path does not sit directly under the base.
// All refusals return errInvalidDiscussionID.
func (s *Server) discussionFolderForID(id string) (string, error) {
	if err := validateDiscussionID(id); err != nil {
		return "", err
	}
	if id == "." || id == ".." || strings.Contains(id, "/") || strings.Contains(id, `\`) {
		return "", errInvalidDiscussionID
	}
	base := filepath.Clean(s.statePath("discussions"))
	clean := filepath.Clean(filepath.Join(base, id))
	if !strings.HasPrefix(clean, base+string(filepath.Separator)) {
		return "", errInvalidDiscussionID
	}
	return clean, nil
}

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
