// BL407 Phase 2 — PRD-completion push + PR for worktree mode. One PR
// per PRD (not one per task), triggered when a PRD rolls up to
// PRDCompleted, not at individual session/task-end — the key
// difference from internal/agents' PostSessionPRHook, which would
// open one PR per task if naively reused at PRD scope.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// repoFromGitURL extracts "owner/repo" from common GitHub URL forms:
//
//	https://github.com/owner/repo(.git)
//	git@github.com:owner/repo(.git)
//
// Mirrors internal/agents/spawn.go's own unexported helper of the
// same name — duplicated rather than imported (internal/autonomous
// reaching into internal/agents would invert the dependency this
// codebase's "mirror, don't import" convention relies on throughout).
func repoFromGitURL(url string) string {
	url = strings.TrimSuffix(url, ".git")
	if i := strings.Index(url, ":"); i > 0 && strings.HasPrefix(url, "git@") {
		return url[i+1:]
	}
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return url
}

// handleWorktreeCompletion runs once a PRD rolls up to PRDCompleted.
// No-op unless the PRD actually ran in worktree mode (Git.Branch set
// by EnsureWorktree, Phase 1) and AutoPR is on — every other PRD is
// left exactly as Run() already leaves it today. Best-effort
// throughout, same shape as PostSessionPRHook/fireUpstreamIssueActions:
// a push/PR failure is logged and recorded on the PRD as a Decision,
// never fails Run() itself. The worktree is only removed once the
// branch is safely pushed AND the PR is open (Decision 5) — a failure
// anywhere in between leaves it in place for operator inspection,
// rather than risk deleting the only copy of unpushed work.
func (m *Manager) handleWorktreeCompletion(prd *PRD) {
	if prd.ClusterProfile != "" {
		// Cluster-dispatched PRDs never created a local worktree
		// (EnsureWorktree's gating requires ClusterProfile == "") —
		// handleClusterCompletion handles their PR instead.
		return
	}
	if prd.Git.Branch == "" || !prd.Git.AutoPR {
		return
	}
	if out, err := runGitIn(prd.ProjectDir, "push", "-u", "origin", prd.Git.Branch); err != nil {
		log.Printf("[autonomous] prd=%s git push failed: %v\n%s", prd.ID, err, out)
		m.recordGitCompletionFailure(prd, "push failed: "+err.Error())
		return
	}

	url := strings.TrimSpace(prd.Git.URL)
	if url == "" {
		if out, err := runGitIn(prd.ProjectDir, "remote", "get-url", "origin"); err == nil {
			url = strings.TrimSpace(out)
		}
	}
	if url == "" {
		log.Printf("[autonomous] prd=%s git push succeeded but no origin URL resolvable — cannot open PR", prd.ID)
		m.recordGitCompletionFailure(prd, "pushed, but could not resolve the origin remote URL to open a PR")
		return
	}

	if _, err := m.openCompletionPR(prd, url, ""); err != nil {
		m.recordGitCompletionFailure(prd, "pushed, but open PR failed: "+err.Error())
		return
	}

	// The worktree directory no longer exists on disk after this;
	// ProjectDir is deliberately left as a historical record of where
	// the PRD ran (same convention as other fields that survive their
	// underlying resource being torn down, e.g. Task.SessionID).
	// --force: the branch is already safely pushed above, so a stray
	// untracked/uncommitted leftover (a .gitignore edit from
	// EnsureIgnoredPatterns, a stray log file, ...) blocking cleanup
	// would just orphan the worktree directory for no benefit — the
	// PR already reflects everything that was actually committed.
	if out, werr := runGitIn(prd.ProjectDir, "worktree", "remove", "--force", prd.ProjectDir); werr != nil {
		log.Printf("[autonomous] prd=%s worktree remove failed (non-fatal, left for inspection): %v\n%s", prd.ID, werr, out)
	}
}

// handleClusterCompletion (BL407 Phase 3) is handleWorktreeCompletion's
// counterpart for cluster-dispatched PRDs. There is no local push and
// no worktree to clean up here: the worker committed and pushed its
// own branch directly (agents.PushOnCompletion, using the bootstrap-
// minted token the parent never sees), reporting the branch name back
// via AgentResult. cmd/datawatch/main.go's autonomousVerify closure
// records that branch (plus Git.URL/Provider/BaseBranch, resolved from
// the dispatching ProjectProfile the same way the worker's own
// bootstrap response was built) onto prd.Git as soon as it's known —
// by the time a PRD reaches PRDCompleted, this is just "open the PR".
func (m *Manager) handleClusterCompletion(prd *PRD) {
	if prd.ClusterProfile == "" {
		return
	}
	if prd.Git.Branch == "" || !prd.Git.AutoPR {
		return
	}
	url := strings.TrimSpace(prd.Git.URL)
	if url == "" {
		log.Printf("[autonomous] prd=%s cluster worker reported branch %q but no git URL was resolvable — cannot open PR", prd.ID, prd.Git.Branch)
		m.recordGitCompletionFailure(prd, "worker pushed "+prd.Git.Branch+", but no git URL was resolvable (dispatching project profile has no Git.URL?) to open a PR")
		return
	}
	if _, err := m.openCompletionPR(prd, url, "worker already pushed its own commits for this branch; the parent never had a local copy."); err != nil {
		m.recordGitCompletionFailure(prd, "worker pushed "+prd.Git.Branch+", but open PR failed: "+err.Error())
	}
}

// openCompletionPR is the part handleWorktreeCompletion and
// handleClusterCompletion share: resolve the provider, open the PR,
// and record the outcome onto prd (PRURL/URL/Provider + a Decision).
// Callers are responsible for the push (or lack of one) that precedes
// this and for logging/recording their own push-side failures —
// openCompletionPR only ever fails at the "open PR" step itself.
func (m *Manager) openCompletionPR(prd *PRD, url, bodyNote string) (string, error) {
	provider := strings.TrimSpace(prd.Git.Provider)
	if provider == "" {
		provider = "github"
	}
	resolve := m.gitProviderFn
	if resolve == nil {
		resolve = git.Resolve
	}
	title := prd.Title
	if title == "" {
		title = "Automaton " + prd.ID
	}
	body := fmt.Sprintf("Automated PR opened by datawatch on PRD completion.\n\n"+
		"- prd_id: `%s`\n- branch: `%s`\n", prd.ID, prd.Git.Branch)
	if bodyNote != "" {
		body += "- note: " + bodyNote + "\n"
	}
	body += "\n" + prd.Spec
	prURL, err := resolve(provider).OpenPR(context.Background(), git.PROptions{
		Repo:       repoFromGitURL(url),
		HeadBranch: prd.Git.Branch,
		BaseBranch: prd.Git.BaseBranch,
		Title:      title,
		Body:       body,
	})
	if err != nil {
		log.Printf("[autonomous] prd=%s open PR failed: %v", prd.ID, err)
		return "", err
	}

	prd.Git.PRURL = prURL
	prd.Git.URL = url
	if prd.Git.Provider == "" {
		prd.Git.Provider = provider
	}
	prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "git_pr_opened", Actor: "autonomous", Note: prURL})
	if serr := m.store.SavePRD(prd); serr != nil {
		log.Printf("[autonomous] prd=%s save after PR open: %v", prd.ID, serr)
	}
	log.Printf("[autonomous] prd=%s opened PR %s", prd.ID, prURL)
	return prURL, nil
}

func (m *Manager) recordGitCompletionFailure(prd *PRD, note string) {
	prd.Decisions = append(prd.Decisions, Decision{At: time.Now(), Kind: "git_pr_failed", Actor: "autonomous", Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		log.Printf("[autonomous] prd=%s save after git completion failure: %v", prd.ID, err)
	}
}
