// v5.26.22 — git credential abstraction for daemon-side clone of F10
// project profiles. Operator-asked: "Can the credentials for the
// recent changes be abstracted and documented to work in k8s?"
//
// k8s pattern: Helm chart mounts gitToken.existingSecret as the env
// DATAWATCH_GIT_TOKEN; daemon uses it for HTTPS clones automatically.
// Local pattern: no env → no rewrite → git uses whatever local
// credential helper / SSH agent the daemon user has. Either works.
//
// SSH URLs (git@host:...) are NOT rewritten — those use SSH agent /
// mounted Secret with the SSH key. Documented in
// docs/howto/setup-and-install.md.

package server

import (
	"net/url"
	"strings"
)

// injectGitToken rewrites an HTTPS git URL to embed the token in the
// userinfo portion. SSH URLs and HTTPS URLs that already carry
// userinfo are returned unchanged. The chosen username is
// `x-access-token` which works for GitHub + GitLab + most other
// providers as a "use the token directly" sentinel.
func injectGitToken(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	if !strings.HasPrefix(rawURL, "https://") && !strings.HasPrefix(rawURL, "http://") {
		// SSH / git protocol / file URL — no token rewrite.
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL // fall back; let git complain instead
	}
	// If userinfo is already set the operator has already encoded
	// auth. Don't overwrite — they may be using a different scheme.
	if u.User != nil {
		return rawURL
	}
	u.User = url.UserPassword("x-access-token", token)
	return u.String()
}

// redactGitToken removes a token we injected from an error / output
// blob before surfacing it to the operator. Best-effort: replaces
// any `x-access-token:<token>@` with `x-access-token:***@`. Also
// covers the rare case where the operator already embedded a token
// in the profile URL — both shapes get masked the same way so logs
// don't leak.
func redactGitToken(blob, originalURL string) string {
	out := blob
	// First, anything we injected.
	out = redactURLAuthPattern(out, "x-access-token")
	// Second, if the original URL had embedded auth, redact that too.
	if strings.Contains(originalURL, "@") {
		if u, err := url.Parse(originalURL); err == nil && u.User != nil {
			if user := u.User.Username(); user != "" && user != "x-access-token" {
				out = redactURLAuthPattern(out, user)
			}
		}
	}
	return out
}

// redactURLAuthPattern masks `<user>:<secret>@` occurrences. Naive
// but enough for the limited blast radius of git's stderr output.
func redactURLAuthPattern(blob, user string) string {
	prefix := user + ":"
	idx := 0
	out := ""
	for {
		i := strings.Index(blob[idx:], prefix)
		if i < 0 {
			out += blob[idx:]
			return out
		}
		startSecret := idx + i + len(prefix)
		endAt := strings.Index(blob[startSecret:], "@")
		if endAt < 0 {
			out += blob[idx:]
			return out
		}
		out += blob[idx:startSecret] + "***"
		idx = startSecret + endAt
	}
}
