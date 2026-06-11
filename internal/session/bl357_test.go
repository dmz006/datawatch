package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestQueueStore(t *testing.T) *QueueStore {
	t.Helper()
	dir := t.TempDir()
	qs, err := NewQueueStore(filepath.Join(dir, "queue.json"))
	if err != nil {
		t.Fatalf("NewQueueStore: %v", err)
	}
	return qs
}

func TestBL357_Push(t *testing.T) {
	qs := newTestQueueStore(t)

	it1, err := qs.Push("worker", map[string]any{"task": "a"})
	if err != nil {
		t.Fatalf("Push 1: %v", err)
	}
	it2, err := qs.Push("worker", map[string]any{"task": "b"})
	if err != nil {
		t.Fatalf("Push 2: %v", err)
	}

	items := qs.List("", "")
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if it1.State != QueueStatePending {
		t.Errorf("item1 state = %q, want pending", it1.State)
	}
	if it2.State != QueueStatePending {
		t.Errorf("item2 state = %q, want pending", it2.State)
	}
	if it1.Role != "worker" || it2.Role != "worker" {
		t.Errorf("unexpected roles: %q %q", it1.Role, it2.Role)
	}
}

func TestBL357_Claim(t *testing.T) {
	qs := newTestQueueStore(t)
	orig, err := qs.Push("worker", map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	claimed, err := qs.Claim("worker", "host-abc123", 60)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed == nil {
		t.Fatal("Claim returned nil, expected item")
	}
	if claimed.ID != orig.ID {
		t.Errorf("claimed wrong item: %s vs %s", claimed.ID, orig.ID)
	}
	if claimed.State != QueueStateClaimed {
		t.Errorf("state = %q, want claimed", claimed.State)
	}
	if claimed.ClaimedBy != "host-abc123" {
		t.Errorf("claimed_by = %q, want host-abc123", claimed.ClaimedBy)
	}
	if claimed.LeaseExpiry.IsZero() {
		t.Error("lease_expiry not set")
	}
}

func TestBL357_ClaimNoneAvailable(t *testing.T) {
	qs := newTestQueueStore(t)

	item, err := qs.Claim("worker", "host-abc", 60)
	if err != nil {
		t.Fatalf("Claim on empty queue error: %v", err)
	}
	if item != nil {
		t.Errorf("expected nil item on empty queue, got %+v", item)
	}
}

func TestBL357_Complete(t *testing.T) {
	qs := newTestQueueStore(t)
	_, err := qs.Push("worker", nil)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	claimed, err := qs.Claim("worker", "host-xyz", 60)
	if err != nil || claimed == nil {
		t.Fatalf("Claim failed: err=%v item=%v", err, claimed)
	}

	result := map[string]any{"output": "done"}
	if err := qs.Complete(claimed.ID, result); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, ok := qs.Get(claimed.ID)
	if !ok {
		t.Fatal("item not found after Complete")
	}
	if got.State != QueueStateComplete {
		t.Errorf("state = %q, want complete", got.State)
	}
	if got.CompletedAt.IsZero() {
		t.Error("completed_at not set")
	}
}

func TestBL357_Fail(t *testing.T) {
	qs := newTestQueueStore(t)
	_, err := qs.Push("worker", nil)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	claimed, err := qs.Claim("worker", "host-xyz", 60)
	if err != nil || claimed == nil {
		t.Fatalf("Claim failed: err=%v item=%v", err, claimed)
	}

	if err := qs.Fail(claimed.ID, "something went wrong"); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	got, ok := qs.Get(claimed.ID)
	if !ok {
		t.Fatal("item not found after Fail")
	}
	if got.State != QueueStateFailed {
		t.Errorf("state = %q, want failed", got.State)
	}
	if got.Error != "something went wrong" {
		t.Errorf("error = %q, want 'something went wrong'", got.Error)
	}
}

func TestBL357_ExpireLease(t *testing.T) {
	qs := newTestQueueStore(t)
	_, err := qs.Push("worker", nil)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	claimed, err := qs.Claim("worker", "host-xyz", 1)
	if err != nil || claimed == nil {
		t.Fatalf("Claim failed: err=%v item=%v", err, claimed)
	}
	if claimed.State != QueueStateClaimed {
		t.Fatalf("expected claimed state, got %s", claimed.State)
	}

	// Manually backdate the lease to the past.
	qs.mu.Lock()
	qs.items[claimed.ID].LeaseExpiry = time.Now().Add(-5 * time.Second)
	qs.mu.Unlock()

	n := qs.ExpireLeases()
	if n != 1 {
		t.Errorf("ExpireLeases returned %d, want 1", n)
	}

	got, ok := qs.Get(claimed.ID)
	if !ok {
		t.Fatal("item not found after ExpireLeases")
	}
	if got.State != QueueStatePending {
		t.Errorf("state after expiry = %q, want pending", got.State)
	}
	if got.ClaimedBy != "" {
		t.Errorf("claimed_by should be cleared, got %q", got.ClaimedBy)
	}
}

func TestBL357_ListFilter(t *testing.T) {
	qs := newTestQueueStore(t)

	_, err := qs.Push("r1", map[string]any{"n": 1})
	if err != nil {
		t.Fatalf("Push r1: %v", err)
	}
	_, err = qs.Push("r1", map[string]any{"n": 2})
	if err != nil {
		t.Fatalf("Push r1 #2: %v", err)
	}
	_, err = qs.Push("r2", map[string]any{"n": 3})
	if err != nil {
		t.Fatalf("Push r2: %v", err)
	}

	r1Items := qs.List("r1", "")
	if len(r1Items) != 2 {
		t.Errorf("List(r1) = %d items, want 2", len(r1Items))
	}
	for _, it := range r1Items {
		if it.Role != "r1" {
			t.Errorf("unexpected role %q in r1 list", it.Role)
		}
	}

	r2Items := qs.List("r2", "")
	if len(r2Items) != 1 {
		t.Errorf("List(r2) = %d items, want 1", len(r2Items))
	}
}

func TestBL357_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")

	// Create store, push item, close.
	qs1, err := NewQueueStore(path)
	if err != nil {
		t.Fatalf("NewQueueStore: %v", err)
	}
	it, err := qs1.Push("persist-role", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("Push: %v", err)
	}

	// Reload store from disk.
	qs2, err := NewQueueStore(path)
	if err != nil {
		t.Fatalf("NewQueueStore reload: %v", err)
	}
	got, ok := qs2.Get(it.ID)
	if !ok {
		t.Fatal("item not found after reload")
	}
	if got.Role != "persist-role" {
		t.Errorf("role = %q, want persist-role", got.Role)
	}

	// Ensure file is gone if all items deleted.
	if err := qs2.Delete(it.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); err != nil && !os.IsNotExist(err) {
		t.Errorf("unexpected stat error: %v", err)
	}
}
