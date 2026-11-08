package server

import "errors"

// errInvalidDiscussionID is returned by validateDiscussionID for every
// rejected id.
var errInvalidDiscussionID = errors.New("invalid discussion id")

// maxDiscussionIDLen is the longest discussion id, in characters, that
// validateDiscussionID accepts.
const maxDiscussionIDLen = 128

// validateDiscussionID reports whether id is a usable discussion id. A valid
// id is 1 to 128 characters long and is made only of ASCII letters, digits,
// dot, underscore and hyphen. The names "." and ".." are never valid. Any
// other input, including the empty string, returns an error whose text is
// "invalid discussion id".
func validateDiscussionID(id string) error {
	if len(id) < 1 || len(id) > maxDiscussionIDLen {
		return errInvalidDiscussionID
	}
	if id == "." || id == ".." {
		return errInvalidDiscussionID
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			return errInvalidDiscussionID
		}
	}
	return nil
}
