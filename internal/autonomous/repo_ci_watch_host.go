// repo_ci_watch_host.go — the configured-host branch of the repo CI watch
// poll pass (repo_ci_watch_poll.go). A watched repository whose origin
// resolves to an entry in git.hosts is listed through the host CI facade
// (git.LatestRuns, which uses gh for GitHub hosts and glab for GitLab
// hosts) instead of the per-SHA `gh run list --commit` path. A repository
// that does not resolve to a configured host keeps the gh path unchanged.
//
// Only the run listing changes on this branch. Head and tag resolution
// stay on git, and the action phase (cancel, rerun, delete, superseded
// naming) is skipped: those writes are GitHub-specific and are keyed by
// owner/repo, so a host-path run is recorded but never acted on.
//
// LatestRuns takes a branch or tag name, not a commit, so the host path
// asks for each watched ref by name and keeps only the runs whose SHA is
// that ref's resolved commit. LatestRuns does not return the workflow
// name, creation or start times; the host path therefore uses the run
// name as the workflow name, and runs without timestamps are never hung.
package autonomous

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/git"
)

// SetRepoCIGitHosts installs the configured git.hosts entries the repo CI
// watch resolves each watched repository against. The slice is copied.
// Nil or empty means no repository takes the host path.
func (m *Manager) SetRepoCIGitHosts(hosts []git.GitHostConfig) {
	if m == nil {
		return
	}
	cp := append([]git.GitHostConfig(nil), hosts...)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repoCIGitHosts = cp
}

// repoCIConfiguredHost returns the configured git host that remote (the
// origin URL) belongs to. Like the merge pipeline's configuredHost, the
// built-in github.com and gitlab.com fallbacks of ResolveProjectHost do
// not count: only an entry in git.hosts selects the host path, and the
// entry returned is the operator's own.
func (m *Manager) repoCIConfiguredHost(remote string) (git.GitHostConfig, bool) {
	if m == nil {
		return git.GitHostConfig{}, false
	}
	m.mu.Lock()
	hosts := append([]git.GitHostConfig(nil), m.repoCIGitHosts...)
	m.mu.Unlock()
	if len(hosts) == 0 || strings.TrimSpace(remote) == "" {
		return git.GitHostConfig{}, false
	}
	resolved, ok := git.ResolveProjectHost(hosts, remote)
	if !ok {
		return git.GitHostConfig{}, false
	}
	for _, h := range hosts {
		if h.Name == resolved.Name {
			return h, true
		}
	}
	return git.GitHostConfig{}, false
}

// repoCIHostRepoPath returns the project path of a git remote, the form
// LatestRuns takes as its repo argument: the URL path (or the scp-style
// path after the colon) with the leading slash and any trailing ".git"
// removed. An empty string means the remote has no usable path.
func repoCIHostRepoPath(remote string) string {
	remote = strings.TrimSpace(remote)
	var path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		path = u.Path
	} else if _, after, ok := strings.Cut(remote, ":"); ok {
		path = after
	} else {
		return ""
	}
	path = strings.Trim(strings.TrimSuffix(strings.TrimSpace(path), ".git"), "/")
	return path
}

// repoCIHostRuns lists the CI runs of repo on ref through the host
// facade. The call goes through repoCIHostRunsFn when a test installed
// one, otherwise git.LatestRuns.
func (m *Manager) repoCIHostRuns(ctx context.Context, host git.GitHostConfig, repo, ref string) ([]git.CIRun, error) {
	m.mu.Lock()
	fn := m.repoCIHostRunsFn
	m.mu.Unlock()
	if fn == nil {
		fn = git.LatestRuns
	}
	return fn(ctx, host, repo, ref)
}

// repoCIWatchRunFromCI maps one host CI run into the poll pass's run
// shape. GitHub statuses (queued, in_progress, completed) and conclusions
// pass through unchanged. GitLab pipeline statuses are mapped onto the
// same vocabulary so classify reads them the same way: a finished
// pipeline gets a conclusion, an unfinished one gets an in-flight status.
// A run whose ID is not a positive integer is reported as ok=false.
func repoCIWatchRunFromCI(r git.CIRun) (RepoCIWatchRun, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.ID), 10, 64)
	if err != nil || id <= 0 {
		return RepoCIWatchRun{}, false
	}
	status, conclusion := strings.ToLower(r.Status), strings.ToLower(r.Conclusion)
	switch status {
	case "success":
		status, conclusion = "completed", "success"
	case "failed":
		status, conclusion = "completed", "failure"
	case "canceled", "cancelled":
		status, conclusion = "completed", "cancelled"
	case "skipped":
		status, conclusion = "completed", "skipped"
	case "running":
		status = "in_progress"
	case "pending", "created", "waiting_for_resource", "preparing", "scheduled", "manual":
		status = "queued"
	case "completed":
		// GitHub's finished status: the conclusion carries the result.
	}
	return RepoCIWatchRun{
		ID:           id,
		Name:         r.Name,
		WorkflowName: r.Name,
		Status:       status,
		Conclusion:   conclusion,
		URL:          r.URL,
	}, true
}

// hostRunsForRef lists one watched ref's runs through the host facade and
// keeps the runs whose commit is the ref's resolved SHA. A run whose SHA
// does not match is another commit on the same branch and is left out.
func (p *repoCIWatchPollContext) hostRunsForRef(host git.GitHostConfig, repo string, ref repoCIWatchRef) ([]RepoCIWatchRun, error) {
	cis, err := p.m.repoCIHostRuns(p.ctx, host, repo, ref.name)
	if err != nil {
		return nil, err
	}
	runs := make([]RepoCIWatchRun, 0, len(cis))
	for _, ci := range cis {
		if !strings.EqualFold(ci.SHA, ref.sha) {
			continue
		}
		run, ok := repoCIWatchRunFromCI(ci)
		if !ok {
			continue
		}
		runs = append(runs, run)
	}
	return runs, nil
}
