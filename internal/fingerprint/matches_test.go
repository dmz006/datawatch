package fingerprint

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

const testHash = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func newTestStore(t *testing.T) *MatchStore {
	t.Helper()
	s, err := NewMatchStore(t.TempDir() + "/matches.json")
	if err != nil {
		t.Fatalf("NewMatchStore: %v", err)
	}
	return s
}

func TestMatchStoreCreatesPending(t *testing.T) {
	s := newTestStore(t)
	m, created := s.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)
	if !created {
		t.Fatalf("first Observe: created = false, want true")
	}
	if m.State != StatePending {
		t.Fatalf("new match state = %q, want %q", m.State, StatePending)
	}
	if m.Peer != "peer-a" || m.Kind != KindNode || m.LocalName != "gpu-0" || m.PeerName != "node-b1" {
		t.Fatalf("match fields wrong: %+v", m)
	}
	if m.ID != MatchID("peer-a", KindNode, "gpu-0", "node-b1") {
		t.Fatalf("match id = %q, want deterministic MatchID", m.ID)
	}
	// Only the first 8 hex characters of the hash are retained; the
	// full fingerprint must never be recorded.
	if m.HashPrefix != testHash[:8] {
		t.Fatalf("hash_prefix = %q, want %q", m.HashPrefix, testHash[:8])
	}
	for _, rec := range s.List("") {
		if strings.Contains(rec.HashPrefix, testHash[8:]) {
			t.Fatalf("full hash leaked into persisted record %+v", rec)
		}
	}
	if m.FirstSeen.IsZero() || m.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: %+v", m)
	}

	// A second, distinct observation for a different local name of the
	// same kind must land on a different id.
	m2, created := s.Observe("peer-a", KindNode, "gpu-1", "node-b2", testHash)
	if !created || m2.ID == m.ID {
		t.Fatalf("distinct observation collided: %v, %v", created, m2.ID)
	}
}

func TestMatchStoreConfirm(t *testing.T) {
	s := newTestStore(t)
	m, _ := s.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)

	conf, err := s.Confirm(m.ID)
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if conf.State != StateConfirmed {
		t.Fatalf("state after confirm = %q, want %q", conf.State, StateConfirmed)
	}

	// Confirming again is a no-op success and leaves the state alone.
	again, err := s.Confirm(m.ID)
	if err != nil {
		t.Fatalf("repeat Confirm: %v", err)
	}
	if again.State != StateConfirmed {
		t.Fatalf("state after repeat confirm = %q, want %q", again.State, StateConfirmed)
	}
}

func TestMatchStoreReject(t *testing.T) {
	s := newTestStore(t)
	m, _ := s.Observe("peer-a", KindBilling, "shared-sub", "pool-1", testHash)

	// Reject from pending.
	rej, err := s.Reject(m.ID)
	if err != nil {
		t.Fatalf("Reject(pending): %v", err)
	}
	if rej.State != StateRejected {
		t.Fatalf("state after reject = %q, want %q", rej.State, StateRejected)
	}

	// Reject from confirmed: the operator's un-merge path.
	m2, _ := s.Observe("peer-a", KindBilling, "shared-sub", "pool-2", testHash)
	if _, err := s.Confirm(m2.ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	unmerged, err := s.Reject(m2.ID)
	if err != nil {
		t.Fatalf("Reject(confirmed): %v", err)
	}
	if unmerged.State != StateRejected {
		t.Fatalf("state after un-merge = %q, want %q", unmerged.State, StateRejected)
	}

	// Re-rejecting is a no-op success.
	again, err := s.Reject(m.ID)
	if err != nil || again.State != StateRejected {
		t.Fatalf("repeat Reject = (%q, %v), want (%q, nil)", again.State, err, StateRejected)
	}
}

func TestMatchStoreRejectedStaysRejected(t *testing.T) {
	s := newTestStore(t)
	m, _ := s.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)
	if _, err := s.Reject(m.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// The peer keeps showing the collision: re-observation must not
	// resurrect the record as pending.
	again, created := s.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)
	if created {
		t.Fatalf("re-Observe of a known match reported created = true")
	}
	if again.State != StateRejected {
		t.Fatalf("re-observed match state = %q, want %q", again.State, StateRejected)
	}

	// A confirmed match is equally sticky on re-observation.
	m2, _ := s.Observe("peer-a", KindNode, "gpu-1", "node-b2", testHash)
	if _, err := s.Confirm(m2.ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	again2, _ := s.Observe("peer-a", KindNode, "gpu-1", "node-b2", testHash)
	if again2.State != StateConfirmed {
		t.Fatalf("confirmed match drifted to %q on re-observation", again2.State)
	}
}

func TestMatchStoreObserveIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	first, created := s.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)
	if !created {
		t.Fatalf("first Observe: created = false")
	}
	for i := 0; i < 5; i++ {
		again, created := s.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)
		if created {
			t.Fatalf("repeat Observe %d: created = true", i)
		}
		if again.ID != first.ID {
			t.Fatalf("repeat Observe %d: id drifted to %q", i, again.ID)
		}
	}
	if got := len(s.List("")); got != 1 {
		t.Fatalf("List after repeated Observe has %d records, want 1", got)
	}

	// Concurrent observations of the same match must still converge to
	// one record.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)
		}()
	}
	wg.Wait()
	if got := len(s.List("")); got != 1 {
		t.Fatalf("List after concurrent Observe has %d records, want 1", got)
	}
}

func TestMatchStorePersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/matches.json"
	original, err := NewMatchStore(path)
	if err != nil {
		t.Fatalf("NewMatchStore: %v", err)
	}
	m, _ := original.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)
	if _, err := original.Confirm(m.ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	m2, _ := original.Observe("peer-a", KindBilling, "shared-sub", "pool-1", testHash)
	if _, err := original.Reject(m2.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	reloaded, err := NewMatchStore(path)
	if err != nil {
		t.Fatalf("NewMatchStore (reload): %v", err)
	}
	got, ok := reloaded.Get(m.ID)
	if !ok {
		t.Fatalf("reloaded store lost confirmed match %q", m.ID)
	}
	if got.State != StateConfirmed {
		t.Fatalf("reloaded match state = %q, want %q", got.State, StateConfirmed)
	}
	got2, ok := reloaded.Get(m2.ID)
	if !ok {
		t.Fatalf("reloaded store lost rejected match %q", m2.ID)
	}
	if got2.State != StateRejected || got2.HashPrefix != testHash[:8] {
		t.Fatalf("reloaded rejected match = %+v", got2)
	}
	if got.LocalName != "gpu-0" || got.Peer != "peer-a" {
		t.Fatalf("reloaded record fields wrong: %+v", got)
	}
	if len(reloaded.List("")) != 2 {
		t.Fatalf("reloaded List has %d records, want 2", len(reloaded.List("")))
	}
}

func TestMatchStoreUnknownIDErrors(t *testing.T) {
	s := newTestStore(t)
	const unknown = "deadbeefdeadbeef"

	if _, ok := s.Get(unknown); ok {
		t.Fatalf("Get(unknown) reported found")
	}
	if _, err := s.Confirm(unknown); !errors.Is(err, ErrUnknownMatch) {
		t.Fatalf("Confirm(unknown) err = %v, want ErrUnknownMatch", err)
	}
	if _, err := s.Reject(unknown); !errors.Is(err, ErrUnknownMatch) {
		t.Fatalf("Reject(unknown) err = %v, want ErrUnknownMatch", err)
	}
}

func TestMatchStoreSharedWithOnlyConfirmed(t *testing.T) {
	s := newTestStore(t)
	hash := testHash

	// One pending, one rejected, one confirmed — all on the same local
	// resource. Only the confirmed one may leak out.
	pending, _ := s.Observe("peer-a", KindNode, "gpu-0", "node-b1", hash)
	otherPeer, _ := s.Observe("peer-b", KindNode, "gpu-0", "node-b2", hash)
	confirmed, _ := s.Observe("peer-c", KindNode, "gpu-0", "node-b3", hash)
	if _, err := s.Reject(otherPeer.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if _, err := s.Confirm(confirmed.ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	// Different local name: confirmed but must not appear here.
	other, _ := s.Observe("peer-c", KindNode, "gpu-1", "node-b4", hash)
	if _, err := s.Confirm(other.ID); err != nil {
		t.Fatalf("Confirm(other): %v", err)
	}
	// Different kind: confirmed but must not appear here.
	otherKind, _ := s.Observe("peer-c", KindBilling, "gpu-0", "pool-9", hash)
	if _, err := s.Confirm(otherKind.ID); err != nil {
		t.Fatalf("Confirm(otherKind): %v", err)
	}

	got := s.SharedWith(KindNode, "gpu-0")
	if len(got) != 1 {
		t.Fatalf("SharedWith has %d entries, want 1: %+v", len(got), got)
	}
	if got[0].ID != confirmed.ID {
		t.Fatalf("SharedWith returned %q, want %q", got[0].ID, confirmed.ID)
	}
	_ = pending
}

func TestMatchStoreOnDecisionFires(t *testing.T) {
	s := newTestStore(t)
	var mu sync.Mutex
	var calls []string
	s.OnDecision = func(m Match, action string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, m.ID+":"+action+":"+m.State)
	}

	m, _ := s.Observe("peer-a", KindNode, "gpu-0", "node-b1", testHash)
	if _, err := s.Confirm(m.ID); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if _, err := s.Reject(m.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if _, err := s.Confirm("no-such-id"); err == nil {
		t.Fatalf("Confirm(unknown): expected error")
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		m.ID + ":" + ActionConfirm + ":" + StateConfirmed,
		m.ID + ":" + ActionReject + ":" + StateRejected,
	}
	if len(calls) != len(want) {
		t.Fatalf("OnDecision fired %d times: %v", len(calls), calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("OnDecision call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}
