package memory

// The memory hook installer never rewrites a settings file it could not read in
// full, and never writes a user's own settings.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandboxHome gives the test its own home, so no real user file is in reach.
func sandboxHome(t *testing.T) (home, scripts string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	scripts = t.TempDir()
	EnsureHookScripts(scripts)
	return home, scripts
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func unchanged(t *testing.T, path, want string) {
	t.Helper()
	if got, err := os.ReadFile(path); err != nil || string(got) != want {
		t.Errorf("%s = %q (err %v), want it left as %q", path, got, err, want)
	}
}

func refused(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("err = %v, want one saying the hooks were not installed", err)
	}
}

func TestMemoryHooksLeaveAFileTheyCannotReadByteForByte(t *testing.T) {
	const deny = `"permissions":{"deny":["Bash(rm -rf:*)"]}`
	for what, body := range map[string]string{
		"a comment":             "{\n  // keep this\n  " + deny + "\n}\n",
		"a trailing comma":      "{" + deny + ",}",
		"text after the object": "{" + deny + "} x",
		"not an object":         `["x"]`,
		"null":                  `null`,
		"a key twice":           "{" + deny + `,"permissions":{}}`,
		"hooks not an object":   `{"hooks":[1],` + deny + `}`,
		"only spaces":           " \n",
	} {
		t.Run(what, func(t *testing.T) {
			_, scripts := sandboxHome(t)
			project := t.TempDir()
			path := filepath.Join(project, ".claude", "settings.local.json")
			write(t, path, body)
			refused(t, InstallClaudeHooks(project, scripts, 15))
			unchanged(t, path, body)
		})
	}
}

func TestMemoryHooksRefuseAHomeFolder(t *testing.T) {
	const mine = `{"permissions":{"deny":["Read(./secrets)"]}}`
	home, scripts := sandboxHome(t)
	path := filepath.Join(home, ".claude", "settings.local.json")
	write(t, path, mine)
	refused(t, InstallClaudeHooks(home, scripts, 15))
	unchanged(t, path, mine)

	empty, scripts2 := sandboxHome(t)
	refused(t, InstallClaudeHooks(empty, scripts2, 15))
	if _, err := os.Stat(filepath.Join(empty, ".claude")); err == nil {
		t.Error("a .claude folder was made in the home folder")
	}
	refused(t, InstallClaudeHooks("", scripts2, 15)) // no folder at all
}

func TestMemoryHooksRefuseALinkedFile(t *testing.T) {
	const theirs = `{"permissions":{"deny":["Read(./secrets)"]}}`
	cases := map[string]func(t *testing.T, project, target string) error{
		"the settings file": func(t *testing.T, project, target string) error {
			_ = os.MkdirAll(filepath.Join(project, ".claude"), 0o755)
			return os.Symlink(filepath.Join(target, "settings.local.json"), filepath.Join(project, ".claude", "settings.local.json"))
		},
		"the .claude folder": func(t *testing.T, project, target string) error {
			return os.Symlink(target, filepath.Join(project, ".claude"))
		},
	}
	for what, link := range cases {
		t.Run(what, func(t *testing.T) {
			_, scripts := sandboxHome(t)
			project, target := t.TempDir(), t.TempDir()
			write(t, filepath.Join(target, "settings.local.json"), theirs)
			if err := link(t, project, target); err != nil {
				t.Skip(err)
			}
			refused(t, InstallClaudeHooks(project, scripts, 15))
			unchanged(t, filepath.Join(target, "settings.local.json"), theirs)
		})
	}
}

func TestMemoryHooksKeepWhatTheyMergeInto(t *testing.T) {
	_, scripts := sandboxHome(t)
	project := t.TempDir()
	path := filepath.Join(project, ".claude", "settings.local.json")
	write(t, path, `{"big":12345678901234567890,"permissions":{"deny":["Bash(a && b > c)"]}}`)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := InstallClaudeHooks(project, scripts, 15); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{"12345678901234567890", "Bash(a && b > c)", "datawatch_save_hook.sh", "datawatch_precompact_hook.sh"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q missing from %s", want, got)
		}
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	if !HooksInstalled(project) {
		t.Error("not reported installed")
	}
}
