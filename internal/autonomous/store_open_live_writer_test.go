// Guards the "second Store must not clobber a live writer" contract: opening
// a Store on a data directory that another Store is actively writing to must
// not delete that writer's in-flight '.tmp-*' files. Store.loadRecords only
// cleans '.tmp-*' entries older than staleTempFileAge, because a fresh one may
// be the temp file of an atomicWriteFile whose rename has not landed yet —
// removing it makes that rename fail with "no such file or directory" and the
// SavePRD is lost, while an older one is only a leftover of an interrupted
// write (see internal/autonomous/store_records.go).
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// seedTempFile places a '.tmp-live' file in the records directory that looks
// exactly like the temp file an in-progress SavePRD created via atomicWriteFile.
func seedTempFile(t *testing.T, dir string) string {
	t.Helper()
	recDir := filepath.Join(dir, "autonomous", prdRecordsDirName)
	live := filepath.Join(recDir, ".tmp-live")
	if err := os.WriteFile(live, []byte("in flight"), 0o644); err != nil {
		t.Fatalf("seed %s: %v", live, err)
	}
	return live
}

func TestNewStoreKeepsLiveTempFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("first NewStore: %v", err)
	}
	live := seedTempFile(t, dir)

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("second NewStore: %v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("fresh '.tmp-*' file of a live writer was removed by the new Store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "autonomous", prdRecordsDirName, ".tmp-live.json")); !os.IsNotExist(err) {
		t.Fatalf("a PRD record was created for '.tmp-live': %v", err)
	}
	ids, err := s.recordIDs()
	if err != nil {
		t.Fatalf("recordIDs: %v", err)
	}
	for _, id := range ids {
		if id == ".tmp-live" {
			t.Fatalf("new Store holds a PRD record for '.tmp-live'")
		}
	}
}

func TestNewStoreRemovesStaleTempFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("first NewStore: %v", err)
	}
	live := seedTempFile(t, dir)
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(live, twoHoursAgo, twoHoursAgo); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if _, err := NewStore(dir); err != nil {
		t.Fatalf("second NewStore: %v", err)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatalf("stale '.tmp-*' leftover of an interrupted write was not cleaned up: %v", err)
	}
}

func TestSavePRDSurvivesConcurrentOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	prd, err := s.CreatePRD("save while a second Store keeps reopening", dir, "cli", "model", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}

	// A goroutine keeps reopening the same data directory (each open sweeps
	// '.tmp-*' files) while the test goroutine keeps saving. Before the fix
	// the sweep could delete the save's temp file between its creation and
	// rename, so roughly one save in five failed with
	// 'rename ...: no such file or directory'.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	var openMu sync.Mutex
	openErrs := 0
	var firstOpenErr error
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := NewStore(dir); err != nil {
				openMu.Lock()
				openErrs++
				if firstOpenErr == nil {
					firstOpenErr = err
				}
				openMu.Unlock()
			}
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	saveErrs := 0
	var firstSaveErr error
	for n := 0; time.Now().Before(deadline); n++ {
		prd.Spec = fmt.Sprintf("save under concurrent opens, revision %d", n)
		if err := s.SavePRD(prd); err != nil {
			saveErrs++
			if firstSaveErr == nil {
				firstSaveErr = err
			}
		}
	}
	close(stop)
	wg.Wait()

	if saveErrs != 0 {
		t.Fatalf("%d SavePRD call(s) failed while a second Store kept opening; first: %v", saveErrs, firstSaveErr)
	}
	if openErrs != 0 {
		openMu.Lock()
		defer openMu.Unlock()
		t.Fatalf("%d NewStore call(s) failed while saves were in flight; first: %v", openErrs, firstOpenErr)
	}
}
