// The shared writer's refusals hold for this installer too: a file it may not
// write, or whose bytes it would change, is left as it is.

package hookinstaller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLeavesAFileItMayNotReplace(t *testing.T) {
	cases := map[string]struct {
		body string
		mode os.FileMode
	}{
		"read-only":  {`{"permissions":{"deny":["Bash(rm:*)"]}}`, 0o444},
		"not UTF-8":  {"{\"note\":\"\xff\"}", 0o644},
		"write-only": {`{"model":"x"}`, 0o222},
	}
	for label, tc := range cases {
		for folder, make := range folders(t) {
			t.Run(label+"/"+folder, func(t *testing.T) {
				if tc.mode != 0o644 && os.Geteuid() == 0 {
					t.Skip("root is not stopped by file modes")
				}
				dir, project := make()
				settings := filepath.Join(project, ".claude", "settings.json")
				put(t, settings, tc.body)
				if err := os.Chmod(settings, tc.mode); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(settings, 0o644) })
				err := Install(dir, "host-aa01", "http://localhost:1", "")
				if err == nil || !strings.Contains(err.Error(), "not installed") {
					t.Fatalf("err = %v, want one saying hooks were not installed", err)
				}
				info, err := os.Stat(settings)
				if err != nil || info.Mode().Perm() != tc.mode {
					t.Fatalf("mode = %v, %v; want %v", info.Mode().Perm(), err, tc.mode)
				}
				_ = os.Chmod(settings, 0o644)
				if got, _ := os.ReadFile(settings); string(got) != tc.body {
					t.Errorf("settings changed: %q", got)
				}
				for _, f := range installedFiles {
					if _, err := os.Stat(filepath.Join(project, f)); !os.IsNotExist(err) {
						t.Errorf("%s was written: %v", f, err)
					}
				}
			})
		}
	}
}
