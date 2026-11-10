package packs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const trustManifest = `
name: trust-pack
version: 1.0.0
description: A pack for trust tests
tasks:
  - title: Check the tree
    verification_script: verify/check.sh
`

// trustPack loads a passing pack fixture so the test has a real digest.
func trustPack(t *testing.T, source string) *Pack {
	t.Helper()
	dir := writePackDir(t, trustManifest, map[string]string{"verify/check.sh": shellScript})
	p, err := LoadPackFolder(dir)
	if err != nil {
		t.Fatalf("LoadPackFolder: %v", err)
	}
	p.Source = source
	return p
}

var acceptedAt = time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC)

func TestRunnableRefusesUntrustedPackFromNonFirstPartySource(t *testing.T) {
	store, err := LoadTrustStore(t.TempDir())
	if err != nil {
		t.Fatalf("LoadTrustStore: %v", err)
	}
	p := trustPack(t, "someone-else")
	err = store.Runnable(p)
	if err == nil {
		t.Fatal("Runnable: want refusal for untrusted pack, got nil")
	}
	if err.Error() != "pack not accepted: accept it first" {
		t.Errorf("Runnable error = %q, want %q", err.Error(), "pack not accepted: accept it first")
	}
}

func TestRunnableRefusesPackWithEmptySource(t *testing.T) {
	store, _ := LoadTrustStore(t.TempDir())
	p := trustPack(t, "")
	if err := store.Runnable(p); err == nil {
		t.Error("Runnable: an empty source is not first party; want refusal")
	}
}

func TestRunnableAllowsFirstPartySourceWithoutRecord(t *testing.T) {
	store, _ := LoadTrustStore(t.TempDir())
	p := trustPack(t, "community")
	if err := store.Runnable(p); err != nil {
		t.Errorf("Runnable first-party: %v, want nil", err)
	}
}

func TestAcceptThenRunnableAllows(t *testing.T) {
	root := t.TempDir()
	store, _ := LoadTrustStore(root)
	p := trustPack(t, "someone-else")

	rec, err := store.Accept(p, "  operator@example  ", acceptedAt)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if rec.Name != "trust-pack" || rec.Version != "1.0.0" || rec.Digest != p.Digest {
		t.Errorf("record identity = %+v", rec)
	}
	if rec.AcceptedBy != "operator@example" {
		t.Errorf("AcceptedBy = %q, want trimmed operator@example", rec.AcceptedBy)
	}
	if !rec.AcceptedAt.Equal(acceptedAt) {
		t.Errorf("AcceptedAt = %v, want %v", rec.AcceptedAt, acceptedAt)
	}
	if err := store.Runnable(p); err != nil {
		t.Errorf("Runnable after accept: %v, want nil", err)
	}

	// A fresh load from disk still trusts the pack.
	reloaded, err := LoadTrustStore(root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := reloaded.Runnable(p); err != nil {
		t.Errorf("Runnable after reload: %v, want nil", err)
	}
}

func TestAcceptWritesJSONFileInInstallRoot(t *testing.T) {
	root := t.TempDir()
	store, _ := LoadTrustStore(root)
	p := trustPack(t, "someone-else")
	if _, err := store.Accept(p, "operator", acceptedAt); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "trust.json"))
	if err != nil {
		t.Fatalf("trust.json not written: %v", err)
	}
	var f struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("trust.json is not JSON: %v", err)
	}
	if len(f.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(f.Records))
	}
	r := f.Records[0]
	for _, key := range []string{"name", "version", "digest", "accepted_by", "accepted_at"} {
		if _, ok := r[key]; !ok {
			t.Errorf("record missing key %q: %v", key, r)
		}
	}
	if r["accepted_by"] != "operator" {
		t.Errorf("accepted_by = %v, want operator", r["accepted_by"])
	}
	// No temporary file is left behind after the atomic write.
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".trust-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
}

func TestAcceptReplacesEarlierRecordForSameVersion(t *testing.T) {
	root := t.TempDir()
	store, _ := LoadTrustStore(root)
	p := trustPack(t, "someone-else")
	if _, err := store.Accept(p, "first", acceptedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(p, "second", acceptedAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rec, ok := store.Record("trust-pack", "1.0.0")
	if !ok || rec.AcceptedBy != "second" {
		t.Errorf("record = %+v ok=%v, want accepted_by second", rec, ok)
	}
	reloaded, _ := LoadTrustStore(root)
	if n := len(reloaded.records); n != 1 {
		t.Errorf("records on disk = %d, want 1", n)
	}
}

func TestRunnableRefusesWhenDigestChanged(t *testing.T) {
	root := t.TempDir()
	store, _ := LoadTrustStore(root)
	p := trustPack(t, "someone-else")
	if _, err := store.Accept(p, "operator", acceptedAt); err != nil {
		t.Fatal(err)
	}
	p.Digest = "changed-digest"
	if err := store.Runnable(p); err == nil {
		t.Error("Runnable with a changed digest: want refusal, got nil")
	}
}

func TestRunnableRefusesUnacceptedVersion(t *testing.T) {
	root := t.TempDir()
	store, _ := LoadTrustStore(root)
	p := trustPack(t, "someone-else")
	if _, err := store.Accept(p, "operator", acceptedAt); err != nil {
		t.Fatal(err)
	}
	p.Version = "2.0.0"
	if err := store.Runnable(p); err == nil {
		t.Error("Runnable for an unaccepted version: want refusal, got nil")
	}
}

func TestRunnableRefusesDigestlessPack(t *testing.T) {
	store, _ := LoadTrustStore(t.TempDir())
	p := trustPack(t, "someone-else")
	p.Digest = ""
	if err := store.Runnable(p); err == nil {
		t.Error("Runnable with no digest: want refusal, got nil")
	}
}

func TestRunnableNilPack(t *testing.T) {
	store, _ := LoadTrustStore(t.TempDir())
	if err := store.Runnable(nil); err == nil {
		t.Error("Runnable(nil): want error")
	}
}

func TestAcceptRejectsBadInput(t *testing.T) {
	store, _ := LoadTrustStore(t.TempDir())
	p := trustPack(t, "someone-else")
	if _, err := store.Accept(nil, "operator", acceptedAt); err == nil {
		t.Error("Accept(nil): want error")
	}
	if _, err := store.Accept(p, "   ", acceptedAt); err == nil {
		t.Error("Accept with blank accepted_by: want error")
	}
	noDigest := trustPack(t, "someone-else")
	noDigest.Digest = ""
	if _, err := store.Accept(noDigest, "operator", acceptedAt); err == nil {
		t.Error("Accept with no digest: want error")
	}
}

func TestLoadTrustStoreMissingFileIsEmpty(t *testing.T) {
	store, err := LoadTrustStore(filepath.Join(t.TempDir(), "not-created"))
	if err != nil {
		t.Fatalf("LoadTrustStore missing root: %v", err)
	}
	if _, ok := store.Record("x", "1.0.0"); ok {
		t.Error("empty store reported a record")
	}
}

func TestLoadTrustStoreCorruptFileIsError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "trust.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustStore(root); err == nil {
		t.Error("corrupt trust.json: want error, got nil")
	}
}

func TestLoadTrustStoreUnreadableFileIsError(t *testing.T) {
	root := t.TempDir()
	// A directory where the file should be cannot be read as a file.
	if err := os.Mkdir(filepath.Join(root, "trust.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustStore(root); err == nil {
		t.Error("trust.json is a directory: want error, got nil")
	}
}

func TestAcceptFailsWhenRootIsAFile(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The store path sits under a file, so reading it fails before any accept.
	if _, err := LoadTrustStore(blocker); err == nil {
		t.Error("LoadTrustStore under a file root: want error, got nil")
	}
}

func TestAcceptFailsWhenStoreFolderCannotBeCreated(t *testing.T) {
	store, _ := LoadTrustStore(t.TempDir())
	// Point the store at a path whose parent is a regular file.
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(blocker, "trust.json")
	p := trustPack(t, "someone-else")
	if _, err := store.Accept(p, "operator", acceptedAt); err == nil {
		t.Error("Accept with an uncreatable folder: want error, got nil")
	}
	if _, ok := store.Record("trust-pack", "1.0.0"); ok {
		t.Error("a failed Accept must not record the pack in memory")
	}
}

func TestWriteTrustFileSortsRecords(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "trust.json")
	recs := []TrustRecord{
		{Name: "zeta", Version: "1.0.0", Digest: "d", AcceptedBy: "a", AcceptedAt: acceptedAt},
		{Name: "alpha", Version: "2.0.0", Digest: "d", AcceptedBy: "a", AcceptedAt: acceptedAt},
		{Name: "alpha", Version: "1.0.0", Digest: "d", AcceptedBy: "a", AcceptedAt: acceptedAt},
	}
	if err := writeTrustFile(path, recs); err != nil {
		t.Fatalf("writeTrustFile: %v", err)
	}
	s, err := LoadTrustStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.records[0].Name != "alpha" || s.records[0].Version != "1.0.0" || s.records[2].Name != "zeta" {
		t.Errorf("records not sorted: %+v", s.records)
	}
}

func TestWriteTrustFileFailsWhenPathIsDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "trust.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTrustFile(path, nil); err == nil {
		t.Error("rename over a directory: want error, got nil")
	}
}
