// GH#192 D78a — memory tags, full end-to-end. Store-level SetTags/
// migration/read-path coverage lives in store_test.go; this covers the
// ServerAdapter.SetTags capability-cast wrapper (the one concrete type
// wired as both server.MemoryAPI and mcp.MemoryMCP in production).

package memory

import (
	"path/filepath"
	"testing"
)

func TestServerAdapter_SetTags(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close() //nolint:errcheck

	retriever := NewRetriever(store, nil, 5)
	adapter := NewServerAdapter(retriever, "/proj")

	id, err := adapter.Remember("/proj", "a memory")
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	if err := adapter.SetTags(id, "alpha,beta"); err != nil {
		t.Fatalf("SetTags: %v", err)
	}

	memories, err := adapter.ListRecent("/proj", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(memories))
	}
	if memories[0]["tags"] != "alpha,beta" {
		t.Errorf("tags = %v, want 'alpha,beta' -- confirms convertToMaps carries the field through, not just the Store struct", memories[0]["tags"])
	}
}
