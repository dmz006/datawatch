package delivery

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/secfile"
)

var testKeyParams = KeyParams{Time: 1, MemoryKiB: 8 * 1024, Threads: 1}

func keyPath(dir string) string { return filepath.Join(dir, filepath.FromSlash(KeyFile)) }

func openKey(t *testing.T, dir string, pw []byte) (*StoreKey, error) {
	t.Helper()
	return OpenStoreKey(dir, pw, testKeyParams)
}

func TestStoreKeyIsWrittenOnlyOnCommitAndReadBack(t *testing.T) {
	dir := t.TempDir()
	k, err := openKey(t, dir, []byte("pw"))
	if err != nil || len(k.Key()) != 32 {
		t.Fatalf("first open: %v key=%d", err, len(k.Key()))
	}
	// A start that fails later has written nothing.
	if _, err := os.Stat(keyPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("written before commit: %v", err)
	}
	if !k.IsNew() {
		t.Fatal("a new key says so")
	}
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(keyPath(dir))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", st, err)
	}
	again, err := openKey(t, dir, []byte("pw"))
	if err != nil || !bytes.Equal(again.Key(), k.Key()) || again.IsNew() {
		t.Fatalf("second open: %v", err)
	}
	before, _ := os.ReadFile(keyPath(dir))
	if err := again.Commit(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(keyPath(dir))
	if !bytes.Equal(before, after) {
		t.Fatal("the key file is written once")
	}
	if strings.Contains(string(after), "pw\"") {
		t.Fatal("password in the key file")
	}
}

func TestAWrongPasswordStopsTheStoreAndChangesNoByte(t *testing.T) {
	dir := t.TempDir()
	k, _ := openKey(t, dir, []byte("pw"))
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(keyPath(dir))
	_, err := openKey(t, dir, []byte("other"))
	if !errors.Is(err, ErrStoreKey) || !strings.Contains(err.Error(), "password") {
		t.Fatalf("wrong password: %v", err)
	}
	after, _ := os.ReadFile(keyPath(dir))
	if !bytes.Equal(before, after) {
		t.Fatal("a refused open changed the key file")
	}
}

func TestASealedStoreWithoutAPasswordRefuses(t *testing.T) {
	dir := t.TempDir()
	k, _ := openKey(t, dir, []byte("pw"))
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := openKey(t, dir, nil); !errors.Is(err, ErrStoreKey) {
		t.Fatalf("non-secure over a sealed store: %v", err)
	}
}

func TestAMissingKeyWithRecordsIsNeverRecreated(t *testing.T) {
	dir := t.TempDir()
	k, _ := openKey(t, dir, []byte("pw"))
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(dir, HeartbeatDir, stateFileName)
	if err := os.MkdirAll(filepath.Dir(rec), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := secfile.WriteFile(rec, []byte("{}"), 0o600, k.Key()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath(dir)); err != nil {
		t.Fatal(err)
	}
	for _, pw := range [][]byte{[]byte("pw"), nil} {
		if _, err := openKey(t, dir, pw); !errors.Is(err, ErrStoreKey) {
			t.Fatalf("password %q: %v", pw, err)
		}
	}
	if _, err := os.Stat(keyPath(dir)); !os.IsNotExist(err) {
		t.Fatal("the key was recreated")
	}
}

func TestADamagedKeyFileRefuses(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(keyPath(dir)), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"", "{", `{"version":1,"kdf":"argon2id","time":1,"memory_kib":4294967295,"threads":1,"salt":"AAAA","test":"x"}`} {
		if err := os.WriteFile(keyPath(dir), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := openKey(t, dir, []byte("pw")); !errors.Is(err, ErrStoreKey) {
			t.Fatalf("%q: %v", body, err)
		}
	}
}

func TestNonSecureNeedsNoKey(t *testing.T) {
	dir := t.TempDir()
	k, err := openKey(t, dir, nil)
	if err != nil || k.Key() != nil {
		t.Fatalf("%v %v", err, k.Key())
	}
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keyPath(dir)); !os.IsNotExist(err) {
		t.Fatal("a non-secure install has no key file")
	}
}

// A settings change gives the config a new salt. The delivery records do not
// use it.
func TestAConfigSaltChangeLeavesTheRecordsReadable(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	pw := []byte("pw")
	if err := config.SaveSecure(config.DefaultConfig(), cfgPath, append([]byte(nil), pw...)); err != nil {
		t.Fatal(err)
	}
	salt := func() []byte {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		s, err := config.ExtractSalt(data)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first := salt()
	k, _ := openKey(t, dir, []byte("pw"))
	if err := k.Commit(); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.dir, h.key = filepath.Join(dir, HeartbeatDir), k.Key()
	h.open()
	h.cands = []Candidate{failedWork("s1", h.now)}
	h.pass()

	if err := config.SaveSecure(config.DefaultConfig(), cfgPath, append([]byte(nil), pw...)); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, salt()) {
		t.Fatal("the config salt did not change; the test proves nothing")
	}
	again, err := openKey(t, dir, []byte("pw"))
	if err != nil {
		t.Fatal(err)
	}
	h.key = again.Key()
	h.open()
	r := h.next()
	if _, ok := findEntry(r.Backlog, "s1"); !ok || hasRed(r, RedRecords) {
		t.Fatalf("red=%v backlog=%v", r.Red, r.Backlog)
	}
}

func TestPlainRecordsAreSealedWhenSecureModeIsFirstTurnedOn(t *testing.T) {
	h := newHarness(t)
	h.cands = []Candidate{failedWork("markerid42", h.now)}
	h.pass()
	h.key = []byte("0123456789abcdef0123456789abcdef")
	opts := h.options()
	opts.AdoptPlain = true
	e, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	h.e = e
	r := h.next()
	if _, ok := findEntry(r.Backlog, "markerid42"); !ok {
		t.Fatalf("red=%v", r.Red)
	}
	for _, name := range []string{stateFileName, startMarkFile} {
		data, _ := os.ReadFile(filepath.Join(h.dir, name))
		if strings.Contains(string(data), "markerid42") {
			t.Fatalf("%s is still plain", name)
		}
	}
	// Later, a plain file in a sealed store is a foreign write.
	if err := os.WriteFile(filepath.Join(h.dir, stateFileName), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.open()
	if got := h.e.Report(); !hasRed(got, RedRecords) {
		t.Fatalf("plain file accepted: %v", got.Red)
	}
}
