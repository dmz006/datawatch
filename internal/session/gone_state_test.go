package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGoneFinalStateCompleteWhenMarkerEnds(t *testing.T) {
	tail := "working...\n\x1b[32mDATAWATCH_COMPLETE: all done\x1b[0m\n\n"
	st, note := GoneFinalState(tail)
	if st != StateComplete {
		t.Fatalf("state = %q, want %q", st, StateComplete)
	}
	if note != GoneEndNote {
		t.Fatalf("note = %q, want %q", note, GoneEndNote)
	}
}

func TestGoneFinalStateKilledWithoutMarker(t *testing.T) {
	st, note := GoneFinalState("compiling\nrunning tests\n")
	if st != StateKilled {
		t.Fatalf("state = %q, want %q", st, StateKilled)
	}
	if note != GoneEndNote {
		t.Fatalf("note = %q, want %q", note, GoneEndNote)
	}
}

func TestGoneFinalStateMarkerNotAtEndIsKilled(t *testing.T) {
	tail := "DATAWATCH_COMPLETE: done\nmore output after the marker\n"
	st, note := GoneFinalState(tail)
	if st != StateKilled {
		t.Fatalf("state = %q, want %q", st, StateKilled)
	}
	if note != GoneEndNote {
		t.Fatalf("note = %q, want %q", note, GoneEndNote)
	}
}

func TestGoneFinalStateEmptyOutputIsKilled(t *testing.T) {
	for _, tail := range []string{"", "\n\n  \n", "\x1b[0m\n"} {
		st, note := GoneFinalState(tail)
		if st != StateKilled {
			t.Fatalf("tail %q: state = %q, want %q", tail, st, StateKilled)
		}
		if note != GoneEndNote {
			t.Fatalf("tail %q: note = %q, want %q", tail, note, GoneEndNote)
		}
	}
}

func TestGoneEndNoteText(t *testing.T) {
	if GoneEndNote != "process gone, finalised" {
		t.Fatalf("GoneEndNote = %q", GoneEndNote)
	}
}

func TestReadOutputTailReturnsOnlyTheTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.log")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadOutputTail(path, 4); got != "6789" {
		t.Fatalf("tail(4) = %q, want %q", got, "6789")
	}
	if got := ReadOutputTail(path, 100); got != "0123456789" {
		t.Fatalf("tail(100) = %q, want whole file", got)
	}
	if got := ReadOutputTail(path, 0); got != "" {
		t.Fatalf("tail(0) = %q, want empty", got)
	}
	if got := ReadOutputTail(filepath.Join(dir, "missing.log"), 4); got != "" {
		t.Fatalf("missing file tail = %q, want empty", got)
	}
	if got := ReadOutputTail(dir, 4); got != "" {
		t.Fatalf("directory tail = %q, want empty", got)
	}
}
