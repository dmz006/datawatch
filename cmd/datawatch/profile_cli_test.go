package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempFile writes content to a new file in t.TempDir and returns
// the path. Shared helper for CLI tests.
func writeTempFile(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatalf("writeTempFile: %v", err)
	}
	return path
}

// captureStdout runs fn with os.Stdout redirected to a pipe and
// returns everything fn wrote. Used to assert CLI formatters produce
// the expected text.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}

// The bulk of profile_cli.go is HTTP plumbing against a running
// daemon — covered end-to-end by the REST integration tests in the
// server package. Here we lock in the pure helpers: pluralize, yaml
// body conversion, and the table/json/yaml formatter dispatch.

func TestPluralize(t *testing.T) {
	cases := map[string]string{
		"project": "projects",
		"cluster": "clusters",
		"bogus":   "bogus", // pass-through for future kinds
	}
	for in, want := range cases {
		if got := pluralize(in); got != want {
			t.Errorf("pluralize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadProfileInput_JSON(t *testing.T) {
	// Minimal valid JSON; readProfileInput should pass through.
	input := []byte(`{"name":"foo"}`)
	tmp := writeTempFile(t, input)
	out, err := readProfileInput(tmp)
	if err != nil {
		t.Fatalf("readProfileInput: %v", err)
	}
	if !bytes.Equal(out, input) {
		t.Errorf("output mismatch: got %q want %q", out, input)
	}
}

func TestReadProfileInput_YAML(t *testing.T) {
	yaml := []byte("name: foo\ngit:\n  url: https://example.com/x\n")
	tmp := writeTempFile(t, yaml)
	out, err := readProfileInput(tmp)
	if err != nil {
		t.Fatalf("readProfileInput yaml: %v", err)
	}
	// Should now be JSON with the same structure
	var decoded map[string]interface{}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output not JSON: %v (raw: %s)", err, out)
	}
	if decoded["name"] != "foo" {
		t.Errorf("name=%v want foo", decoded["name"])
	}
	git, _ := decoded["git"].(map[string]interface{})
	if git["url"] != "https://example.com/x" {
		t.Errorf("git.url lost: %v", git)
	}
}

func TestReadProfileInput_Garbage(t *testing.T) {
	tmp := writeTempFile(t, []byte("this is { not valid"))
	_, err := readProfileInput(tmp)
	if err == nil {
		t.Errorf("garbage input should error")
	}
}

func TestProfileSummary_Project(t *testing.T) {
	item := map[string]interface{}{
		"name": "p",
		"image_pair": map[string]interface{}{
			"agent":   "agent-claude",
			"sidecar": "lang-go",
		},
	}
	got := profileSummary(item)
	if !strings.Contains(got, "agent-claude") || !strings.Contains(got, "lang-go") {
		t.Errorf("project summary missing parts: %q", got)
	}
}

func TestProfileSummary_ProjectSolo(t *testing.T) {
	item := map[string]interface{}{
		"name": "solo",
		"image_pair": map[string]interface{}{
			"agent":   "agent-claude",
			"sidecar": "",
		},
	}
	got := profileSummary(item)
	if !strings.Contains(got, "(solo)") {
		t.Errorf("solo agent summary should mention (solo), got %q", got)
	}
}

func TestProfileSummary_Cluster(t *testing.T) {
	item := map[string]interface{}{
		"name":    "c",
		"kind":    "k8s",
		"context": "testing",
		// namespace omitted → default
	}
	got := profileSummary(item)
	for _, want := range []string{"kind=k8s", "context=testing", "ns=default"} {
		if !strings.Contains(got, want) {
			t.Errorf("cluster summary missing %q: %q", want, got)
		}
	}
}

func TestRenderProfileOutput_JSONPrettyPrints(t *testing.T) {
	body := []byte(`{"name":"x","nested":{"a":1}}`)
	captured := captureStdout(t, func() {
		_ = renderProfileOutput(body, "json", "")
	})
	if !strings.Contains(captured, "\n  \"name\"") {
		t.Errorf("json format should pretty-print with 2-space indent, got:\n%s", captured)
	}
}

func TestRenderProfileOutput_Table_Empty(t *testing.T) {
	body := []byte(`{"profiles":[]}`)
	captured := captureStdout(t, func() {
		_ = renderProfileOutput(body, "table", "profiles")
	})
	if !strings.Contains(captured, "(no profiles)") {
		t.Errorf("empty table should say (no profiles), got:\n%s", captured)
	}
}

func TestRenderProfileOutput_Table_ListsNames(t *testing.T) {
	body := []byte(`{"profiles":[{"name":"a","image_pair":{"agent":"agent-claude","sidecar":"lang-go"}},{"name":"b","image_pair":{"agent":"agent-aider","sidecar":""}}]}`)
	captured := captureStdout(t, func() {
		_ = renderProfileOutput(body, "table", "profiles")
	})
	for _, n := range []string{"a", "b", "agent-claude", "(solo)"} {
		if !strings.Contains(captured, n) {
			t.Errorf("table missing %q, got:\n%s", n, captured)
		}
	}
}

func TestRenderProfileOutput_YAML(t *testing.T) {
	body := []byte(`{"name":"x","list":["a","b"]}`)
	captured := captureStdout(t, func() {
		_ = renderProfileOutput(body, "yaml", "")
	})
	if !strings.Contains(captured, "name: x") {
		t.Errorf("yaml output missing, got:\n%s", captured)
	}
}

func TestRenderProfileOutput_UnknownFormat(t *testing.T) {
	if err := renderProfileOutput([]byte(`{}`), "toml", ""); err == nil {
		t.Errorf("unknown format should error")
	}
}
