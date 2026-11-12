// The encryption status/migrate/wipe handlers (BL334 T43e) must build their
// state-file list from the server's configured data directory
// (Server.statePath), never from a home directory derived from the process
// environment.

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestStatePathSecurityEncryptionUnderDataDir pins the security encryption
// handlers' file list to the configured data directory: a Server with a temp
// data directory and a decoy HOME holding plaintext sentinel files must
// report only data-directory paths, with the decoy HOME tree left exactly as
// it was (status) and with wipe considering only the single sentinel file in
// a dedicated test-created data directory.
func TestStatePathSecurityEncryptionUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	decoyHome := t.TempDir()
	t.Setenv("HOME", decoyHome)

	sentinel := "DECOY-SENTINEL-do-not-touch"

	// Decoy: what the old home-directory code would have probed.
	secDir := filepath.Join(decoyHome, ".datawatch")
	if err := os.MkdirAll(filepath.Join(secDir, "compute"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secDir, "servers.json"), []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secDir, "compute", "nodes.json"), []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	before := statePathTree(t, decoyHome)

	// Real data directory: one plaintext file the handler may legitimately
	// probe and consider.
	sentinelFile := filepath.Join(dataDir, "servers.json")
	if err := os.WriteFile(sentinelFile, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.DataDir = dataDir
	s := &Server{
		cfg:     cfg,
		cfgPath: filepath.Join(t.TempDir(), "config.yaml"),
		dataDir: dataDir,
	}

	// --- status -----------------------------------------------------------
	res := mpDo(s.handleSecurityEncryptionStatus, "GET", "/api/security/encryption/status", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", res.Code, res.Body)
	}
	var body encryptionStatusResponse
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode status body: %v", err)
	}

	// Every reported path must live under the data directory; none may point
	// at the decoy HOME.
	for _, f := range body.Files {
		if !pathUnder(t, f.Path, dataDir) {
			t.Fatalf("reported path %q is not under data directory %q", f.Path, dataDir)
		}
		if pathUnder(t, f.Path, decoyHome) {
			t.Fatalf("reported path %q points at the decoy HOME", f.Path)
		}
	}

	// The data-directory sentinel must be the servers.json that is
	// considered: it exists and is plaintext (secure mode off, no enc key).
	foundDataServers := false
	for _, f := range body.Files {
		if pathUnder(t, f.Path, dataDir) && filepath.Base(f.Path) == "servers.json" &&
			filepath.Dir(f.Path) == dataDir {
			foundDataServers = true
			if !f.Exists || f.Encrypted {
				t.Fatalf("data directory servers.json: %+v", f)
			}
		}
	}
	if !foundDataServers {
		t.Fatalf("data directory servers.json %q not present in the reported file list", sentinelFile)
	}

	// The decoy HOME tree — including the sentinel files — is untouched.
	if after := statePathTree(t, decoyHome); !reflect.DeepEqual(before, after) {
		t.Fatalf("decoy HOME tree changed: before=%v after=%v", before, after)
	}
	for _, p := range []string{filepath.Join(secDir, "servers.json"), filepath.Join(secDir, "compute", "nodes.json")} {
		data, err := os.ReadFile(p)
		if err != nil || string(data) != sentinel {
			t.Fatalf("decoy file %q was touched: %q, %v", p, data, err)
		}
	}

	// --- wipe -------------------------------------------------------------
	// Wipe is exercised only against a dedicated data directory created by
	// this test, holding only sentinel files (servers.json and
	// channel_routing.json): the wipe must consider only those files and
	// touch nothing in the decoy HOME.
	wipeDir := t.TempDir()
	wipeTarget := filepath.Join(wipeDir, "servers.json")
	wipeRouting := filepath.Join(wipeDir, "channel_routing.json")
	for _, p := range []string{wipeTarget, wipeRouting} {
		if err := os.WriteFile(p, []byte(sentinel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wipeBefore := statePathTree(t, decoyHome)

	ws := &Server{
		cfg:     cfg,
		cfgPath: filepath.Join(t.TempDir(), "config.yaml"),
		dataDir: wipeDir,
	}
	wipeRes := mpDo(ws.handleSecurityWipePlaintext, "POST", "/api/security/wipe-plaintext", `{"confirm":true}`)
	if wipeRes.Code != http.StatusOK {
		t.Fatalf("POST wipe = %d: %s", wipeRes.Code, wipeRes.Body)
	}
	var wipeBody struct {
		Wiped   []string `json:"wiped"`
		Skipped []string `json:"skipped"`
	}
	if err := json.Unmarshal(wipeRes.Body.Bytes(), &wipeBody); err != nil {
		t.Fatalf("decode wipe body: %v", err)
	}
	want0 := []string{wipeRouting, wipeTarget}
	want1 := []string{wipeTarget, wipeRouting}
	if !reflect.DeepEqual(wipeBody.Wiped, want0) && !reflect.DeepEqual(wipeBody.Wiped, want1) {
		t.Fatalf("wipe considered %v, want exactly the two sentinel files %v (in some order)", wipeBody.Wiped, want0)
	}
	if len(wipeBody.Skipped) != 0 {
		t.Fatalf("wipe skipped %v, want none", wipeBody.Skipped)
	}
	for _, p := range []string{wipeTarget, wipeRouting} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("wiped file %q still present: %v", p, err)
		}
	}
	if wipeAfter := statePathTree(t, decoyHome); !reflect.DeepEqual(wipeBefore, wipeAfter) {
		t.Fatalf("decoy HOME tree changed during wipe: before=%v after=%v", wipeBefore, wipeAfter)
	}
}

// pathUnder reports whether p resolves inside dir (exactly or strictly
// below it), via a ".."-free Rel rather than a bare prefix match.
func pathUnder(t *testing.T, p, dir string) bool {
	t.Helper()
	absP, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs(%q): %v", p, err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs(%q): %v", dir, err)
	}
	if absP == absDir {
		return true
	}
	rel, err := filepath.Rel(absDir, absP)
	if err != nil || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
