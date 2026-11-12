// State-path tests for the file secret provider's default
// location: the secrets folder must live under the configured
// data directory, never under a (decoy) home directory.

package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatePathSecretsDirUnderDataDir pins the state path of the
// default file provider to the configured data directory. A decoy
// HOME is planted with a .datawatch/secrets folder containing a
// sentinel so any accidental home lookup is observable.
func TestStatePathSecretsDirUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	dataDir = filepath.Clean(dataDir)

	// Decoy home with a secrets folder + sentinel.
	decoyHome := t.TempDir()
	decoySecrets := filepath.Join(decoyHome, ".datawatch", "secrets")
	if err := os.MkdirAll(decoySecrets, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(decoySecrets, "sentinel")
	if err := os.WriteFile(sentinel, []byte("decoy"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", decoyHome)

	// DefaultFileProviderDir points under the data directory.
	dir := DefaultFileProviderDir(dataDir)
	want := filepath.Join(dataDir, "secrets")
	if dir != want {
		t.Fatalf("DefaultFileProviderDir(%q)=%q want %q", dataDir, dir, want)
	}
	if !strings.HasPrefix(dir, dataDir+string(filepath.Separator)) {
		t.Fatalf("dir %q is not under data dir %q", dir, dataDir)
	}

	// Provider built from it stays under the data directory.
	p := NewFileProvider(dir)
	if !strings.HasPrefix(filepath.Clean(p.BaseDir), dataDir+string(filepath.Separator)) {
		t.Fatalf("provider BaseDir %q is not under data dir %q", p.BaseDir, dataDir)
	}
	if err := p.Put("test-key", "test-value"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := p.Get("test-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "test-value" {
		t.Errorf("Get=%q want %q", got, "test-value")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "secrets", "test-key")); err != nil {
		t.Errorf("expected secret file under data dir: %v", err)
	}

	// Decoy home untouched: its secrets folder still holds only
	// the sentinel.
	entries, err := os.ReadDir(decoySecrets)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "sentinel" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("decoy secrets folder was touched: entries=%v want [sentinel]", names)
	}

	// "~" expansion still resolves against the process home (the
	// decoy here) — only a "~" prefix triggers home lookup; plain
	// dataDir values are used as configured.
	wantTilde := filepath.Join(decoyHome, ".datawatch", "secrets")
	if got := DefaultFileProviderDir("~/.datawatch"); got != wantTilde {
		t.Errorf("DefaultFileProviderDir(\"~/.datawatch\")=%q want %q", got, wantTilde)
	}
}
