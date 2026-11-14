// Package git — provider-neutral host facade.
//
// host_facade.go lets callers work with a git host by its config entry
// (git.hosts) instead of by forge name. ResolveProjectHost picks the host a
// remote URL belongs to, and OpenChangeRequest, FileIssue and PublishRelease
// dispatch the neutral operation to the GitHub or GitLab provider for that
// host's kind. Plain git kinds (git, gitea, bitbucket) use GenericGit, which
// returns ErrNoForgeAPI for these three operations.

package git

import (
	"context"
	"net/url"
	"strings"
)

// Host kinds, mirroring the config package's git.hosts[].kind values. This
// package cannot import config (see GitHostConfig), so the names are repeated.
const (
	hostKindGitHub    = "github"
	hostKindGitLab    = "gitlab"
	hostKindGitea     = "gitea"
	hostKindBitbucket = "bitbucket"
	hostKindGit       = "git"
)

// HostResult is the neutral result of a host operation. URL is the public
// address of the created change request, issue or release. Kind is the
// host's kind and Host is its git.hosts name.
type HostResult struct {
	URL  string
	Kind string
	Host string
}

// hostForge is the set of forge operations the facade dispatches. GitHub,
// GitLab and GenericGit all satisfy it.
type hostForge interface {
	OpenPR(ctx context.Context, opts PROptions) (string, error)
	CreateIssue(ctx context.Context, opts IssueOptions) (string, error)
	CreateRelease(ctx context.Context, opts ReleaseOptions) (string, error)
}

// forgeFactory builds the provider for a resolved host. It is a variable so
// tests can substitute fakes for the GitHub and GitLab providers.
var forgeFactory = defaultForgeFactory

// defaultForgeFactory maps a host kind to its real provider.
func defaultForgeFactory(host GitHostConfig) hostForge {
	switch host.Kind {
	case hostKindGitHub:
		g := NewGitHub()
		g.Host = host
		return g
	case hostKindGitLab:
		g := NewGitLab()
		g.Host = host
		return g
	default:
		g := NewGenericGit(host.Kind, host.BaseURL)
		g.Host = host
		return g
	}
}

// builtinHosts are the defaults used when no configured host matches the
// remote: the public github.com and gitlab.com instances.
var builtinHosts = []GitHostConfig{
	{Name: hostKindGitHub, Kind: hostKindGitHub},
	{Name: hostKindGitLab, Kind: hostKindGitLab},
}

// ResolveProjectHost returns the git host that remoteURL belongs to. It
// compares the remote's host name with the host name of each configured
// BaseURL, in list order. When nothing matches, it falls back to the
// built-in github.com and gitlab.com defaults. The bool is false when no
// host matches.
func ResolveProjectHost(hosts []GitHostConfig, remoteURL string) (GitHostConfig, bool) {
	remote := remoteHostName(remoteURL)
	if remote == "" {
		return GitHostConfig{}, false
	}
	for _, h := range hosts {
		if h.BaseURL == "" {
			continue
		}
		if baseHostName(h.BaseURL) == remote {
			return h, true
		}
	}
	for _, h := range builtinHosts {
		if remote == defaultHostName(h.Kind) {
			return h, true
		}
	}
	return GitHostConfig{}, false
}

// defaultHostName is the public host name of a built-in kind.
func defaultHostName(kind string) string {
	if kind == hostKindGitLab {
		return "gitlab.com"
	}
	return "github.com"
}

// baseHostName returns the lowercase host name of a base URL. A base URL
// without a scheme is parsed as if it had https://.
func baseHostName(base string) string {
	base = strings.TrimSpace(base)
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// remoteHostName returns the lowercase host name of a git remote. It accepts
// URL forms (https://, ssh://, git://) and scp-like forms (user@host:path).
func remoteHostName(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	if strings.Contains(remote, "://") {
		return baseHostName(remote)
	}
	// scp-like: [user@]host:path
	hostPart, _, ok := strings.Cut(remote, ":")
	if !ok {
		return ""
	}
	if at := strings.LastIndex(hostPart, "@"); at >= 0 {
		hostPart = hostPart[at+1:]
	}
	return strings.ToLower(strings.TrimSpace(hostPart))
}

// OpenChangeRequest opens a pull or merge request on host and returns its
// URL.
func OpenChangeRequest(ctx context.Context, host GitHostConfig, opts PROptions) (HostResult, error) {
	u, err := forgeFactory(host).OpenPR(ctx, opts)
	RecordHostOp(HostOpPullRequest, err == nil)
	if err == nil {
		recordHostOperation(ctx, host, HostAuditMR, opts.Repo, opts.HeadBranch)
	}
	return hostResult(host, u), err
}

// FileIssue files an issue on host and returns its URL.
func FileIssue(ctx context.Context, host GitHostConfig, opts IssueOptions) (HostResult, error) {
	u, err := forgeFactory(host).CreateIssue(ctx, opts)
	if err == nil {
		recordHostOperation(ctx, host, HostAuditIssue, opts.Repo, opts.Title)
	}
	return hostResult(host, u), err
}

// PublishRelease publishes a release for an existing tag on host and returns
// its URL.
func PublishRelease(ctx context.Context, host GitHostConfig, opts ReleaseOptions) (HostResult, error) {
	u, err := forgeFactory(host).CreateRelease(ctx, opts)
	if err == nil {
		recordHostOperation(ctx, host, HostAuditRelease, opts.Repo, opts.Tag)
	}
	return hostResult(host, u), err
}

// hostResult builds the neutral result for host with the given URL.
func hostResult(host GitHostConfig, u string) HostResult {
	return HostResult{URL: u, Kind: host.Kind, Host: host.Name}
}
