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
	"context"
	"net/url"
	"strings"
	"time"
)

// GitTokenMinter is the daemon-side equivalent of agents.GitTokenMinter:
// mint-then-revoke per-spawn tokens via the BL113 broker. Wired from
// main.go via SetGitTokenMinter; nil = fall back to env-token /
// local-creds path.
//
// v5.26.24 — operator-asked: BL113 token-broker integration so the
// daemon-side clone of project_profile-based PRDs doesn't need the
// long-lived DATAWATCH_GIT_TOKEN env in the Pod. With the broker
// wired, the clone handler mints a 5-minute token scoped to the
// repo, uses it for the clone, then revokes.
type GitTokenMinter interface {
	MintForWorker(ctx context.Context, workerID, repo string, ttl time.Duration) (string, error)
	RevokeForWorker(ctx context.Context, workerID string) error
}

// repoFromGitURL extracts the "owner/repo" path from a git URL.
// Mirrors internal/agents.repoFromGitURL (kept private to that
// package) since both daemon-side clone paths need the same
// resolution. Returns the original URL on unrecognizable shapes so
// the broker surfaces a clear error rather than silently using the
// wrong scope.
func repoFromGitURL(rawURL string) string {
	rawURL = strings.TrimSuffix(rawURL, ".git")
	// SSH: git@host:owner/repo
	if strings.HasPrefix(rawURL, "git@") {
		if i := strings.Index(rawURL, ":"); i > 0 {
			return rawURL[i+1:]
		}
	}
	// HTTPS: https://host/owner/repo — take last 2 path segments.
	parts := strings.Split(rawURL, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return rawURL
}

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
