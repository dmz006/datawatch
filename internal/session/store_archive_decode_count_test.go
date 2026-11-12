package session

import (
	"testing"
	"time"
)

// Tests for the archived-record decode counter (store_archive.go). They check
// how many archived lines a listing and a single lookup actually read.

func TestStoreArchiveDecodeCount(t *testing.T) {
	s, _ := archStore(t, nil)
	for _, sess := range []*Session{
		archSess("dc01", StateComplete, 20*day),
		archSess("dc02", StateFailed, 15*day),
		archSess("dc03", StateKilled, 30*day),
	} {
		if err := s.Save(sess); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.ArchiveOnce(time.Now()); err != nil || n != 3 {
		t.Fatalf("archived %d, err %v; want 3", n, err)
	}

	// A listing with no filter decodes every archived record once.
	s.resetArchiveDecodeCount()
	out, total, err := s.ListArchivedFiltered(ArchivedQuery{})
	if err != nil || total != 3 || len(out) != 3 {
		t.Fatalf("list = %v total=%d err=%v; want 3 records", ids(out), total, err)
	}
	if got := s.archiveDecodeCount(); got != 3 {
		t.Fatalf("decode count after unfiltered list = %d, want 3", got)
	}

	// A lookup of one id decodes exactly one record.
	s.resetArchiveDecodeCount()
	if _, ok := s.GetArchived("h-dc01"); !ok {
		t.Fatal("GetArchived(h-dc01) not found")
	}
	if got := s.archiveDecodeCount(); got != 1 {
		t.Fatalf("decode count after GetArchived = %d, want 1", got)
	}
}
