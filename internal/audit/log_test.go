// BL9 — audit log tests.

package audit

import (
	"testing"
	"time"
)

func TestBL9_NewLog_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer l.Close()
}

func TestBL9_WriteRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	defer l.Close()
	if err := l.Write(Entry{Actor: "operator", Action: "start", SessionID: "aa"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Write(Entry{Actor: "channel:signal", Action: "send_input", SessionID: "aa"}); err != nil {
		t.Fatal(err)
	}
	out, err := l.Read(QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 entries, got %d", len(out))
	}
	// Newest first.
	if out[0].Action != "send_input" {
		t.Errorf("expected send_input first (newest), got %+v", out[0])
	}
}

func TestBL9_Read_ActorFilter(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	defer l.Close()
	_ = l.Write(Entry{Actor: "operator", Action: "start"})
	_ = l.Write(Entry{Actor: "channel:signal", Action: "send_input"})
	out, _ := l.Read(QueryFilter{Actor: "operator"})
	if len(out) != 1 {
		t.Fatalf("actor filter: want 1, got %d", len(out))
	}
}

func TestBL9_Read_SessionFilter(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	defer l.Close()
	_ = l.Write(Entry{Actor: "operator", Action: "start", SessionID: "aa"})
	_ = l.Write(Entry{Actor: "operator", Action: "start", SessionID: "bb"})
	out, _ := l.Read(QueryFilter{SessionID: "bb"})
	if len(out) != 1 || out[0].SessionID != "bb" {
		t.Fatalf("session filter wrong: %+v", out)
	}
}

func TestBL9_Read_SinceUntilWindow(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	defer l.Close()
	now := time.Now()
	_ = l.Write(Entry{Timestamp: now.Add(-2 * time.Hour), Action: "old"})
	_ = l.Write(Entry{Timestamp: now.Add(-30 * time.Minute), Action: "recent"})
	_ = l.Write(Entry{Timestamp: now, Action: "now"})
	out, _ := l.Read(QueryFilter{Since: now.Add(-1 * time.Hour)})
	if len(out) != 2 {
		t.Errorf("since filter: want 2 (recent + now), got %d", len(out))
	}
	out, _ = l.Read(QueryFilter{Until: now.Add(-1 * time.Hour)})
	if len(out) != 1 {
		t.Errorf("until filter: want 1 (old), got %d", len(out))
	}
}

func TestBL9_Read_LimitNewestFirst(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	defer l.Close()
	for i := 0; i < 5; i++ {
		_ = l.Write(Entry{Action: "x", Details: map[string]any{"i": i}})
	}
	out, _ := l.Read(QueryFilter{Limit: 2})
	if len(out) != 2 {
		t.Fatalf("limit: want 2, got %d", len(out))
	}
	// Most recent (i=4) should be first.
	if v, _ := out[0].Details["i"].(float64); v != 4 {
		t.Errorf("expected i=4 first, got %v", out[0].Details["i"])
	}
}

func TestBL9_Read_NoFileEmpty(t *testing.T) {
	dir := t.TempDir()
	l, _ := New(dir)
	_ = l.Close()
	out, err := l.Read(QueryFilter{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("empty log should return empty slice, got %d entries", len(out))
	}
}
