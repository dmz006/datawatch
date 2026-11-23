package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPiFailedLineEvidence(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("missing file", func(t *testing.T) {
		if _, ok := PiFailedLineEvidence(filepath.Join(dir, "none.log"), "n", "m"); ok {
			t.Fatal("want false")
		}
	})
	t.Run("no marker", func(t *testing.T) {
		if _, ok := PiFailedLineEvidence(write("a.log", "working\nmore\n"), "n", "m"); ok {
			t.Fatal("want false")
		}
	})
	t.Run("marker mid-line is ignored", func(t *testing.T) {
		if _, ok := PiFailedLineEvidence(write("b.log", "echo DATAWATCH_PI_FAILED: x\n"), "n", "m"); ok {
			t.Fatal("want false")
		}
	})
	t.Run("marker with status", func(t *testing.T) {
		ev, ok := PiFailedLineEvidence(write("c.log", "work\nDATAWATCH_PI_FAILED: pi exited with status 3\n"), "node1", "mod")
		if !ok {
			t.Fatal("want true")
		}
		if ev.ExitStatus == nil || *ev.ExitStatus != 3 || ev.Node != "node1" || ev.Model != "mod" {
			t.Fatalf("bad evidence: %+v", ev)
		}
	})
	t.Run("marker without status", func(t *testing.T) {
		ev, ok := PiFailedLineEvidence(write("d.log", "\x1b[31mDATAWATCH_PI_FAILED: pi produced no output\x1b[0m\r\n"), "n", "m")
		if !ok || ev.ExitStatus != nil || len(ev.LastOutput) != 1 {
			t.Fatalf("got %+v ok=%v", ev, ok)
		}
	})
}

func TestPiFailedLineEvidenceImmediate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(p, []byte("start\nDATAWATCH_PI_FAILED: pi exited with status 7 (model m)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	ev, ok := PiFailedLineEvidence(p, "n", "m")
	if !ok {
		t.Fatal("want true")
	}
	if ev.ExitStatus == nil || *ev.ExitStatus != 7 {
		t.Fatalf("want status 7, got %+v", ev.ExitStatus)
	}
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("detection took %v, want immediate", d)
	}
}

func TestPiFailedLineEvidenceNoLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "output.log")
	if err := os.WriteFile(p, []byte("still working\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := PiFailedLineEvidence(p, "n", "m"); ok {
		t.Fatal("want false")
	}
}
