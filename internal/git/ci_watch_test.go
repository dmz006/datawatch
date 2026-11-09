package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeCLI writes an executable shell script named name into dir. The
// script appends its arguments to <dir>/<name>.args and prints body.
func writeFakeCLI(t *testing.T, dir, name, body string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + filepath.Join(dir, name+".args") + "\"\n" +
		"cat <<'FAKE_EOF'\n" + body + "\nFAKE_EOF\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil { // #nosec G306 -- test helper script must be executable
		t.Fatal(err)
	}
}

// useFakeCLIs puts a temp dir holding the fake CLIs first on PATH.
func useFakeCLIs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func readArgs(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name+".args"))
	if err != nil {
		t.Fatalf("%s was not called: %v", name, err)
	}
	return string(b)
}

func TestHostCIWatch(t *testing.T) {
	t.Run("github maps gh run list output", func(t *testing.T) {
		dir := useFakeCLIs(t)
		writeFakeCLI(t, dir, "gh", `[{"databaseId":12345,"name":"CI","status":"completed","conclusion":"success","headSha":"abc123","url":"https://github.com/o/r/actions/runs/12345"},{"databaseId":12346,"name":"Lint","status":"in_progress","conclusion":"","headSha":"def456","url":"https://github.com/o/r/actions/runs/12346"}]`)
		host := GitHostConfig{Name: "gh", Kind: "github"}
		runs, err := LatestRuns(context.Background(), host, "o/r", "feature/x")
		if err != nil {
			t.Fatalf("LatestRuns: %v", err)
		}
		args := readArgs(t, dir, "gh")
		for _, want := range []string{"run list", "--repo o/r", "--branch feature/x", "--json databaseId,name,status,conclusion,headSha,url"} {
			if !strings.Contains(args, want) {
				t.Errorf("gh args %q missing %q", args, want)
			}
		}
		if len(runs) != 2 {
			t.Fatalf("runs = %d, want 2", len(runs))
		}
		want := CIRun{ID: "12345", Name: "CI", Status: "completed", Conclusion: "success", SHA: "abc123", URL: "https://github.com/o/r/actions/runs/12345"}
		if runs[0] != want {
			t.Errorf("runs[0] = %+v, want %+v", runs[0], want)
		}
		if runs[1].ID != "12346" || runs[1].Status != "in_progress" || runs[1].Conclusion != "" {
			t.Errorf("runs[1] = %+v", runs[1])
		}
	})

	t.Run("github empty list is not an error", func(t *testing.T) {
		dir := useFakeCLIs(t)
		writeFakeCLI(t, dir, "gh", `[]`)
		runs, err := LatestRuns(context.Background(), GitHostConfig{Kind: "github"}, "o/r", "main")
		if err != nil {
			t.Fatalf("LatestRuns: %v", err)
		}
		if len(runs) != 0 {
			t.Errorf("runs = %+v, want none", runs)
		}
		_ = dir
	})

	t.Run("gitlab maps pipelines with escaped project and ref", func(t *testing.T) {
		dir := useFakeCLIs(t)
		writeFakeCLI(t, dir, "glab", `[{"id":987,"status":"success","sha":"fedcba","web_url":"https://gitlab.example.test/g/p/-/pipelines/987"}]`)
		host := GitHostConfig{Name: "gl", Kind: "gitlab", BaseURL: "https://gitlab.example.test"}
		runs, err := LatestRuns(context.Background(), host, "group/sub/project", "release/1.0")
		if err != nil {
			t.Fatalf("LatestRuns: %v", err)
		}
		args := readArgs(t, dir, "glab")
		if !strings.Contains(args, "api projects/group%2Fsub%2Fproject/pipelines?ref=release%2F1.0") {
			t.Errorf("glab args %q missing escaped project path and ref", args)
		}
		want := CIRun{ID: "987", Status: "success", SHA: "fedcba", URL: "https://gitlab.example.test/g/p/-/pipelines/987"}
		if len(runs) != 1 || runs[0] != want {
			t.Errorf("runs = %+v, want [%+v]", runs, want)
		}
	})

	t.Run("invalid json is reported", func(t *testing.T) {
		dir := useFakeCLIs(t)
		writeFakeCLI(t, dir, "gh", `not json`)
		if _, err := LatestRuns(context.Background(), GitHostConfig{Kind: "github"}, "o/r", "main"); err == nil || !strings.Contains(err.Error(), "decode") {
			t.Errorf("err = %v, want decode error", err)
		}
		_ = dir
	})

	t.Run("missing cli is reported", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := LatestRuns(context.Background(), GitHostConfig{Kind: "github"}, "o/r", "main")
		if err == nil || !strings.Contains(err.Error(), "not installed") {
			t.Errorf("err = %v, want not-installed error", err)
		}
	})

	t.Run("unsupported kind and empty inputs are rejected", func(t *testing.T) {
		ctx := context.Background()
		if _, err := LatestRuns(ctx, GitHostConfig{Kind: "git"}, "o/r", "main"); err == nil {
			t.Error("git kind: want error")
		}
		if _, err := LatestRuns(ctx, GitHostConfig{Kind: "github"}, " ", "main"); err == nil {
			t.Error("empty repo: want error")
		}
		if _, err := LatestRuns(ctx, GitHostConfig{Kind: "github"}, "o/r", ""); err == nil {
			t.Error("empty ref: want error")
		}
	})

	t.Run("cancelled context returns its error", func(t *testing.T) {
		dir := useFakeCLIs(t)
		writeFakeCLI(t, dir, "gh", `[]`)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := LatestRuns(ctx, GitHostConfig{Kind: "github"}, "o/r", "main"); err == nil {
			t.Error("want error from cancelled context")
		}
	})
}
