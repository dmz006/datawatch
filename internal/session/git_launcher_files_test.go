package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rules-file fixtures, mirroring the launcher's real behavior: the repo
// commits the operator's base rules file, then the session launcher rewrites
// it at session start by appending the daemon's own sections (Memory &
// Knowledge, RTK, and in production also the platform-context and
// Session Guardrails sections). sessionAgentBase is the committed state;
// sessionAgentDaemon is the working copy the launcher leaves behind.
const sessionAgentBase = "# Project\n\nBuild with go build ./... .\n"

const sessionAgentDaemon = sessionAgentBase +
	"\n# Memory & Knowledge (datawatch)\n\nUse the memory tools first.\n\n" +
	"<!-- rtk-instructions -->\nalways prefix with rtk\n<!-- /rtk-instructions -->\n"

// launcherSessionRepo is a fresh git repo with one tracked worker file
// ("app.txt") and a committed root AGENT.md (sessionAgentBase). It mimics
// everything after a session starts in it: the launcher writes its two root
// tooling files (worker-mcp.json, worker-starting-context.md) and rewrites
// AGENT.md with the daemon sections appended, and the worker modifies its
// one real file. Nothing else in the tree changes.
func launcherSessionRepo(t *testing.T) (dir string, g *ProjectGit) {
	t.Helper()
	requireGit(t)
	dir = t.TempDir()
	gitRunT(t, dir, "init", "-q", "-b", "main")
	gitRunT(t, dir, "config", "user.email", "t@datawatch")
	gitRunT(t, dir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(sessionAgentBase), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, dir, "add", "app.txt", "AGENT.md")
	gitRunT(t, dir, "commit", "-q", "-m", "base")

	// Session start: the launcher's two root files appear, AGENT.md is
	// rewritten with the daemon sections, and the worker modifies its file.
	if err := os.WriteFile(filepath.Join(dir, "worker-mcp.json"), []byte(`{"mcpServers":{}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "worker-starting-context.md"), []byte("# Worker starting context\n\nWork from the task spec only.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(sessionAgentDaemon), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, NewProjectGit(dir)
}

// skipReason returns the recorded skip reason for path in skipped, "" if the
// path was not skipped.
func skipReason(skipped []StagingSkip, path string) string {
	for _, s := range skipped {
		if s.Path == path {
			return s.Reason
		}
	}
	return ""
}

// TestLauncherFilesStagingSkipped is the session-auto-commit side of the
// launcher fidelity guarantee: in both staging branches — scoped (scope is
// the repo root, so underAnyDir lets everything through) and unscoped
// (empty scope, files bounded to this run) — the launcher's two root files
// and the section-appended AGENT.md are refused with their reasons, while
// the worker's real modified file is staged.
func TestLauncherFilesStagingSkipped(t *testing.T) {
	check := func(t *testing.T, dir string, g *ProjectGit, scope []string) {
		t.Helper()
		skipped, err := g.stageChanges(scope)
		if err != nil {
			t.Fatalf("stageChanges: %v", err)
		}
		if got := skipReason(skipped, "worker-mcp.json"); got != "launcher" {
			t.Fatalf("worker-mcp.json: want reason launcher, got %q (skips: %v)", got, skipped)
		}
		if got := skipReason(skipped, "worker-starting-context.md"); got != "launcher" {
			t.Fatalf("worker-starting-context.md: want reason launcher, got %q (skips: %v)", got, skipped)
		}
		if got := skipReason(skipped, "AGENT.md"); got != "daemon-sections" {
			t.Fatalf("AGENT.md: want reason daemon-sections, got %q (skips: %v)", got, skipped)
		}
		cached := gitOut(t, dir, "diff", "--cached", "--name-only")
		if !strings.Contains(cached, "app.txt") {
			t.Fatalf("real modified file app.txt must be staged; index holds: %q", cached)
		}
		for _, p := range []string{"worker-mcp.json", "worker-starting-context.md", "AGENT.md"} {
			if strings.Contains(cached, p) {
				t.Fatalf("%s must not be staged; index holds: %q", p, cached)
			}
		}
	}

	t.Run("scoped", func(t *testing.T) {
		dir, g := launcherSessionRepo(t)
		check(t, dir, g, []string{dir})
	})
	t.Run("unscoped", func(t *testing.T) {
		dir, g := launcherSessionRepo(t)
		check(t, dir, g, nil)
	})
}

// TestDaemonSectionsOnlyRealEditStillStages is the fail-closed boundary:
// an AGENT.md that carries a real operator edit on top of the daemon's
// appended sections is the worker's work, so it must be staged — the
// daemon-sections skip is strictly "the entire diff is the appended
// sections" and nothing looser.
func TestDaemonSectionsOnlyRealEditStillStages(t *testing.T) {
	dir, g := launcherSessionRepo(t)
	// Real edit: an operator line above the daemon's appended sections,
	// which survives StripDaemonSections and breaks the "sections only"
	// predicate.
	content := sessionAgentBase +
		"\nOperator rule: always run go vet before finishing.\n" +
		strings.TrimPrefix(sessionAgentDaemon, sessionAgentBase)
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	skipped, err := g.stageChanges([]string{dir})
	if err != nil {
		t.Fatalf("stageChanges: %v", err)
	}
	if got := skipReason(skipped, "AGENT.md"); got != "" {
		t.Fatalf("AGENT.md with a real edit must be staged, but was skipped (%q)", got)
	}
	cached := gitOut(t, dir, "diff", "--cached", "--name-only")
	if !strings.Contains(cached, "AGENT.md") {
		t.Fatalf("AGENT.md with a real edit must be staged; index holds: %q", cached)
	}
}
