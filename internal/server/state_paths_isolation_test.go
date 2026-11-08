// State path isolation, end to end: every one of the handlers that were
// moved off the home-directory fallback must serve its read and write
// endpoints from the configured data directory — and never touch
// $HOME/.datawatch, even when that tree already holds live-looking state
// files. This is the broad companion to the per-handler pin tests: it runs
// the whole moved-handler surface against one Server with a decoy HOME and
// requires the decoy tree to be byte-identical afterwards (no reads, no
// writes, no new files).

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// isolationSnapshot maps every entry (file or directory) under root, keyed
// by its slash-separated path relative to root, to "type:size:sha256" (dirs:
// "dir:0:0") so a before/after comparison catches content changes, size
// changes and created or removed entries.
func isolationSnapshot(root string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root))
		if rel == "." {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		sized := fmt.Sprintf("%d", info.Size())
		digest := "0"
		if !d.IsDir() {
			data, rerr := os.ReadFile(p)
			if rerr == nil {
				sum := sha256.Sum256(data)
				digest = hex.EncodeToString(sum[:])
			}
		}
		out[rel] = d.Type().String() + ":" + sized + ":" + digest
		return nil
	})
	return out
}

// TestStatePathIsolationNoHomeReadsOrWrites exercises every safe read and
// write endpoint of the moved handlers (channel routing, dashboard layout,
// smoke progress, discussion WAL, usage readings, encryption status,
// migration status) against a Server whose data directory is a temp dir and
// whose HOME is a decoy seeded with sentinel state at exactly the
// home-relative locations the old fallback code would have used. Every call
// must succeed, every written file must land under the data directory, and
// the decoy HOME tree must be byte-identical afterwards.
//
// Deliberate scope: the encryption wipe and migrate handlers are destructive
// and are covered by TestStatePathSecurityEncryptionUnderDataDir instead.
func TestStatePathIsolationNoHomeReadsOrWrites(t *testing.T) {
	dataDir := t.TempDir()
	decoyHome := t.TempDir()
	t.Setenv("HOME", decoyHome)
	t.Setenv("DATAWATCH_DATA_DIR", "")

	const sentinel = "DECOY-SENTINEL-must-never-be-touched"
	// The discussion id the test will write into; the decoy must already
	// hold a WAL for the very same id.
	const discussionID = "isolation-decoy-discussion"

	// Decoys: exactly the state files the moved handlers use, planted at
	// the home-relative locations the pre-isolation code would have used.
	decoys := map[string]string{
		"config.yaml":                                sentinel,
		"channel_routing.json":                       `{"rules":[{"channel_pattern":"decoy-*","peer_name":"decoy-peer"}]}`,
		"dashboard-layout.json":                      `{"cards":[{"id":"decoy-card","cs":1,"rs":1}]}`,
		"smoke-runs/decoy.json":                      `{"run_id":"decoy","pass":1}`,
		"smoke-progress.json":                        sentinel,
		"discussions/" + discussionID + "/wal.jsonl": `{"seq":99,"content":"decoy-wal-sentinel","role":"user","timestamp":"2020-01-01T00:00:00Z"}` + "\n",
		"usagecalc/usage.json":                       sentinel,
		"burn_guard_state.json":                      sentinel,
		"servers.json":                               sentinel,
		"compute/nodes.json":                         sentinel,
		"v7-migration-status.json":                   `{"migrated":["decoy-node"],"show":true}`,
	}
	for rel, contents := range decoys {
		target := filepath.Join(decoyHome, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatalf("mkdir decoy %s: %v", rel, err)
		}
		if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
			t.Fatalf("write decoy %s: %v", rel, err)
		}
	}

	// Seed the data-directory migration status the way a completed run
	// leaves it, so the status GET can be asserted to read THIS file, not
	// the decoy one (which names a different node).
	migPath := filepath.Join(dataDir, "v7-migration-status.json")
	if err := os.WriteFile(migPath, []byte(`{"migrated":["data-dir-node"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	before := isolationSnapshot(decoyHome)

	// The same construction the other data-directory isolation tests use:
	// a minimal Server with its cfg and dataDir both pointing at the temp
	// data directory, no manager or peer store needed for these endpoints.
	cfg := config.DefaultConfig()
	cfg.DataDir = dataDir
	s := &Server{hostname: "testhost", cfg: cfg, dataDir: dataDir}

	call := func(what string, h func(hh http.ResponseWriter, r *http.Request), method, target string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req := httptest.NewRequest(method, target, reader)
		rr := httptest.NewRecorder()
		h(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s %s: expected 200, got %d body=%s", method, what, rr.Code, rr.Body.String())
		}
		return rr
	}

	// --- channel routing: GET ---------------------------------------------
	rr := call("channel routing", s.handleChannelRouting, http.MethodGet, "/api/channel/routing", nil)
	if strings.Contains(rr.Body.String(), "decoy-peer") || strings.Contains(rr.Body.String(), "decoy-*") {
		t.Fatalf("channel routing GET served decoy HOME content: %s", rr.Body.String())
	}

	// --- dashboard layout: GET then PUT -----------------------------------
	rr = call("dashboard layout GET", s.handleDashboardLayout, http.MethodGet, "/api/dashboard/layout", nil)
	if strings.Contains(rr.Body.String(), "decoy-card") {
		t.Fatalf("dashboard layout GET served decoy HOME content: %s", rr.Body.String())
	}
	_ = call("dashboard layout PUT", s.handleDashboardLayout, http.MethodPut, "/api/dashboard/layout",
		[]byte(`{"cards":[]}`))
	layoutPath := filepath.Join(dataDir, "dashboard-layout.json")
	layoutBytes, err := os.ReadFile(layoutPath)
	if err != nil {
		t.Fatalf("dashboard layout was not written under the data directory: %v", err)
	}
	if strings.Contains(string(layoutBytes), "decoy-card") {
		t.Fatalf("dashboard layout under the data directory carries decoy content: %s", layoutBytes)
	}

	// --- smoke progress: POST then GET ------------------------------------
	rr = call("smoke progress POST", s.handleSmokeProgress, http.MethodPost, "/api/smoke/progress",
		[]byte(`{"run_id":"isolation-run","pass":1}`))
	if strings.Contains(rr.Body.String(), `"decoy"`) {
		t.Fatalf("smoke progress POST leaked decoy content: %s", rr.Body.String())
	}
	smokePath := filepath.Join(dataDir, "smoke-runs", "isolation-run.json")
	smokeBytes, err := os.ReadFile(smokePath)
	if err != nil {
		t.Fatalf("smoke progress record was not written under the data directory: %v", err)
	}
	var smokeRec map[string]any
	if err := json.Unmarshal(smokeBytes, &smokeRec); err != nil {
		t.Fatalf("decode smoke record: %v", err)
	}
	if smokeRec["run_id"] != "isolation-run" {
		t.Fatalf("smoke record run_id=%v, want isolation-run", smokeRec["run_id"])
	}
	rr = call("smoke progress GET", s.handleSmokeProgress, http.MethodGet, "/api/smoke/progress", nil)
	var smokeList []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &smokeList); err != nil {
		t.Fatalf("decode smoke list: %v", err)
	}
	if len(smokeList) != 1 || smokeList[0]["id"] != "isolation-run" {
		t.Fatalf("smoke progress GET = %s, want exactly the one data-directory record", rr.Body.String())
	}

	// --- discussion WAL: write then read ----------------------------------
	rr = call("discussion write", s.handleDiscussionScope, http.MethodPost,
		"/api/memory/discussion/"+discussionID,
		[]byte(`{"content":"real entry under the data dir","role":"user"}`))
	if strings.Contains(rr.Body.String(), "decoy-wal-sentinel") {
		t.Fatalf("discussion write leaked decoy WAL content: %s", rr.Body.String())
	}
	realWAL := filepath.Join(dataDir, "discussions", discussionID, "wal.jsonl")
	walBytes, err := os.ReadFile(realWAL)
	if err != nil {
		t.Fatalf("discussion WAL was not written under the data directory: %v", err)
	}
	if strings.Contains(string(walBytes), "decoy-wal-sentinel") {
		t.Fatalf("WAL under the data directory carries decoy content: %s", walBytes)
	}
	rr = call("discussion WAL read", s.handleDiscussionScope, http.MethodGet,
		"/api/memory/discussion/"+discussionID+"/wal", nil)
	if strings.Contains(rr.Body.String(), "decoy-wal-sentinel") {
		t.Fatalf("discussion WAL read served decoy HOME content: %s", rr.Body.String())
	}
	var walResp struct {
		Entries []struct {
			Content string `json:"content"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &walResp); err != nil {
		t.Fatalf("decode WAL read: %v", err)
	}
	if len(walResp.Entries) != 1 || walResp.Entries[0].Content != "real entry under the data dir" {
		t.Fatalf("WAL read = %s, want exactly the one entry written under the data dir", rr.Body.String())
	}

	// --- usage readings: add then read series ------------------------------
	usageBody, err := json.Marshal(usagecalc.Reading{
		At:      time.Now().UTC().Add(-time.Hour),
		Percent: fptrUsage(61.5),
		Source:  usagecalc.SourceTyped,
	})
	if err != nil {
		t.Fatalf("marshal usage reading: %v", err)
	}
	addReq := httptest.NewRequest(http.MethodPost, "/api/usage/readings", bytes.NewReader(usageBody))
	addReq = withUsageCaps(addReq, federation.CapUsageRead, federation.CapUsageWrite)
	addRR := httptest.NewRecorder()
	s.handleUsageReadings(addRR, addReq)
	if addRR.Code != http.StatusOK {
		t.Fatalf("usage readings POST: expected 200, got %d body=%s", addRR.Code, addRR.Body.String())
	}
	getReq := withUsageCaps(
		httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil),
		federation.CapUsageRead)
	getRR := httptest.NewRecorder()
	s.handleUsageReadings(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("usage series GET: expected 200, got %d body=%s", getRR.Code, getRR.Body.String())
	}
	var rec usagecalc.AccountRecord
	if err := json.NewDecoder(getRR.Body).Decode(&rec); err != nil {
		t.Fatalf("decode usage series: %v (body=%s)", err, getRR.Body.String())
	}
	if len(rec.Readings) != 1 || rec.Readings[0].ID == "" {
		t.Fatalf("usage series = %+v, want exactly the one stored reading", rec)
	}
	usageStore := filepath.Join(dataDir, "usagecalc", "usage.json")
	storeBytes, err := os.ReadFile(usageStore)
	if err != nil {
		t.Fatalf("usage store was not written under the data directory: %v", err)
	}
	if !bytes.Contains(storeBytes, []byte(rec.Readings[0].ID)) {
		t.Fatalf("usage store under the data directory does not carry the stored reading %q", rec.Readings[0].ID)
	}

	// --- encryption status: GET -------------------------------------------
	// (the destructive migrate/wipe endpoints are out of scope here — see
	// TestStatePathSecurityEncryptionUnderDataDir)
	rr = call("encryption status", s.handleSecurityEncryptionStatus, http.MethodGet,
		"/api/security/encryption/status", nil)
	var encStat encryptionStatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &encStat); err != nil {
		t.Fatalf("decode encryption status: %v", err)
	}
	for _, f := range encStat.Files {
		abs := f.Path
		if !pathUnder(t, abs, dataDir) {
			t.Fatalf("encryption status reported path %q that is not under the data directory %q", abs, dataDir)
		}
	}

	// --- migration status: GET --------------------------------------------
	rr = call("migration status", s.handleMigrationStatus, http.MethodGet,
		"/api/migration/status", nil)
	var migDoc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &migDoc); err != nil {
		t.Fatalf("decode migration status: %v", err)
	}
	if migDoc["show"] != true {
		t.Fatalf("migration status did not read the data-directory file: %s", rr.Body.String())
	}
	migList, _ := migDoc["migrated"].([]any)
	gotMig := make([]string, 0, len(migList))
	for _, m := range migList {
		gotMig = append(gotMig, m.(string))
	}
	if !reflect.DeepEqual(gotMig, []string{"data-dir-node"}) {
		t.Fatalf("migration status migrated=%v, want [data-dir-node] (the decoy names decoy-node)", gotMig)
	}

	// --- the decoy HOME must be byte-identical: same file set, same sizes,
	//     same sha256 of every file. Nothing read, written, created or
	//     removed under HOME by any of the endpoints above.
	after := isolationSnapshot(decoyHome)
	if !reflect.DeepEqual(before, after) {
		var added, removed []string
		for p := range after {
			if _, ok := before[p]; !ok {
				added = append(added, p)
			}
		}
		for p := range before {
			if _, ok := after[p]; !ok {
				removed = append(removed, p)
			}
		}
		sort.Strings(added)
		sort.Strings(removed)
		for p := range after {
			if b, ok := before[p]; ok && b != after[p] {
				t.Errorf("decoy HOME entry %q changed:\n before %s\n after  %s", p, b, after[p])
			}
		}
		t.Fatalf("decoy HOME tree changed: added=%v removed=%v", added, removed)
	}
}
