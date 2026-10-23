package gitguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func homeOf(h string) func() (string, error) { return func() (string, error) { return h, nil } }

func resetRefused() {
	refusedMu.Lock()
	refusedDirs = map[string]string{}
	refusedMu.Unlock()
}

func TestNonProjectReason_NonProjectHomeAndRoot(t *testing.T) {
	home := t.TempDir()
	if r := NonProjectReason(home, homeOf(home)); !strings.Contains(r, "home") {
		t.Fatalf("home: got %q", r)
	}
	if r := NonProjectReason("/", homeOf(home)); !strings.Contains(r, "root") {
		t.Fatalf("root: got %q", r)
	}
}

func TestNonProjectReason_NonProjectHomeSymlink(t *testing.T) {
	home := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(home, link); err != nil {
		t.Skip(err)
	}
	if r := NonProjectReason(link, homeOf(home)); !strings.Contains(r, "home") {
		t.Fatalf("symlink to home: got %q", r)
	}
}

func TestNonProjectReason_NonProjectNoMarker(t *testing.T) {
	home := t.TempDir()
	sub := filepath.Join(home, "scratch")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	if r := NonProjectReason(sub, homeOf(home)); !strings.Contains(r, "no git repository") {
		t.Fatalf("got %q", r)
	}
}

func TestNonProjectReason_NonProjectMarkersAllowed(t *testing.T) {
	home := t.TempDir()
	for _, m := range projectMarkers {
		sub := filepath.Join(home, "p-"+strings.TrimPrefix(m, "."))
		if err := os.Mkdir(sub, 0o750); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(sub, m)
		if m == ".git" || m == ".datawatch" {
			err := os.Mkdir(p, 0o750)
			if err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if r := NonProjectReason(sub, homeOf(home)); r != "" {
			t.Fatalf("marker %s: refused %q", m, r)
		}
	}
}

func TestNonProjectRefuse_NonProjectOnceAndTyped(t *testing.T) {
	resetRefused()
	home := t.TempDir()
	sub := filepath.Join(home, "x")
	_ = os.Mkdir(sub, 0o750)

	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	e1 := refuseNonProject(sub, homeOf(home))
	e2 := refuseNonProject(sub, homeOf(home))
	os.Stdout = old
	_ = w.Close()
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if !IsNonProject(e1) || !IsNonProject(fmt.Errorf("wrap: %w", e2)) {
		t.Fatalf("not typed: %v %v", e1, e2)
	}
	var a, b *NonProjectError
	if !errors.As(e1, &a) || !errors.As(e2, &b) || a.Reason != b.Reason || a.Dir != sub {
		t.Fatalf("mismatch %v %v", e1, e2)
	}
	if c := strings.Count(out, "[audit]"); c != 1 {
		t.Fatalf("audit lines = %d, out=%q", c, out)
	}
	if !strings.Contains(out, sub) || !strings.Contains(out, "no git repository") {
		t.Fatalf("audit line lacks dir/reason: %q", out)
	}
}

func TestNonProjectRefuse_NonProjectAllowsRepo(t *testing.T) {
	resetRefused()
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o750)
	if err := refuseNonProject(repo, homeOf(home)); err != nil {
		t.Fatal(err)
	}
}
