package mergepipeline

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordStore_CRUDAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := NewRecordStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Get missing = %v", err)
	}
	if err := s.Delete("a"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("Delete missing = %v", err)
	}
	s.now = func() time.Time { return time.Unix(100, 0) }
	def := DefaultDefinition()
	if _, err := s.Set("b", def); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set("a", def); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("a")
	if err != nil || got.Project != "a" || len(got.Definition.Steps) != len(def.Steps) || !got.UpdatedAt.Equal(time.Unix(100, 0)) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	got.Definition.Steps[0].Name = "mutated"
	again, _ := s.Get("a")
	if again.Definition.Steps[0].Name == "mutated" {
		t.Fatal("Get returned an alias of stored steps")
	}
	if l := s.List(); len(l) != 2 || l[0].Project != "a" || l[1].Project != "b" {
		t.Fatalf("List = %+v", l)
	}
	s2, err := NewRecordStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Get("b"); err != nil {
		t.Fatalf("reload lost record: %v", err)
	}
	if err := s2.Delete("b"); err != nil {
		t.Fatal(err)
	}
	s3, _ := NewRecordStore(dir)
	if _, err := s3.Get("b"); !errors.Is(err, ErrRecordNotFound) {
		t.Fatal("delete not persisted")
	}
}

func TestRecordStore_SetRejects(t *testing.T) {
	s, _ := NewRecordStore(t.TempDir())
	if _, err := s.Set(" ", DefaultDefinition()); err == nil {
		t.Fatal("blank project accepted")
	}
	if _, err := s.Set("p", Definition{}); err == nil {
		t.Fatal("invalid definition accepted")
	}
	if len(s.List()) != 0 {
		t.Fatal("rejected set stored something")
	}
}

func TestRecordStore_CorruptFileAndWriteFailure(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "merge-pipeline")
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "records.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRecordStore(dir); err == nil {
		t.Fatal("corrupt store accepted")
	}

	// A data directory that is a file makes persist fail; state rolls back.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &RecordStore{path: filepath.Join(blocker, "x", "records.json"), records: map[string]Record{}, now: time.Now}
	if _, err := s.Set("p", DefaultDefinition()); err == nil {
		t.Fatal("persist failure not reported")
	}
	if len(s.records) != 0 {
		t.Fatal("failed Set was not rolled back")
	}
	s.records["p"] = Record{Project: "p"}
	if err := s.Delete("p"); err == nil || len(s.records) != 1 {
		t.Fatal("failed Delete not rolled back")
	}
}
