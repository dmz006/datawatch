// Keeping installed hook files out of a project's git history.

package hookinstaller

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// installedFiles are the files Install writes that no commit may take.
var installedFiles = []string{".claude/sprint/post-event.sh", ".claude/sprint/.dw-env", ".claude/sprint/platform-context.txt"}

const settingsFile = ".claude/settings.json"

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSuffix(string(out), "\n"), err
}

// keepOutOfCommits prepares a project that is in a git repository, before
// anything is written: the files Install creates are listed in the
// repository's own exclude file, which is not part of the project, so neither a
// worker's "git add -A" nor datawatch's commit can take them. It refuses when
// installing would change a file the project's commits can see: a tracked one,
// or the project's own settings.json that git does not ignore. A folder that is
// not in a repository needs nothing.
func keepOutOfCommits(projectDir string) error {
	if _, err := gitOut(projectDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil
	}
	for _, f := range append([]string{settingsFile}, installedFiles...) {
		if _, err := gitOut(projectDir, "ls-files", "--error-unmatch", "--", f); err == nil {
			return fmt.Errorf("hookinstaller: not installed: the project tracks %s in git", f)
		}
	}
	ignored := func(f string) bool {
		_, err := gitOut(projectDir, "check-ignore", "-q", "--", f)
		return err == nil
	}
	want := make([]string, 0, len(installedFiles)+1)
	for _, f := range installedFiles {
		if !ignored(f) {
			want = append(want, f)
		}
	}
	if !ignored(settingsFile) {
		if _, err := os.Stat(filepath.Join(projectDir, settingsFile)); err == nil {
			return fmt.Errorf("hookinstaller: not installed: the project has its own %s that git does not ignore", settingsFile)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("hookinstaller: not installed: %w", err)
		}
		want = append(want, settingsFile)
	}
	if len(want) == 0 {
		return nil
	}
	prefix, err := gitOut(projectDir, "rev-parse", "--show-prefix")
	if err != nil {
		return fmt.Errorf("hookinstaller: not installed: %w", err)
	}
	exclude, err := gitOut(projectDir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return fmt.Errorf("hookinstaller: not installed: %w", err)
	}
	if !filepath.IsAbs(exclude) {
		exclude = filepath.Join(projectDir, exclude)
	}
	old, err := os.ReadFile(exclude)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("hookinstaller: not installed: %w", err)
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(old), "\n") {
		have[l] = true
	}
	var add strings.Builder
	for _, f := range want {
		if line := "/" + prefix + f; !have[line] {
			add.WriteString(line + "\n")
		}
	}
	if add.Len() > 0 {
		body := string(old)
		if body != "" && !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		body += "# datawatch session hooks (installed files, not project work)\n" + add.String()
		if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
			return fmt.Errorf("hookinstaller: not installed: %w", err)
		}
		if err := os.WriteFile(exclude, []byte(body), 0o644); err != nil {
			return fmt.Errorf("hookinstaller: not installed: %w", err)
		}
	}
	// Check the result rather than trust the lines just written.
	for _, f := range want {
		if !ignored(f) {
			return fmt.Errorf("hookinstaller: not installed: git would still see %s", f)
		}
	}
	return nil
}
