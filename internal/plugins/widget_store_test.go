package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWidgetStore_SetGet(t *testing.T) {
	s, err := NewWidgetStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(WidgetValue{Plugin: "p", WidgetID: "w", Data: json.RawMessage(`{"n":1}`), Source: WidgetSourcePush}); err != nil {
		t.Fatal(err)
	}
	v, ok := s.Get("p", "w")
	if !ok {
		t.Fatal("value not found")
	}
	if string(v.Data) != `{"n":1}` || v.Source != WidgetSourcePush || v.UpdatedAt.IsZero() {
		t.Fatalf("unexpected value: %+v", v)
	}
	if _, ok := s.Get("p", "other"); ok {
		t.Fatal("unexpected value for unknown widget")
	}
}

func TestWidgetStore_SetErrorKeepsData(t *testing.T) {
	s, _ := NewWidgetStore("")
	if err := s.Set(WidgetValue{Plugin: "p", WidgetID: "w", Data: json.RawMessage(`[1,2]`), Source: WidgetSourcePull}); err != nil {
		t.Fatal(err)
	}
	s.SetError("p", "w", "pull timed out")
	v, _ := s.Get("p", "w")
	if string(v.Data) != `[1,2]` || v.Error != "pull timed out" {
		t.Fatalf("unexpected value: %+v", v)
	}
}

func TestWidgetStore_RejectsOversizedPayload(t *testing.T) {
	s, _ := NewWidgetStore("")
	big := json.RawMessage(`"` + strings.Repeat("x", MaxWidgetPayloadBytes) + `"`)
	if err := s.Set(WidgetValue{Plugin: "p", WidgetID: "w", Data: big}); err == nil {
		t.Fatal("expected oversized payload to be rejected")
	}
	if _, ok := s.Get("p", "w"); ok {
		t.Fatal("oversized payload was stored")
	}
}

func TestWidgetStore_ListAndDeletePlugin(t *testing.T) {
	s, _ := NewWidgetStore("")
	for _, k := range [][2]string{{"b", "z"}, {"a", "y"}, {"b", "a"}} {
		if err := s.Set(WidgetValue{Plugin: k[0], WidgetID: k[1], Data: json.RawMessage(`1`)}); err != nil {
			t.Fatal(err)
		}
	}
	all := s.List("")
	if len(all) != 3 || all[0].Plugin != "a" || all[1].WidgetID != "a" || all[2].WidgetID != "z" {
		t.Fatalf("unexpected order: %+v", all)
	}
	if got := s.List("b"); len(got) != 2 {
		t.Fatalf("List(b) = %d values, want 2", len(got))
	}
	s.DeletePlugin("b")
	if got := s.List(""); len(got) != 1 || got[0].Plugin != "a" {
		t.Fatalf("after delete: %+v", got)
	}
}

func TestWidgetStore_SnapshotSkippedWhenClean(t *testing.T) {
	dir := t.TempDir()
	s, err := NewWidgetStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plugins", "widgets.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("snapshot written for a clean store: %v", err)
	}

	if err := s.Set(WidgetValue{Plugin: "p", WidgetID: "w", Data: json.RawMessage(`1`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %o, want 600", st.Mode().Perm())
	}
	// Remove the file: a second Snapshot with no changes must not rewrite it.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("snapshot rewritten without changes: %v", err)
	}
}

func TestWidgetStore_SurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := NewWidgetStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(WidgetValue{Plugin: "p", WidgetID: "w", Data: json.RawMessage(`{"ok":true}`), Source: WidgetSourcePush}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, 20*time.Millisecond); close(done) }()
	path := filepath.Join(dir, "plugins", "widgets.json")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("snapshot never written")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	s2, err := NewWidgetStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := s2.Get("p", "w")
	if !ok {
		t.Fatal("value lost across restart")
	}
	if string(v.Data) != `{"ok":true}` || v.Source != WidgetSourcePush {
		t.Fatalf("unexpected value after restart: %+v", v)
	}
}

func TestWidgetStore_CorruptSnapshotErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins", "widgets.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWidgetStore(dir); err == nil {
		t.Fatal("expected an error for a corrupt snapshot")
	}
}

func TestWidgetStore_EmptySnapshotIsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins", "widgets.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := NewWidgetStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.List(""); len(got) != 0 {
		t.Fatalf("expected empty store, got %+v", got)
	}
}
