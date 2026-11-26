// Installing hooks never rewrites a settings file it could not read in full,
// and never writes the settings that apply to everything a user runs.

package hookinstaller

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tree is every file and link under dir with its content, .git left out.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // an unreadable file is listed by its parent's walk
		}
		rel, _ := filepath.Rel(dir, p)
		if info.IsDir() {
			if info.Name() == ".git" && rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(p)
			out[rel] = "link:" + target
			return nil
		}
		b, _ := os.ReadFile(p)
		out[rel] = info.Mode().Perm().String() + " " + string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, before, after map[string]string) {
	t.Helper()
	for k, v := range after {
		if before[k] != v {
			t.Errorf("%s created or changed: %q", k, v)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			t.Errorf("%s removed", k)
		}
	}
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// folders makes a project folder both ways a settings file can be in one that
// git does not protect: outside any repository, and ignored by git.
func folders(t *testing.T) map[string]func() (string, string) {
	return map[string]func() (string, string){
		"not a git folder": func() (string, string) { dir := t.TempDir(); return dir, dir },
		"ignored by git": func() (string, string) {
			dir := t.TempDir()
			gitIn(t, dir, "init", "-q")
			put(t, filepath.Join(dir, ".gitignore"), ".claude/\n")
			return dir, dir
		},
	}
}

func mustRefuse(t *testing.T, project string) {
	t.Helper()
	err := Install(project, "host-aa01", "http://localhost:1", "")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("err = %v, want one saying hooks were not installed", err)
	}
}

func TestInstallNeverRewritesSettingsItCannotRead(t *testing.T) {
	const deny = `"permissions":{"deny":["Bash(rm -rf:*)"]}`
	bad := map[string]string{
		"a comment":             "{\n  // keep this\n  " + deny + "\n}\n",
		"a trailing comma":      "{" + deny + ",}",
		"text after the object": "{" + deny + "}\n{\"more\":1}",
		"cut off":               `{"permissions":{"deny":[`,
		"not an object":         `[{"x":1}]`,
		"null":                  `null`,
		"only spaces":           "  \n",
		"hooks not an object":   `{"hooks":[1],` + deny + `}`,
		"an event not a list":   `{"hooks":{"Stop":{"hooks":[]}},` + deny + `}`,
		"a key twice":           "{" + deny + `,"permissions":{}}`,
	}
	for where, mk := range folders(t) {
		for what, body := range bad {
			t.Run(where+"/"+what, func(t *testing.T) {
				root, project := mk()
				put(t, filepath.Join(project, ".claude", "settings.json"), body)
				before := tree(t, root)
				mustRefuse(t, project)
				sameTree(t, before, tree(t, root))
			})
		}
		t.Run(where+"/unreadable", func(t *testing.T) {
			if os.Geteuid() == 0 {
				t.Skip("root reads everything")
			}
			_, project := mk()
			path := filepath.Join(project, ".claude", "settings.json")
			put(t, path, "{"+deny+"}")
			if err := os.Chmod(path, 0o200); err != nil { // writable, not readable
				t.Fatal(err)
			}
			mustRefuse(t, project)
			_ = os.Chmod(path, 0o644)
			if got, _ := os.ReadFile(path); string(got) != "{"+deny+"}" {
				t.Errorf("rewritten: %s", got)
			}
		})
		t.Run(where+"/a folder", func(t *testing.T) {
			_, project := mk()
			if err := os.MkdirAll(filepath.Join(project, ".claude", "settings.json"), 0o755); err != nil {
				t.Fatal(err)
			}
			mustRefuse(t, project)
			if _, err := os.Stat(filepath.Join(project, ".claude", "sprint", "post-event.sh")); err == nil {
				t.Error("hook files written beside it")
			}
		})
	}
}

// What can be read is kept: every value, and the file's mode.
func TestInstallKeepsWhatItMergesInto(t *testing.T) {
	for where, mk := range folders(t) {
		t.Run(where, func(t *testing.T) {
			_, project := mk()
			path := filepath.Join(project, ".claude", "settings.json")
			put(t, path, `{"big":12345678901234567890,"permissions":{"deny":["Bash(a && b > c)"]}}`)
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := Install(project, "host-aa01", "http://localhost:1", ""); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			for _, want := range []string{"12345678901234567890", "Bash(a && b > c)", "post-event.sh"} {
				if !strings.Contains(string(got), want) {
					t.Errorf("%q missing from:\n%s", want, got)
				}
			}
			if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
				t.Errorf("mode = %v, want 0600", info.Mode().Perm())
			}
		})
	}
}

func TestInstallRefusesTheHomeFolderAndUserSettings(t *testing.T) {
	const mine = `{"model":"opus"}`
	cases := map[string]func(t *testing.T, home string) string{
		"home with settings": func(t *testing.T, home string) string {
			put(t, filepath.Join(home, ".claude", "settings.json"), mine)
			return home
		},
		"home without settings": func(t *testing.T, home string) string { return home },
		"home by another name": func(t *testing.T, home string) string {
			link := filepath.Join(t.TempDir(), "h")
			if err := os.Symlink(home, link); err != nil {
				t.Skip(err)
			}
			return link + string(os.PathSeparator) + "."
		},
		"a home repository": func(t *testing.T, home string) string {
			gitIn(t, home, "init", "-q")
			put(t, filepath.Join(home, ".gitignore"), ".claude/\n")
			return home
		},
		"project .claude links to the user's": func(t *testing.T, home string) string {
			put(t, filepath.Join(home, ".claude", "settings.json"), mine)
			project := t.TempDir()
			if err := os.Symlink(filepath.Join(home, ".claude"), filepath.Join(project, ".claude")); err != nil {
				t.Skip(err)
			}
			return project
		},
		"project settings link to the user's": func(t *testing.T, home string) string {
			put(t, filepath.Join(home, ".claude", "settings.json"), mine)
			project := t.TempDir()
			_ = os.MkdirAll(filepath.Join(project, ".claude"), 0o755)
			if err := os.Symlink(filepath.Join(home, ".claude", "settings.json"), filepath.Join(project, ".claude", "settings.json")); err != nil {
				t.Skip(err)
			}
			return project
		},
		"the CLAUDE_CONFIG_DIR folder": func(t *testing.T, home string) string {
			project := t.TempDir()
			put(t, filepath.Join(project, ".claude", "settings.json"), mine)
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(project, ".claude"))
			return project
		},
	}
	for label, mk := range cases {
		t.Run(label, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("CLAUDE_CONFIG_DIR", "")
			project := mk(t, home)
			beforeHome, beforeProject := tree(t, home), tree(t, project)
			mustRefuse(t, project)
			sameTree(t, beforeHome, tree(t, home))
			sameTree(t, beforeProject, tree(t, project))
		})
	}
	t.Run("a project inside home is fine", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("CLAUDE_CONFIG_DIR", "")
		project := filepath.Join(home, "work", "p")
		_ = os.MkdirAll(project, 0o755)
		if err := Install(project, "host-aa01", "http://localhost:1", ""); err != nil {
			t.Fatal(err)
		}
	})
}
