package classifier

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProberSnapshot(t *testing.T) {
	var hits int32
	srv := probeServer(t, true, &hits)
	now := time.Unix(1000, 0)
	p := NewProber(srv.Client(), func(string) (string, error) { return srv.URL, nil }, Config{ProbeTTL: time.Minute}, func() time.Time { return now })
	if got := p.Snapshot(); len(got) != 0 {
		t.Fatalf("empty prober snapshot = %v", got)
	}
	for _, node := range []string{"b-node", "a-node"} {
		if st, err := p.Probe(context.Background(), node, "m", PathNative); err != nil || st != StateSupported {
			t.Fatalf("probe %s: %v %v", node, st, err)
		}
	}
	snap := p.Snapshot()
	if len(snap) != 2 || snap[0].Node != "a-node" || snap[1].Node != "b-node" {
		t.Fatalf("snapshot not sorted by node: %+v", snap)
	}
	if snap[0].State != StateSupported || snap[0].Path != PathNative || !snap[0].ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("snapshot entry = %+v", snap[0])
	}
	now = now.Add(2 * time.Minute)
	if got := p.Snapshot(); len(got) != 0 {
		t.Fatalf("expired entries must not be listed: %v", got)
	}
}

func TestSnapshotOrdersModelThenPath(t *testing.T) {
	p := NewProber(nil, nil, Config{}, func() time.Time { return time.Unix(0, 0) })
	exp := time.Unix(100, 0)
	p.cache[probeKey{"n", "m2", PathNative}] = probeEntry{StateSupported, exp}
	p.cache[probeKey{"n", "m1", PathNative}] = probeEntry{StateSupported, exp}
	p.cache[probeKey{"n", "m1", PathLogprobs}] = probeEntry{StateUnsupported, exp}
	got := p.Snapshot()
	if got[0].Model != "m1" || got[0].Path != PathLogprobs || got[1].Path != PathNative || got[2].Model != "m2" {
		t.Fatalf("order = %+v", got)
	}
}

func TestFileStoreList(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cal")
	s := FileStore{Dir: dir}
	if recs, err := s.List(); err != nil || recs != nil {
		t.Fatalf("missing dir: %v %v", recs, err)
	}
	for _, m := range []string{"zeta", "alpha"} {
		fp := NewFingerprint(m, "q4", "n1", "p", "t")
		if err := s.Save(&Record{Fingerprint: fp, FingerprintHash: fp.Hash(), Threshold: 0.7}); err != nil {
			t.Fatal(err)
		}
	}
	fpa := NewFingerprint("alpha", "q4", "n0", "p", "t")
	if err := s.Save(&Record{Fingerprint: fpa, Threshold: 0.6}); err != nil {
		t.Fatal(err)
	}
	fpb := NewFingerprint("alpha", "q2", "n0", "p", "t")
	if err := s.Save(&Record{Fingerprint: fpb, Threshold: 0.6}); err != nil {
		t.Fatal(err)
	}
	// Files that are not records are ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "calibration-dir.json"), 0o750); err != nil {
		t.Fatal(err)
	}
	recs, err := s.List()
	if err != nil || len(recs) != 4 {
		t.Fatalf("list = %d %v", len(recs), err)
	}
	if recs[0].Fingerprint.Node != "n0" || recs[0].Fingerprint.Quantization != "q2" || recs[1].Fingerprint.Quantization != "q4" || recs[3].Fingerprint.Model != "zeta" {
		t.Fatalf("order = %+v", recs)
	}
}

func TestFileStoreListCorruptRecordFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "calibration-bad.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileStore{Dir: dir}).List(); err == nil {
		t.Fatal("a corrupt record must be an error, not a silent skip")
	}
	fp := NewFingerprint("m", "q", "n", "p", "t")
	if _, err := (FileStore{Dir: dir}).Load(fp); err != nil {
		t.Fatalf("load of an absent slot stays nil,nil: %v", err)
	}
}

func TestFileStoreListUnreadableDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (FileStore{Dir: f}).List(); err == nil {
		t.Fatal("a non-directory store dir must be an error")
	}
}

func TestPublisherAllowlist(t *testing.T) {
	allow := []string{"Qwen", " meta-llama "}
	for pub, want := range map[string]bool{"qwen": true, "QWEN": true, "meta-llama": true, "someone-else": false, "": false, "  ": false} {
		if PublisherAllowed(allow, pub) != want {
			t.Errorf("PublisherAllowed(%q) = %v, want %v", pub, !want, want)
		}
	}
	if PublisherAllowed(nil, "qwen") {
		t.Error("an empty allowlist trusts nothing")
	}
	next, changed := AddPublisher(allow, " Google ")
	if !changed || len(next) != 3 || next[2] != "google" || len(allow) != 2 {
		t.Fatalf("add = %v %v (input must not be mutated: %v)", next, changed, allow)
	}
	if again, changed := AddPublisher(next, "GOOGLE"); changed || len(again) != 3 {
		t.Fatalf("duplicate add changed the list: %v", again)
	}
	if same, changed := AddPublisher(allow, "  "); changed || len(same) != 2 {
		t.Fatalf("empty add changed the list: %v", same)
	}
}
