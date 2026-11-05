package trace

import "testing"

// An id that is not an indexed session never reaches the file system: a
// traversal-shaped id reads nothing and creates nothing.
func TestStoreReadIgnoresUnindexedIDs(t *testing.T) {
	st, err := NewStore(t.TempDir(), Settings{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../../etc/passwd", "..", "a/b", ""} {
		evs, err := st.Read(id, Filter{})
		if err != nil || len(evs) != 0 {
			t.Fatalf("Read(%q) = %v, %v; want nothing", id, evs, err)
		}
	}
}
