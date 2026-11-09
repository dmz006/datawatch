// Release size of an Automaton: the explicit size a record may carry, and
// the validation that keeps unknown sizes out of the record. The layered
// resolution (Automation, project, built-in default) is decided elsewhere.
package autonomous

import "strings"

// Release sizes an Automaton may carry explicitly. The empty string means
// unset and is valid; it is not a constant here because it is the zero value.
const (
	ReleaseSizePatch = "patch"
	ReleaseSizeMinor = "minor"
	ReleaseSizeMajor = "major"
	ReleaseSizeNone  = "none"
)

// NormalizeReleaseSize trims and lowercases s and reports whether the result
// is a valid release size. Empty is valid and means unset.
func NormalizeReleaseSize(s string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "", ReleaseSizePatch, ReleaseSizeMinor, ReleaseSizeMajor, ReleaseSizeNone:
		return v, true
	}
	return v, false
}

// ValidReleaseSize reports whether v is an accepted release size. Empty is
// valid and means unset.
func ValidReleaseSize(v string) bool {
	_, ok := NormalizeReleaseSize(v)
	return ok
}
