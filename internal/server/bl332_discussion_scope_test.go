// BL332 T42a — Discussion Scopes REST API tests.
//
// TS-1:  GET /api/memory/discussion with no discussions dir → 200 with empty list
// TS-2:  POST entry to discussion/{id}, then GET → returns entry
// TS-3:  After POST, GET /api/memory/discussion/{id}/wal → WAL has entry
// TS-4:  POST entry, DELETE discussion/{id}, GET → empty

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/memory"
)

// fakeDiscussionBackend is a minimal in-memory Backend for discussion scope tests.
type fakeDiscussionBackend struct {
	mu      sync.Mutex
	entries []memory.Memory
	nextID  int64
}

func (b *fakeDiscussionBackend) Save(projectDir, content, summary, role, sessionID string, embedding []float32) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	b.entries = append(b.entries, memory.Memory{
		ID:         b.nextID,
		ProjectDir: projectDir,
		Content:    content,
		Summary:    summary,
		Role:       role,
		SessionID:  sessionID,
	})
	return b.nextID, nil
}

func (b *fakeDiscussionBackend) SaveWithMeta(projectDir, content, summary, role, sessionID, wing, room, hall string, embedding []float32) (int64, error) {
	return b.Save(projectDir, content, summary, role, sessionID, embedding)
}

func (b *fakeDiscussionBackend) ListByRole(projectDir, role string, n int) ([]memory.Memory, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []memory.Memory
	for _, m := range b.entries {
		if m.Role == role {
			out = append(out, m)
			if len(out) >= n {
				break
			}
		}
	}
	return out, nil
}

func (b *fakeDiscussionBackend) Delete(id int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	next := b.entries[:0]
	for _, m := range b.entries {
		if m.ID != id {
			next = append(next, m)
		}
	}
	b.entries = next
	return nil
}

// Stub the rest of the Backend interface.
func (b *fakeDiscussionBackend) Search(projectDir string, queryVec []float32, topK int) ([]memory.Memory, error) {
	return nil, nil
}
func (b *fakeDiscussionBackend) SearchAll(queryVec []float32, topK int) ([]memory.Memory, error) {
	return nil, nil
}
func (b *fakeDiscussionBackend) SearchFiltered(wing, room string, queryVec []float32, topK int) ([]memory.Memory, error) {
	return nil, nil
}
func (b *fakeDiscussionBackend) ListRecent(projectDir string, n int) ([]memory.Memory, error) {
	return nil, nil
}
func (b *fakeDiscussionBackend) ListFiltered(projectDir, role, since string, n int) ([]memory.Memory, error) {
	return nil, nil
}
func (b *fakeDiscussionBackend) Count(projectDir string) (int, error)           { return 0, nil }
func (b *fakeDiscussionBackend) CountAll() (int, error)                         { return 0, nil }
func (b *fakeDiscussionBackend) FindDuplicate(projectDir, content string) int64 { return 0 }
func (b *fakeDiscussionBackend) Prune(olderThan time.Duration) (int64, error)   { return 0, nil }
func (b *fakeDiscussionBackend) PruneByRole(role string, olderThan time.Duration) (int64, error) {
	return 0, nil
}
func (b *fakeDiscussionBackend) Stats() memory.MemoryStats                        { return memory.MemoryStats{} }
func (b *fakeDiscussionBackend) DistinctProjects() ([]string, error)              { return nil, nil }
func (b *fakeDiscussionBackend) ListWings() (map[string]int, error)               { return nil, nil }
func (b *fakeDiscussionBackend) ListRooms(wing string) ([]string, error)          { return nil, nil }
func (b *fakeDiscussionBackend) FindTunnels() (map[string][]string, error)        { return nil, nil }
func (b *fakeDiscussionBackend) ListForReindex() ([]struct{ ID int64; Content string }, error) {
	return nil, nil
}
func (b *fakeDiscussionBackend) UpdateEmbedding(id int64, embedding []float32) error { return nil }
func (b *fakeDiscussionBackend) ApplyRetention(policy memory.RetentionPolicy) int64  { return 0 }
func (b *fakeDiscussionBackend) WALRecent(n int) []memory.WALEntry                   { return nil }
func (b *fakeDiscussionBackend) SetScore(id int64, score int) error                  { return nil }
func (b *fakeDiscussionBackend) IsEncrypted() bool                                   { return false }
func (b *fakeDiscussionBackend) Close() error                                        { return nil }
func (b *fakeDiscussionBackend) Export(w io.Writer) error                            { return nil }
func (b *fakeDiscussionBackend) Import(r io.Reader) (int, error)                     { return 0, nil }

// Compile-time check: fakeDiscussionBackend must implement memory.Backend.
var _ memory.Backend = (*fakeDiscussionBackend)(nil)

// newDiscussionTestServer creates a test Server pointed at a temp .datawatch dir.
func newDiscussionTestServer(t *testing.T, backend memory.Backend) *Server {
	t.Helper()

	// Override HOME so WAL/discussion paths resolve inside the temp dir.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	return &Server{memoryBackend: backend}
}

// TestDiscussionScope_ListEmpty — GET /api/memory/discussion with no discussions
// dir returns 200 with an empty list.
func TestDiscussionScope_ListEmpty(t *testing.T) {
	s := newDiscussionTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/memory/discussion", nil)
	rr := httptest.NewRecorder()
	s.handleDiscussionScopeList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	count, _ := resp["count"].(float64)
	if count != 0 {
		t.Errorf("expected count=0, got %v", count)
	}
	discussions, _ := resp["discussions"].([]any)
	if len(discussions) != 0 {
		t.Errorf("expected empty discussions, got %v", discussions)
	}
}

// TestDiscussionScope_WriteAndRecall — POST entry to discussion/test-id, then
// GET returns it.
func TestDiscussionScope_WriteAndRecall(t *testing.T) {
	backend := &fakeDiscussionBackend{}
	s := newDiscussionTestServer(t, backend)

	// POST a memory entry.
	body := map[string]any{
		"content": "hello discussion world",
		"role":    "user",
	}
	bBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/discussion/test-id", bytes.NewReader(bBody))
	rr := httptest.NewRecorder()
	s.handleDiscussionScope(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// GET the discussion — should return the entry.
	req2 := httptest.NewRequest(http.MethodGet, "/api/memory/discussion/test-id", nil)
	rr2 := httptest.NewRecorder()
	s.handleDiscussionScope(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("GET expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	var resp2 map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	count, _ := resp2["count"].(float64)
	if count != 1 {
		t.Errorf("expected count=1, got %v (body=%s)", count, rr2.Body.String())
	}
}

// TestDiscussionScope_WALPopulated — after POST, GET /api/memory/discussion/{id}/wal
// returns at least one WAL entry.
func TestDiscussionScope_WALPopulated(t *testing.T) {
	backend := &fakeDiscussionBackend{}
	s := newDiscussionTestServer(t, backend)

	// POST an entry.
	body := map[string]any{"content": "wal test content", "role": "user"}
	bBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/discussion/wal-test", bytes.NewReader(bBody))
	rr := httptest.NewRecorder()
	s.handleDiscussionScope(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST expected 200, got %d", rr.Code)
	}

	// GET the WAL.
	req2 := httptest.NewRequest(http.MethodGet, "/api/memory/discussion/wal-test/wal", nil)
	rr2 := httptest.NewRecorder()
	s.handleDiscussionScope(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("WAL GET expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode WAL response: %v", err)
	}
	count, _ := resp["count"].(float64)
	if count < 1 {
		t.Errorf("expected WAL count>=1, got %v (body=%s)", count, rr2.Body.String())
	}
}

// TestDiscussionScope_Delete — POST entry, DELETE discussion/{id}, GET → empty.
func TestDiscussionScope_Delete(t *testing.T) {
	backend := &fakeDiscussionBackend{}
	s := newDiscussionTestServer(t, backend)

	// POST an entry.
	body := map[string]any{"content": "to be deleted", "role": "user"}
	bBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/memory/discussion/del-test", bytes.NewReader(bBody))
	rr := httptest.NewRecorder()
	s.handleDiscussionScope(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST expected 200, got %d", rr.Code)
	}

	// DELETE the discussion.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/memory/discussion/del-test", nil)
	rr2 := httptest.NewRecorder()
	s.handleDiscussionScope(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("DELETE expected 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}

	// GET should now return empty.
	req3 := httptest.NewRequest(http.MethodGet, "/api/memory/discussion/del-test", nil)
	rr3 := httptest.NewRecorder()
	s.handleDiscussionScope(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("GET after DELETE expected 200, got %d", rr3.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr3.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	count, _ := resp["count"].(float64)
	if count != 0 {
		t.Errorf("expected count=0 after delete, got %v", count)
	}
}
