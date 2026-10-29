package ci

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMainBuildsForReleaseTargets cross-compiles the daemon for the targets
// the release pipeline builds that Linux CI does not exercise. A call that
// exists only on Unix (syscall.Kill was one) passes every Linux test and then
// fails the release build for Windows, which went unnoticed across several
// tags. The build output is discarded; only the compiler's verdict matters.
func TestMainBuildsForReleaseTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles the daemon; skipped in -short")
	}
	targets := []struct{ goos, goarch string }{
		{"windows", "amd64"},
		{"windows", "arm64"},
		{"darwin", "arm64"},
	}
	for _, tg := range targets {
		tg := tg
		t.Run(tg.goos+"_"+tg.goarch, func(t *testing.T) {
			cmd := exec.Command("go", "build", "-o", os.DevNull, "./cmd/datawatch/")
			cmd.Dir = pwaRepoRoot(t)
			cmd.Env = append(os.Environ(), "GOOS="+tg.goos, "GOARCH="+tg.goarch, "CGO_ENABLED=0")
			out, err := cmd.CombinedOutput()
			if err != nil {
				msg := string(out)
				if strings.Contains(msg, "docs-bm25-index.json") {
					t.Skip("generated docs index is not present in this checkout")
				}
				t.Fatalf("go build for %s/%s failed:\n%s", tg.goos, tg.goarch, msg)
			}
		})
	}
}
