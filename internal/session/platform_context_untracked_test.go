package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/platformcontext"
)

func untrackedGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// TestUntrackedDelivery_EveryFamily simulates a session start for every
// backend family and proves the platform context never dirties a tracked file
// and is delivered through exactly one channel.
func TestUntrackedDelivery_EveryFamily(t *testing.T) {
	families := make([]string, 0)
	for fam := range platformcontext.DeliveryModes() {
		families = append(families, fam)
	}
	sort.Strings(families)
	tracked := []string{"AGENT.md", "CLAUDE.md", "README.md"}

	for _, fam := range families {
		fam := fam
		t.Run(fam, func(t *testing.T) {
			dir := t.TempDir()
			untrackedGit(t, dir, "init", "-q")
			untrackedGit(t, dir, "config", "user.email", "t@example.com")
			untrackedGit(t, dir, "config", "user.name", "t")
			for _, f := range tracked {
				if err := os.WriteFile(filepath.Join(dir, f), []byte("# "+f+"\noriginal\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			untrackedGit(t, dir, "add", "-A")
			untrackedGit(t, dir, "commit", "-q", "-m", "init")

			mgr := newTestManager(t)
			sess := newTestSession(t, mgr, "ud-"+fam)
			sess.ProjectDir = dir
			sess.BackendFamily = fam

			if fam == "claude-code" {
				tr, err := NewTracker(t.TempDir(), &Session{
					FullID: "host-ud01", Task: "task", ProjectDir: dir,
					Hostname: "host", BackendFamily: fam, CreatedAt: time.Now(),
				})
				if err != nil {
					t.Fatal(err)
				}
				gs := &Session{FullID: "host-ud01", Task: "task", ProjectDir: dir, Hostname: "host", BackendFamily: fam, CreatedAt: time.Now()}
				if err := tr.WriteSessionGuardrails("", gs, GuardrailsOptions{PlatformContextEnabled: true}); err != nil {
					t.Fatal(err)
				}
				sess.PlatformContextLocalFile = gs.PlatformContextLocalFile
			}

			prompt := mgr.prependPlatformContextOnce(sess, fam, "do the task")
			prefixed := strings.Contains(prompt, platformcontext.PlatformContext) && strings.HasSuffix(prompt, "do the task")

			status := untrackedGit(t, dir, "status", "--porcelain", "--untracked-files=all")
			for _, line := range strings.Split(status, "\n") {
				for _, f := range tracked {
					if strings.HasSuffix(line, " "+f) {
						t.Errorf("tracked path %s has a status entry: %q", f, line)
					}
				}
			}
			if diff := untrackedGit(t, dir, "diff", "HEAD"); diff != "" {
				t.Errorf("git diff HEAD not empty:\n%s", diff)
			}

			inLocal := false
			if b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md")); err == nil {
				inLocal = strings.Contains(string(b), strings.TrimSpace(platformcontext.PlatformContext))
			}
			if fam == "claude-code" {
				if !inLocal || prefixed {
					t.Errorf("claude-code: local=%v prefixed=%v, want local only", inLocal, prefixed)
				}
			} else if inLocal || !prefixed {
				t.Errorf("%s: local=%v prefixed=%v, want first-prompt only", fam, inLocal, prefixed)
			}
		})
	}
}
