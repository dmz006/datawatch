package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func opencodeOpts() ProjectConfigOpts {
	return ProjectConfigOpts{Model: "ollama/m1", DenyTools: []string{"webfetch"}, BuildTemperature: 0.2}
}

func TestOpencodeConfigThatCannotBeReadIsLeftByteForByte(t *testing.T) {
	for what, body := range map[string]string{
		"a comment":                 "{\n // mine\n \"theme\":\"x\"\n}",
		"a trailing comma":          `{"theme":"x",}`,
		"cut off":                   `{"theme":`,
		"permission not an object":  `{"permission":"allow"}`,
		"agent not an object":       `{"agent":[]}`,
		"provider not an object":    `{"provider":1}`,
		"build agent not an object": `{"agent":{"build":"x"}}`,
	} {
		t.Run(what, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "opencode.json")
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			err := WriteProjectConfig(dir, opencodeOpts())
			if err == nil || !strings.Contains(err.Error(), "left as it is") {
				t.Errorf("err = %v, want one saying the file was left as it is", err)
			}
			if got, _ := os.ReadFile(path); string(got) != body {
				t.Errorf("rewritten: %s", got)
			}
		})
	}
}

func TestOpencodeConfigRewriteKeepsWhatItDoesNotKnow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	body := `{"theme":"gruvbox","mcp":{"mine":{"type":"remote","url":"https://mcp.invalid"}},` +
		`"permission":{"bash":{"git push *":"deny"}},"agent":{"review":{"prompt":"p"}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteProjectConfig(dir, opencodeOpts()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{"gruvbox", "https://mcp.invalid", "git push *", `"review"`, "ollama/m1", `"webfetch"`, "temperature"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q missing:\n%s", want, got)
		}
	}
}
