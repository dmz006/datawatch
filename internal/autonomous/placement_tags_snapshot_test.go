package autonomous

import (
	"reflect"
	"testing"
)

// snapshotFakeReader wraps the fake reader with a snapshot method and counts
// the per-Automaton and snapshot calls.
type snapshotFakeReader struct {
	*fakeInheritedReader
	perPRDCalls int
	snapshots   int
}

func (f *snapshotFakeReader) SystemTagsByPRD(prdID string) []string {
	f.perPRDCalls++
	return f.fakeInheritedReader.SystemTagsByPRD(prdID)
}

func (f *snapshotFakeReader) SystemTagSnapshot() func(string) []string {
	f.snapshots++
	return f.fakeInheritedReader.SystemTagsByPRD // bypasses the counting method
}

func seedSnapshotFake() *fakeInheritedReader {
	r := &fakeInheritedReader{}
	r.addSession("prd-1", "s1", "node-a", "model-x")
	r.addSession("prd-1", "s2", "model-x", "node-b", "")
	r.addSession("prd-3", "s3")
	return r
}

func TestInheritedTagsSnapshot_MatchesPerPRD(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reader := &snapshotFakeReader{fakeInheritedReader: seedSnapshotFake()}
	m.SetInheritedTagReader(reader)

	ids := []string{"prd-1", "prd-2", "prd-3", ""}
	want := map[string][]string{}
	for _, id := range ids {
		want[id] = m.InheritedTagsForPRD(id)
	}
	reader.perPRDCalls = 0

	snap := m.InheritedTagsSnapshot()
	for _, id := range ids {
		if got := snap(id); !reflect.DeepEqual(got, want[id]) {
			t.Errorf("snapshot(%q) = %v, want %v", id, got, want[id])
		}
	}
	if reader.perPRDCalls != 0 {
		t.Errorf("per-Automaton reader called %d times, want 0 when a snapshot exists", reader.perPRDCalls)
	}
	if reader.snapshots != 1 {
		t.Errorf("snapshot built %d times, want 1", reader.snapshots)
	}
}

func TestInheritedTagsSnapshot_FallbackAndNoReader(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.InheritedTagsSnapshot()("prd-1"); got != nil {
		t.Errorf("no reader: got %v, want nil", got)
	}
	m.SetInheritedTagReader(seedSnapshotFake()) // no snapshot method
	snap := m.InheritedTagsSnapshot()
	for _, id := range []string{"prd-1", "prd-2", "prd-3", ""} {
		if got, want := snap(id), m.InheritedTagsForPRD(id); !reflect.DeepEqual(got, want) {
			t.Errorf("fallback snapshot(%q) = %v, want %v", id, got, want)
		}
	}
}
