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
// As a second barrier form, the id must also be a local name
// (filepath.IsLocal), and reaching the cleaned joined path from the base
// via filepath.Rel must not require climbing into a parent directory (a
// relative result of ".." or starting with ".."). All refusals return
// errInvalidDiscussionID.
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
	// Second barrier form: demand additionally that the id be a local name
	// and that the joined path be reachable from the base without a parent
	// hop. Either refusal keeps the caller inside the discussions folder.
	if !filepath.IsLocal(id) {
		return "", errInvalidDiscussionID
	}
	rel, err := filepath.Rel(base, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
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
