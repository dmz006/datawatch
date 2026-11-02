package ci

import (
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// Only these files may be tracked at the top of the repository. Scripts belong
// in scripts/, reports in docs/plans/reports/, evidence under docs/. A task
// that leaves a stray file at the root (a verifier verdict, an empty copy of a
// script, a report) fails this test instead of reaching main.
var allowedRootFiles = map[string]bool{
	".dockerignore": true, ".env.build.example": true, ".gitignore": true,
	".gitleaks.toml": true, ".golangci.yml": true, ".goreleaser.yaml": true,
	".gosec-baseline.json": true, ".gosec-exclude": true, ".trivyignore": true,
	"AGENT.md": true, "CHANGELOG.md": true, "CLAUDE.md": true, "CODEOWNERS": true,
	"CONTRIBUTING.md": true, "DATAWATCH-CONTEXT.md": true, "Dockerfile.dev": true,
	"LICENSE": true, "Makefile": true, "README.md": true, "SECURITY.md": true,
	"go.mod": true, "go.sum": true,
}

func TestOnlyKnownFilesAreTrackedAtTheRepositoryRoot(t *testing.T) {
	out, err := exec.Command("git", "-C", "../..", "ls-files").Output()
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	var stray []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f == "" || strings.Contains(f, "/") {
			continue
		}
		if !allowedRootFiles[f] {
			stray = append(stray, f)
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Errorf("files tracked at the repository root that are not on the allowed list (move them to scripts/ or docs/, or add them here on purpose): %v", stray)
	}
}
