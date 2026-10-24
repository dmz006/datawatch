package autonomous

import "strings"

// Session ids reach the autonomous package in two forms. The session lister
// reports a session by its full id (host name, a hyphen, then the 4-character
// short id), while a task stores only the short id the executor got back at
// spawn. The helpers here reduce either form to the short id so the two can be
// compared. The short id never contains a hyphen, so the text after the last
// hyphen is the short id even when the host name itself has hyphens.

// sessionShortID returns the short form of a session id: the text after the
// last hyphen, or the trimmed id when it has no hyphen. An empty id gives "".
func sessionShortID(id string) string {
	id = strings.TrimSpace(id)
	if i := strings.LastIndex(id, "-"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// sameSessionID reports whether a and b name the same session by short id.
// Two empty short ids never match.
func sameSessionID(a, b string) bool {
	sa, sb := sessionShortID(a), sessionShortID(b)
	return sa != "" && sa == sb
}
