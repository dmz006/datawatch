package supervisorloop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionTrimming(t *testing.T) {
	p := filepath.Join(t.TempDir(), "runs.jsonl")
	fs := NewFileStore(p, 3)
	for i := 0; i < 5; i++ {
		if err := fs.Append(RunRecord{Job: string(rune('a' + i)), Outcome: OutcomeOK, Attempts: i, Duration: time.Second}); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := fs.List()
	if err != nil || len(recs) != 3 || recs[0].Job != "c" || recs[2].Job != "e" || recs[2].Duration != time.Second {
		t.Fatal(recs, err)
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), "\n") != 3 {
		t.Fatalf("%q", b)
	}
}

func TestStoreMissingAndCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.jsonl")
	fs := NewFileStore(p, 10)
	if recs, err := fs.List(); err != nil || recs != nil {
		t.Fatal(recs, err)
	}
	_ = os.WriteFile(p, []byte("garbage\n"), 0o600)
	_ = fs.Append(RunRecord{Job: "ok"})
	recs, _ := fs.List()
	if len(recs) != 1 || recs[0].Job != "ok" {
		t.Fatal(recs)
	}
	if err := NewFileStore(filepath.Join(p, "no", "dir"), 1).Append(RunRecord{}); err == nil {
		t.Fatal("want error")
	}
}

func TestEngineWritesFileStore(t *testing.T) {
	fs := NewFileStore(filepath.Join(t.TempDir(), "r.jsonl"), 2)
	e, _, _ := newEngine(&fakeClient{replies: []string{"good", "good", "good"}})
	e.Store = fs
	for i := 0; i < 3; i++ {
		if _, err := e.Run(t.Context(), modelJob(StyleSession), "q"); err != nil {
			t.Fatal(err)
		}
	}
	recs, _ := fs.List()
	if len(recs) != 2 || recs[0].Style != StyleSession || recs[0].Started.IsZero() || recs[0].Finished.Before(recs[0].Started) {
		t.Fatalf("%+v", recs)
	}
}
