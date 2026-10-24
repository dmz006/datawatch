package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// internalRefsTree is a throwaway copy of every user-facing surface that
// scripts/check-no-internal-refs.sh audits, built under a t.TempDir()
// directory. Nothing here touches a real repository checkout or any running
// service.
type internalRefsTree struct {
	t    *testing.T
	root string
}

// fakeTrackerID builds an internal tracker ID at runtime from parts so this
// test source itself contains no literal tracker ID.
func fakeTrackerID() string {
	prefix := "B" + "L"
	return prefix + "123"
}

// newInternalRefsTree copies the real check script (resolved relative to this
// test file, like newMergeRepo) into <tmp>/scripts/ at mode 0755 and creates
// minimal clean fixture files for each surface the script reads, so the
// script runs without errors on missing paths.
func newInternalRefsTree(t *testing.T) *internalRefsTree {
	t.Helper()

	root := t.TempDir()

	src, err := filepath.Abs(filepath.Join("..", "..", "scripts", "check-no-internal-refs.sh"))
	if err != nil {
		t.Fatalf("resolving check script: %v", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading check script %s: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir for scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "check-no-internal-refs.sh"), data, 0o755); err != nil {
		t.Fatalf("copying script into temp tree: %v", err)
	}

	x := &internalRefsTree{t: t, root: root}
	x.writeFile("internal/server/web/locales/en.json", "{\n  \"greeting\": \"hello operator\"\n}\n")
	x.writeFile("internal/server/web/openapi.yaml", "openapi: 3.0.0\npaths:\n  /sessions:\n    get:\n      summary: List active sessions\n")
	x.writeFile("internal/server/web/app.js", "// operator PWA shell\nconst title = \"datawatch\"\n")
	x.writeFile("internal/server/api.go", "package server\n\n// Session notes are rendered from locale bundles.\n\nvar note = \"all good\"\n")
	// Second files on the greppable surfaces: with more than one file in a
	// glob the check script's recursive grep names the affected file in its
	// failure output. Both are clean.
	x.writeFile("internal/server/http.go", "package server\n\n// handler plumbing\n\nvar ok = true\n")
	x.writeFile("docs/howto/reviewing-sessions.md", "# Reviewing sessions\n\nOpen the review page from the tray icon.\n")
	x.writeFile("README.md", "# datawatch\n\nA session supervisor for agent fleets.\n")
	x.writeFile("docs/datawatch-definitions.md", "# Definitions\n\nTerms used across the operator UI.\n")
	x.writeFile("docs/howto/getting-started.md", "# Getting started\n\nRun the binary once to create the workspace.\n")
	return x
}

// writeFile writes content to path relative to the temp tree root, creating
// any missing parent directories.
func (x *internalRefsTree) writeFile(path, content string) {
	x.t.Helper()
	full := filepath.Join(x.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		x.t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		x.t.Fatalf("writing %s: %v", path, err)
	}
}

// run executes the copied check script under bash with TMUX and
// TMUX_TMPDIR removed from the environment, and returns its combined
// output and error.
func (x *internalRefsTree) run() (string, error) {
	x.t.Helper()
	cmd := exec.Command("bash", filepath.Join(x.root, "scripts", "check-no-internal-refs.sh"))
	cmd.Env = withoutTmuxEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// withoutTmuxEnv drops TMUX and TMUX_TMPDIR from an environment slice so the
// checked script runs in a multiplexer-free environment.
func withoutTmuxEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_TMPDIR=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// TestInternalRefsCheckPassesOnCleanTree covers a tree whose operator-facing
// surfaces contain no internal refs. The script must exit 0 and print its
// PASS line.
func TestInternalRefsCheckPassesOnCleanTree(t *testing.T) {
	x := newInternalRefsTree(t)

	out, err := x.run()
	if err != nil {
		t.Fatalf("expected clean tree to pass, got error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "✓ PASS: no internal refs in user-facing surfaces.") {
		t.Fatalf("expected the PASS line in output, got:\n%s", out)
	}
}

// TestInternalRefsCheckFailsOnLeak covers one internal ref leaked into each
// audited surface. The script must exit 1 and name the affected file.
func TestInternalRefsCheckFailsOnLeak(t *testing.T) {
	id := fakeTrackerID()

	cases := []struct {
		name      string
		leak      func(x *internalRefsTree)
		wantNamed string
	}{
		{
			name: "locale string",
			leak: func(x *internalRefsTree) {
				x.writeFile("internal/server/web/locales/en.json",
					"{\n  \"greeting\": \"hello "+id+"\"\n}\n")
			},
			wantNamed: "internal/server/web/locales/en.json",
		},
		{
			name: "README.md",
			leak: func(x *internalRefsTree) {
				x.writeFile("README.md",
					"# datawatch\n\nSee "+id+" for the original idea.\n")
			},
			wantNamed: "README.md",
		},
		{
			name: "howto prose",
			leak: func(x *internalRefsTree) {
				x.writeFile("docs/howto/getting-started.md",
					"# Getting started\n\nRun "+id+" before the first start.\n")
			},
			wantNamed: "docs/howto/getting-started.md",
		},
		{
			name: "openapi summary",
			leak: func(x *internalRefsTree) {
				x.writeFile("internal/server/web/openapi.yaml",
					"openapi: 3.0.0\npaths:\n  /sessions:\n    get:\n      summary: Lists "+id+" sessions\n")
			},
			wantNamed: "internal/server/web/openapi.yaml",
		},
		{
			name: "API note",
			leak: func(x *internalRefsTree) {
				x.writeFile("internal/server/api.go",
					"package server\n\nconst note = `{\"note\": \"see "+id+"\"}`\n")
			},
			wantNamed: "internal/server/api.go",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newInternalRefsTree(t)
			tc.leak(x)

			out, err := x.run()
			exit, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("expected the check to fail with exit code 1, got error: %v\n%s", err, out)
			}
			if code := exit.ExitCode(); code != 1 {
				t.Fatalf("expected exit code 1, got %d:\n%s", code, out)
			}
			if !strings.Contains(out, tc.wantNamed) {
				t.Fatalf("expected output to name %s, got:\n%s", tc.wantNamed, out)
			}
		})
	}
}

// TestInternalRefsCheckIgnoresExemptLocations covers an internal ref placed
// only where the rule allows it: a Go code comment and a plan document under
// docs/plans/. Both must pass.
func TestInternalRefsCheckIgnoresExemptLocations(t *testing.T) {
	id := fakeTrackerID()

	cases := []struct {
		name string
		leak func(x *internalRefsTree)
	}{
		{
			name: "go comment",
			leak: func(x *internalRefsTree) {
				x.writeFile("internal/server/api.go",
					"package server\n\n// history: this work was tracked under "+id+" during planning.\n\nvar note = \"all good\"\n")
			},
		},
		{
			name: "plan document",
			leak: func(x *internalRefsTree) {
				x.writeFile("docs/plans/2026-00-00-example.md",
					"# Example plan\n\nTracks "+id+" end to end.\n")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newInternalRefsTree(t)
			tc.leak(x)

			out, err := x.run()
			if err != nil {
				t.Fatalf("expected exempt location to pass, got error: %v\n%s", err, out)
			}
			if !strings.Contains(out, "✓ PASS: no internal refs in user-facing surfaces.") {
				t.Fatalf("expected the PASS line in output, got:\n%s", out)
			}
		})
	}
}
