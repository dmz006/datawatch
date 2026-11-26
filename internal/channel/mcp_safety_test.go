package channel

// Every rewrite of an .mcp.json or a Claude settings file keeps all of it, and
// one that cannot be read in full is left byte for byte.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A server of a kind datawatch does not write, and a top-level key it does not
// know.
const theirMCP = `"remote":{"type":"http","url":"https://mcp.invalid/x","headers":{"X-Key":"k1"}}`

func mcpWriters(t *testing.T) map[string]func(dir string) error {
	fakeNode(t)
	noBridge(t)
	return map[string]func(dir string) error{
		"project write": func(dir string) error { return WriteProjectMCPConfig(dir, "/path/to/channel.js", nil, nil) },
		"extra servers": func(dir string) error {
			return InjectExtrasIntoMCPConfig(dir, map[string]MCPServerSpec{"extra": {Command: "/bin/true"}})
		},
		"instance refresh": func(dir string) error {
			_, err := WriteInstanceMCPConfig(dir, "/path/to/channel.js", map[string]string{"DATAWATCH_TOKEN": "new"})
			return err
		},
		"remove the project entry":  func(dir string) error { return RemoveProjectMCPEntry(dir, ProjectMCPEntryName) },
		"remove the instance entry": func(dir string) error { return RemoveInstanceMCPConfig(dir) },
		"scrub an old token": func(dir string) error {
			_, err := ScrubInstanceMCPToken(dir, "old")
			return err
		},
	}
}

func TestMCPRewritesKeepWhatTheyDoNotKnow(t *testing.T) {
	for what, run := range mcpWriters(t) {
		t.Run(what, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".mcp.json")
			body := `{"$schema":"https://schema.invalid/mcp","mcpServers":{` + theirMCP +
				`,"datawatch":{"command":"/old","args":[],"env":{"DATAWATCH_TOKEN":"old"}}}}`
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := run(dir); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) == body {
				t.Fatal("setup: nothing was rewritten, so this proves nothing")
			}
			for _, want := range []string{"https://schema.invalid/mcp", "https://mcp.invalid/x", `"X-Key"`, `"http"`} {
				if !strings.Contains(string(got), want) {
					t.Errorf("%q lost:\n%s", want, got)
				}
			}
		})
	}
}

func TestMCPRewritesLeaveAFileTheyCannotReadByteForByte(t *testing.T) {
	bad := map[string]string{
		"a comment":               "{\n // mine\n \"mcpServers\":{" + theirMCP + "}}",
		"cut off":                 `{"mcpServers":{` + theirMCP,
		"servers not an object":   `{"mcpServers":[` + `{"a":1}` + `]}`,
		"the datawatch key twice": `{"mcpServers":{"datawatch":{},"datawatch":{"env":{"DATAWATCH_TOKEN":"old"}}}}`,
	}
	for what, run := range mcpWriters(t) {
		for kind, body := range bad {
			t.Run(what+"/"+kind, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, ".mcp.json")
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				_ = run(dir) // the writers say so; the removals just leave it
				if got, err := os.ReadFile(path); err != nil || string(got) != body {
					t.Errorf("changed to %q (err %v)", got, err)
				}
			})
		}
	}
	for what, run := range mcpWriters(t) {
		if strings.HasPrefix(what, "remove") || strings.HasPrefix(what, "scrub") {
			continue
		}
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(bad["a comment"]), 0o600)
		if err := run(dir); err == nil || !strings.Contains(err.Error(), "left as it is") {
			t.Errorf("%s: err = %v, want one saying the file was left as it is", what, err)
		}
	}
}

// The daemon runs this at every start on the user's own ~/.claude folder.
func TestEnsureClaudeSettingsNeverLosesTheUsersSettings(t *testing.T) {
	const deny = `"permissions":{"deny":["Bash(rm -rf:*)"]}`
	for what, body := range map[string]string{
		"a comment": "{\n // mine\n " + deny + "\n}\n", "a trailing comma": "{" + deny + ",}", "null": "null",
		"cut off": `{"permissions":`, "not an object": `[]`,
	} {
		t.Run(what, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			err := EnsureClaudeSettings(dir)
			if err == nil || !strings.Contains(err.Error(), "left as it is") {
				t.Errorf("err = %v, want one saying the file was left as it is", err)
			}
			if got, _ := os.ReadFile(path); string(got) != body {
				t.Errorf("rewritten: %s", got)
			}
		})
	}
	t.Run("a readable file keeps everything, and a link stays a link", func(t *testing.T) {
		dir, dotfiles := t.TempDir(), t.TempDir()
		real := filepath.Join(dotfiles, "claude.json")
		if err := os.WriteFile(real, []byte(`{"big":12345678901234567890,`+deny+`}`), 0o640); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "settings.json")
		if err := os.Symlink(real, path); err != nil {
			t.Skip(err)
		}
		if err := EnsureClaudeSettings(dir); err != nil {
			t.Fatal(err)
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the link was replaced: %v %v", info, err)
		}
		got, _ := os.ReadFile(real)
		for _, want := range []string{"12345678901234567890", "Bash(rm -rf:*)", "skipDangerousModePermissionPrompt"} {
			if !strings.Contains(string(got), want) {
				t.Errorf("%q missing: %s", want, got)
			}
		}
		if info, _ := os.Stat(real); info.Mode().Perm() != 0o640 {
			t.Errorf("mode = %v, want 0640", info.Mode().Perm())
		}
	})
}
