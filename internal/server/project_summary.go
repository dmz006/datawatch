// BL35 — project summary endpoint.
//
// GET /api/project/summary?dir=<abs-path>
// Returns a structured snapshot of a project directory:
//   - git status (branch, uncommitted, recent commits)
//   - sessions that have used this project_dir (recent first)
//   - aggregated stats (total runs, success rate, avg duration)
//
// dir is required and must be absolute. The endpoint itself is
// read-only and can run on any session manager state.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// ProjectSummary is the wire-format response.
type ProjectSummary struct {
	Dir            string             `json:"dir"`
	IsGitRepo      bool               `json:"is_git_repo"`
	Branch         string             `json:"branch,omitempty"`
	UncommittedCount int              `json:"uncommitted_count,omitempty"`
	RecentCommits  []ProjectCommit    `json:"recent_commits,omitempty"`
	Sessions       []ProjectSessionRef `json:"sessions,omitempty"`
	Stats          ProjectStats       `json:"stats"`
	GeneratedAt    time.Time          `json:"generated_at"`
}

// ProjectCommit summarises one git log entry.
type ProjectCommit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// ProjectSessionRef is a compact session-list row scoped to this project.
type ProjectSessionRef struct {
	ID        string    `json:"id"`
	Task      string    `json:"task"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
	DiffSummary string  `json:"diff_summary,omitempty"`
}

// ProjectStats is roll-ups across sessions for this project.
type ProjectStats struct {
	TotalSessions int     `json:"total_sessions"`
	Completed     int     `json:"completed"`
	Failed        int     `json:"failed"`
	Killed        int     `json:"killed"`
	SuccessRate   float64 `json:"success_rate"`
}

func (s *Server) handleProjectSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := strings.TrimSpace(r.URL.Query().Get("dir"))
	if dir == "" {
		http.Error(w, "dir query param required (must be absolute path)", http.StatusBadRequest)
		return
	}
	if !filepath.IsAbs(dir) {
		http.Error(w, "dir must be absolute", http.StatusBadRequest)
		return
	}
	out := buildProjectSummary(s, dir)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// buildProjectSummary collects the git + session signals; pure
// computation against the session manager + git CLI so it's testable
// with a FakeTmux-backed manager.
func buildProjectSummary(s *Server, dir string) ProjectSummary {
	out := ProjectSummary{Dir: dir, GeneratedAt: time.Now()}

	if isGitRepo(dir) {
		out.IsGitRepo = true
		if br, err := gitOutputCmd(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			out.Branch = strings.TrimSpace(br)
		}
		if status, err := gitOutputCmd(dir, "status", "--porcelain"); err == nil {
			out.UncommittedCount = countNonEmptyLines(status)
		}
		if log, err := gitOutputCmd(dir,
			"log", "-n", "10", "--pretty=format:%h\x1f%s\x1f%an\x1f%ad",
			"--date=short"); err == nil {
			for _, line := range strings.Split(log, "\n") {
				parts := strings.Split(line, "\x1f")
				if len(parts) != 4 {
					continue
				}
				out.RecentCommits = append(out.RecentCommits, ProjectCommit{
					Hash: parts[0], Subject: parts[1], Author: parts[2], Date: parts[3],
				})
			}
		}
	}

	if s.manager != nil {
		all := s.manager.ListSessions()
		matches := make([]*session.Session, 0, 8)
		for _, sess := range all {
			if sess.ProjectDir == dir {
				matches = append(matches, sess)
			}
		}
		sort.Slice(matches, func(i, j int) bool {
			return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
		})
		for _, sess := range matches {
			out.Stats.TotalSessions++
			switch sess.State {
			case session.StateComplete:
				out.Stats.Completed++
			case session.StateFailed:
				out.Stats.Failed++
			case session.StateKilled:
				out.Stats.Killed++
			}
			if len(out.Sessions) < 20 {
				out.Sessions = append(out.Sessions, ProjectSessionRef{
					ID: sess.ID, Task: sess.Task, State: string(sess.State),
					UpdatedAt: sess.UpdatedAt, DiffSummary: sess.DiffSummary,
				})
			}
		}
		if out.Stats.TotalSessions > 0 {
			out.Stats.SuccessRate =
				float64(out.Stats.Completed) / float64(out.Stats.TotalSessions)
		}
	}
	return out
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--is-inside-work-tree") // #nosec G702 -- argv-list invocation, not shell
	return cmd.Run() == nil
}

func gitOutputCmd(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...) // #nosec G702 -- argv-list invocation, not shell
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func countNonEmptyLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}
