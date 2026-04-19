// F10 sprint 5 (S5.4) — post-session PR-on-complete hook.
//
// When a session bound to an agent (S3.6) reaches a terminal state,
// and that agent's Project Profile has Git.AutoPR set, the parent
// pushes the worker's working branch back to the project's repo and
// opens a PR via the configured git Provider.
//
// Wired via session.Manager.SetOnSessionEnd(NewPostSessionPRHook(…))
// in cmd/datawatch/main.go. The hook itself lives here so it can
// own the agent → project → git-token resolution chain without
// pulling agent internals into cmd/main.

package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/git"
)

// PRHookConfig groups the dependencies the hook needs. None of the
// fields are nil-safe at construction; New verifies them.
type PRHookConfig struct {
	Manager  *Manager       // for AgentID → Project + GitToken lookup
	Provider git.Provider   // for OpenPR call (gh / glab / stub)
	Pusher   BranchPusher   // pushes the working branch (project session.ProjectGit)
	Now      func() time.Time
}

// BranchPusher is the narrow surface the hook needs from
// session.ProjectGit. Decoupling here keeps the agents package free
// of an internal/session import (would create a cycle).
type BranchPusher interface {
	// CurrentBranch returns the checked-out branch in the worker's
	// project dir.
	CurrentBranch(projectDir string) (string, error)
	// PushBranch pushes branch to the remote, ephemerally injecting
	// token into the HTTPS URL so it doesn't persist in .git/config.
	PushBranch(projectDir, remoteName, branch, originURL, token string) error
}

// SessionLike is the subset of session.Session the hook reads. Same
// reason — keeps the import edge clean.
type SessionLike interface {
	GetID() string
	GetAgentID() string
	GetProjectDir() string
	GetTask() string
}

// PostSessionPRHook returns a callback compatible with
// session.Manager.SetOnSessionEnd that opens a PR for the worker's
// changes when the session belongs to an agent whose Project Profile
// has Git.AutoPR=true.
//
// The hook is best-effort: any failure (no agent, no token, push
// failed, OpenPR returned ErrNotImplemented for the provider) is
// logged but does NOT block the session-end callback chain. The
// session record's UpdatedAt timestamp is the point-in-time anchor
// for downstream observers.
func PostSessionPRHook(cfg PRHookConfig, log func(string, ...interface{})) func(SessionLike) {
	if log == nil {
		log = func(string, ...interface{}) {}
	}
	return func(s SessionLike) {
		agentID := s.GetAgentID()
		if agentID == "" {
			return // not a worker session — nothing to PR
		}
		if cfg.Manager == nil || cfg.Provider == nil || cfg.Pusher == nil {
			log("[pr-hook] config incomplete; skipping (sess=%s agent=%s)", s.GetID(), agentID)
			return
		}
		proj := cfg.Manager.GetProjectFor(agentID)
		if proj == nil {
			log("[pr-hook] no project profile for agent=%s", agentID)
			return
		}
		if !proj.Git.AutoPR {
			return // explicit opt-in; respected
		}
		token := cfg.Manager.GetGitTokenFor(agentID)
		if token == "" {
			log("[pr-hook] no git token for agent=%s; skipping PR", agentID)
			return
		}
		dir := s.GetProjectDir()
		if dir == "" {
			log("[pr-hook] session %s has no project_dir; skipping", s.GetID())
			return
		}

		branch, err := cfg.Pusher.CurrentBranch(dir)
		if err != nil || branch == "" || branch == "HEAD" {
			log("[pr-hook] couldn't resolve branch in %s: %v (branch=%q)", dir, err, branch)
			return
		}

		if err := cfg.Pusher.PushBranch(dir, "origin", branch, proj.Git.URL, token); err != nil {
			log("[pr-hook] push failed: %v", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		title := truncate("F10 worker: "+s.GetTask(), 70)
		body := fmt.Sprintf(
			"Automated PR opened by datawatch parent on session end.\n\n"+
				"- agent_id: `%s`\n"+
				"- session_id: `%s`\n"+
				"- branch: `%s`\n",
			agentID, s.GetID(), branch)

		url, err := cfg.Provider.OpenPR(ctx, git.PROptions{
			Repo:       repoFromGitURL(proj.Git.URL),
			HeadBranch: branch,
			BaseBranch: proj.Git.Branch, // "" → provider default branch
			Title:      title,
			Body:       body,
		})
		if err != nil {
			log("[pr-hook] OpenPR failed: %v", err)
			return
		}
		log("[pr-hook] PR opened: %s", url)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// _ keeps strings referenced if future hook helpers stop importing it.
var _ = strings.TrimSpace
