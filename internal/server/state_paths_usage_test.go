package server

// TestStatePathUsageUnderDataDir: the usage store and the burn guard state
// resolve under the server's configured data directory, never under the
// process home directory.
import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

func TestStatePathUsageUnderDataDir(t *testing.T) {
	dataDir := t.TempDir()
	decoyHome := t.TempDir()
	// Decoys at the exact spots the old home-directory fallback would have
	// written, with sentinel content so a stray write is visible byte-for-byte.
	decoyReadings := []byte("decoy: usage readings must not be touched under the home directory\n")
	decoyBurnState := []byte("decoy: burn guard state must not be touched under the home directory\n")
	decoyPaths := map[string][]byte{
		filepath.Join(decoyHome, ".datawatch", "usagecalc", "usage.json"): decoyReadings,
		filepath.Join(decoyHome, ".datawatch", "burn_guard_state.json"):   decoyBurnState,
	}
	for path, contents := range decoyPaths {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir decoy dir for %s: %v", path, err)
		}
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatalf("write decoy %s: %v", path, err)
		}
	}
	t.Setenv("HOME", decoyHome)

	s := newUsageTestServer(t, dataDir, "", "", "", "")

	// Post a reading through the usage reading add handler.
	body, err := json.Marshal(usagecalc.Reading{
		At:      time.Now().UTC().Add(-time.Hour),
		Percent: fptrUsage(61.5),
		Source:  usagecalc.SourceTyped,
	})
	if err != nil {
		t.Fatalf("marshal reading: %v", err)
	}
	addReq := withUsageCaps(httptest.NewRequest(http.MethodPost, "/api/usage/readings", nil),
		federation.CapUsageRead, federation.CapUsageWrite)
	addReq.Body = io.NopCloser(bytes.NewReader(body))
	addRR := httptest.NewRecorder()
	s.handleUsageReadings(addRR, addReq)
	if addRR.Code != http.StatusOK {
		t.Fatalf("POST reading = %d: %s", addRR.Code, addRR.Body.String())
	}

	// Read the series back through the series handler.
	snapRR := httptest.NewRecorder()
	s.handleUsageReadings(snapRR, withUsageCaps(httptest.NewRequest(http.MethodGet, "/api/usage/readings", nil),
		federation.CapUsageRead))
	if snapRR.Code != http.StatusOK {
		t.Fatalf("GET series = %d: %s", snapRR.Code, snapRR.Body.String())
	}
	var rec usagecalc.AccountRecord
	if err := json.NewDecoder(snapRR.Body).Decode(&rec); err != nil {
		t.Fatalf("decode series: %v (body=%s)", err, snapRR.Body.String())
	}
	if len(rec.Readings) != 1 || rec.Readings[0].ID == "" {
		t.Fatalf("series = %+v, want the one stored reading", rec)
	}

	// The data was written under the configured data directory.
	storeFile := filepath.Join(dataDir, "usagecalc", "usage.json")
	disk, err := os.ReadFile(storeFile)
	if err != nil {
		t.Fatalf("the usage store is not under the data directory: %v (looked for %s)", err, storeFile)
	}
	if !bytes.Contains(disk, []byte(rec.Readings[0].ID)) {
		t.Fatalf("store file under the data directory does not carry the stored reading id %q", rec.Readings[0].ID)
	}

	// Nothing new was written anywhere under the decoy home directory...
	var stray []string
	_ = filepath.Walk(filepath.Join(decoyHome, ".datawatch"), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if _, decoy := decoyPaths[p]; !decoy {
			stray = append(stray, p)
		}
		return nil
	})
	if len(stray) != 0 {
		t.Fatalf("usage state was written under the home directory: %v", stray)
	}

	// ...and the decoys are byte-identical to what was seeded.
	for path, want := range decoyPaths {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read decoy %s: %v", path, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("decoy %s was touched: got %q, want %q", path, got, want)
		}
	}
}
