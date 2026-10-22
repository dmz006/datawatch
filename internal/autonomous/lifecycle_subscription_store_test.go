// BL427 — Subscription store tests (Phase 3).
package autonomous

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

func testSubscriptionKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand key: %v", err)
	}
	return key
}

func testSubscriptionBase() Subscription {
	return Subscription{
		Owner:  "test-owner",
		Filter: LifecycleFilter{Kinds: []string{PRDStatusKind(PRDCompleted), KindTaskSessionStarted}},
		Sink:   SubscriptionSink{Type: SinkSession, Target: "ops-console"},
	}
}

func testSubscriptionStore(t *testing.T) (*SubscriptionStore, string, []byte) {
	t.Helper()
	key := testSubscriptionKey(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "lifecycle_subscriptions.json")
	s, err := NewSubscriptionStore(path, key)
	if err != nil {
		t.Fatalf("NewSubscriptionStore: %v", err)
	}
	return s, path, key
}

func TestSubscriptionStoreRoundTrip(t *testing.T) {
	s, path, key := testSubscriptionStore(t)

	expiry := time.Now().Add(24 * time.Hour)
	injected := &SubscriptionAction{Type: SinkActionSendInput, Text: "override timeout=300"}

	subs := map[string]Subscription{}
	base := testSubscriptionBase()
	a, err := s.Add(base)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if a.ID == "" {
		t.Fatal("Add did not assign an ID")
	}
	if a.CreatedAt.IsZero() {
		t.Fatal("Add did not set CreatedAt")
	}
	subs[a.ID] = a

	b := testSubscriptionBase()
	b.Filter = LifecycleFilter{Level: LevelPRD, PRDID: "prd-1", AutomationID: "auto-1"}
	b.Sink = SubscriptionSink{Type: SinkTopic, Target: "audience-devs"}
	b.Coalesce = true
	b.ExpiresAt = &expiry
	bb, err := s.Add(b)
	if err != nil {
		t.Fatalf("Add(b): %v", err)
	}
	subs[bb.ID] = bb

	c := testSubscriptionBase()
	c.Sink = SubscriptionSink{Type: SinkSession, Target: "ops-console"}
	c.Action = injected
	c.OneShot = true
	cc, err := s.Add(c)
	if err != nil {
		t.Fatalf("Add(c): %v", err)
	}
	subs[cc.ID] = cc

	// Get + List.
	if got, ok := s.Get(a.ID); !ok || got.Sink.Target != "ops-console" {
		t.Fatalf("Get(a) = %+v, %v", got, ok)
	}
	if all := s.List(""); len(all) != 3 {
		t.Fatalf("List() = %d subs, want 3", len(all))
	}
	if mine := s.List("test-owner"); len(mine) != 3 {
		t.Fatalf("List(owner) = %d subs, want 3", len(mine))
	}
	if other := s.List("someone-else"); len(other) != 0 {
		t.Fatalf("List(other owner) = %d subs, want 0", len(other))
	}

	// Reopen: everything must survive.
	s2, err := NewSubscriptionStore(path, key)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !subscriptionsEqualList(s.List(""), s2.List("")) {
		t.Fatalf("round trip mismatch:\nfirst:  %v\nsecond: %v", s.List(""), s2.List(""))
	}
	if got, ok := s2.Get(cc.ID); !ok {
		t.Fatalf("reopened store lost %s", cc.ID)
	} else {
		if got.Action == nil || got.Action.Text != "override timeout=300" || !got.OneShot {
			t.Fatalf("reopened action sub = %+v", got)
		}
	}

	// Delete persists.
	if err := s2.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s2.Delete("no-such-id"); err == nil {
		t.Fatal("Delete(missing) should error")
	}
	s3, err := NewSubscriptionStore(path, key)
	if err != nil {
		t.Fatalf("reopen2: %v", err)
	}
	if _, ok := s3.Get(a.ID); ok {
		t.Fatal("deleted subscription survived reload")
	}
	if got := s3.List(""); len(got) != 2 {
		t.Fatalf("after delete List = %d subs, want 2", len(got))
	}
}

func subscriptionsEqualList(a, b []Subscription) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Owner != b[i].Owner {
			return false
		}
		fa, fb := a[i].Filter, b[i].Filter
		if fa.Level != fb.Level || fa.PRDID != fb.PRDID || fa.TaskID != fb.TaskID || fa.AutomationID != fb.AutomationID {
			return false
		}
		if len(fa.Kinds) != len(fb.Kinds) {
			return false
		}
		for k := range fa.Kinds {
			if fa.Kinds[k] != fb.Kinds[k] {
				return false
			}
		}
		if a[i].Sink != b[i].Sink {
			return false
		}
		if (a[i].Action == nil) != (b[i].Action == nil) {
			return false
		}
		if a[i].Action != nil && *a[i].Action != *b[i].Action {
			return false
		}
		if a[i].OneShot != b[i].OneShot || a[i].Coalesce != b[i].Coalesce {
			return false
		}
		ae, be := a[i].ExpiresAt, b[i].ExpiresAt
		if (ae == nil) != (be == nil) {
			return false
		}
		if ae != nil && !ae.Equal(*be) {
			return false
		}
	}
	return true
}

func TestSubscriptionStoreEncryptedNotPlaintext(t *testing.T) {
	s, path, key := testSubscriptionStore(t)
	marker := "ULTRA-SECRET-SINK-TARGET-xyz"
	sub := testSubscriptionBase()
	sub.Sink.Target = marker
	if _, err := s.Add(sub); err != nil {
		t.Fatalf("Add: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read encrypted file: %v", err)
	}
	if strings.Contains(string(raw), marker) {
		t.Fatalf("file at %s contains plaintext sink target %q", path, marker)
	}
	if strings.Contains(string(raw), "ops-console") {
		t.Fatal("file contains plaintext from the fixture")
	}
	if !secfile.IsEncrypted(raw) {
		t.Fatal("expected secfile.EncryptedHeader on the stored file")
	}
	// The daemon can still read it back with the key.
	plain, err := secfile.ReadFile(path, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !strings.Contains(string(plain), marker) {
		t.Fatal("decrypted content lost the sink target")
	}
}

func TestSubscriptionAddValidation(t *testing.T) {
	s, _, _ := testSubscriptionStore(t)
	valid := testSubscriptionBase()
	if err := ValidateSubscription(valid, time.Now()); err != nil {
		t.Fatalf("fixture should be valid: %v", err)
	}
	if _, err := s.Add(valid); err != nil {
		t.Fatalf("Add(valid): %v", err)
	}

	longText := strings.Repeat("x", SubscriptionActionTextLimit+1)
	past := time.Now().Add(-time.Minute)
	comm := SinkComm
	inject := func(text string) *SubscriptionAction {
		return &SubscriptionAction{Type: SinkActionSendInput, Text: text}
	}

	cases := []struct {
		name    string
		mutate  func(*Subscription)
		wantSub string
	}{
		{"empty kinds and level", func(su *Subscription) { su.Filter = LifecycleFilter{PRDID: "x"} }, "kinds or level"},
		{"bad level", func(su *Subscription) { su.Filter.Level = "story" }, "level must be"},
		{"empty kind entry", func(su *Subscription) { su.Filter.Kinds = []string{""} }, "kind 0 is empty"},
		{"unknown kind", func(su *Subscription) { su.Filter.Kinds = []string{"prd.finished_now"} }, "not a known event kind"},
		{"unknown level kind status", func(su *Subscription) { su.Filter.Kinds = []string{"task.frobnicated"} }, "not a known event kind"},
		{"missing sink type", func(su *Subscription) { su.Sink = SubscriptionSink{Target: "x"} }, "sink type is required"},
		{"webhook sink rejected", func(su *Subscription) { su.Sink = SubscriptionSink{Type: "webhook", Target: "https://x.example"} }, "not available in this phase"},
		{"mcp_queue empty target", func(su *Subscription) { su.Sink = SubscriptionSink{Type: SinkMCPQueue, Target: "  "} }, "sink target is required"},
		{"mcp_queue bad chars", func(su *Subscription) { su.Sink = SubscriptionSink{Type: SinkMCPQueue, Target: "my queue!"} }, "may contain only"},
		{"mcp_queue name too long", func(su *Subscription) {
			su.Sink = SubscriptionSink{Type: SinkMCPQueue, Target: strings.Repeat("q", 65)}
		}, "limit is 64"},
		{"mcp_queue with action", func(su *Subscription) {
			su.Sink = SubscriptionSink{Type: SinkMCPQueue, Target: "me"}
			su.Action = inject("hi")
		}, "session sink"},
		{"unknown sink type", func(su *Subscription) { su.Sink = SubscriptionSink{Type: "carrier-pigeon", Target: "x"} }, "unknown sink type"},
		{"empty sink target", func(su *Subscription) { su.Sink = SubscriptionSink{Type: SinkComm} }, "sink target is required"},
		{"action on comm sink", func(su *Subscription) { su.Sink.Type = comm; su.Action = inject("hi") }, "session sink"},
		{"action unknown type", func(su *Subscription) { su.Action = &SubscriptionAction{Type: "shell", Text: "ls"} }, "unknown action type"},
		{"action empty text", func(su *Subscription) { su.Action = inject("   ") }, "requires non-empty text"},
		{"action text over cap", func(su *Subscription) { su.Action = inject(longText) }, "limit is"},
		{"expires in the past", func(su *Subscription) { su.ExpiresAt = &past }, "in the future"},
		{"valid mcp_queue sink", func(su *Subscription) { su.Sink = SubscriptionSink{Type: SinkMCPQueue, Target: "agent-1.ops:prod_x"} }, ""},
		{"valid topic sink", func(su *Subscription) { su.Sink = SubscriptionSink{Type: SinkTopic, Target: "ops-audience"} }, ""},
	}
	for _, tc := range cases {
		sub := testSubscriptionBase()
		if tc.mutate != nil {
			tc.mutate(&sub)
		}
		err := ValidateSubscription(sub, time.Now())
		if tc.wantSub == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v (+ %v)", tc.name, err, sub)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: expected error containing %q, got nil", tc.name, tc.wantSub)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("%s: error %q does not contain %q", tc.name, err, tc.wantSub)
		}
		// Invalid subscriptions must also be rejected by Add.
		if _, aerr := s.Add(sub); aerr == nil {
			t.Errorf("%s: Add accepted invalid subscription", tc.name)
		}
	}
}

func TestSubscriptionStoreExpiry(t *testing.T) {
	s, _, _ := testSubscriptionStore(t)

	short := testSubscriptionBase()
	soon := time.Now().Add(time.Minute)
	short.ExpiresAt = &soon
	short.ID = "short-lived"
	if _, err := s.Add(short); err != nil {
		t.Fatalf("Add(short): %v", err)
	}

	long := testSubscriptionBase()
	long.ExpiresAt = nil
	long.ID = "no-expiry"
	if _, err := s.Add(long); err != nil {
		t.Fatalf("Add(long): %v", err)
	}

	distant := testSubscriptionBase()
	far := time.Now().Add(24 * time.Hour)
	distant.ExpiresAt = &far
	distant.ID = "distant"
	if _, err := s.Add(distant); err != nil {
		t.Fatalf("Add(distant): %v", err)
	}

	// Nothing expired yet.
	removed, err := s.DeleteExpired(time.Now())
	if err != nil {
		t.Fatalf("DeleteExpired(now): %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("DeleteExpired(now) removed %v, want none", removed)
	}

	// Past the short subscription's deadline.
	removed, err = s.DeleteExpired(soon.Add(time.Second))
	if err != nil {
		t.Fatalf("DeleteExpired(later): %v", err)
	}
	if len(removed) != 1 || removed[0] != "short-lived" {
		t.Fatalf("DeleteExpired(later) = %v, want [short-lived]", removed)
	}
	if _, ok := s.Get("short-lived"); ok {
		t.Fatal("expired subscription still present after DeleteExpired")
	}
	if _, ok := s.Get("no-expiry"); !ok {
		t.Fatal("non-expiring subscription removed")
	}
	if _, ok := s.Get("distant"); !ok {
		t.Fatal("not-yet-expired subscription removed")
	}
	// Idempotent: second sweep removes only the distant one; the
	// no-expiry subscription never expires.
	removed, err = s.DeleteExpired(time.Now().Add(48 * time.Hour))
	if err != nil {
		t.Fatalf("DeleteExpired(final): %v", err)
	}
	if len(removed) != 1 || removed[0] != "distant" {
		t.Fatalf("final sweep = %v, want [distant]", removed)
	}
}

// The store is shared state: concurrent Adds and Deletes must not race
// or lose the file. Run with -race.
func TestSubscriptionStoreConcurrentAddDelete(t *testing.T) {
	s, _, _ := testSubscriptionStore(t)
	var wg sync.WaitGroup
	const workers = 8
	const perWorker = 25
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				sub := testSubscriptionBase()
				sub.Owner = "worker-9"
				sub.Filter.Kinds = []string{PRDStatusKind(PRDFailed)}
				if id, err := s.Add(sub); err == nil {
					if i%2 == 0 {
						if err := s.Delete(id.ID); err != nil {
							t.Errorf("w%d i%d: Delete: %v", w, i, err)
							return
						}
					}
				} else {
					t.Errorf("w%d i%d: Add: %v", w, i, err)
					return
				}
				if _, ierr := s.DeleteExpired(time.Now()); ierr != nil {
					t.Errorf("w%d i%d: DeleteExpired: %v", w, i, ierr)
					return
				}
				_ = s.List("worker-9")
				_ = s.List("")
			}
		}(w)
	}
	wg.Wait()
	// Even-index iterations (i%2==0, ceil(perWorker/2) of them) delete
	// their subscription; the rest survive: perWorker/2 per worker.
	want := workers * (perWorker / 2)
	if got := len(s.List("")); got != want {
		t.Fatalf("final count = %d, want %d", got, want)
	}
}

func TestSubscriptionStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	key := testSubscriptionKey(t)
	path := filepath.Join(dir, "lifecycle_subscriptions.json")

	// Encrypted garbage (valid secfile envelope, invalid JSON inside)
	// must fail to load — not start as an empty store.
	if err := secfile.WriteFile(path, []byte("this is definitely not json"), 0o600, key); err != nil {
		t.Fatalf("setup encrypted file: %v", err)
	}
	if _, err := NewSubscriptionStore(path, key); err == nil {
		t.Fatal("NewSubscriptionStore over corrupt encrypted file should error")
	}

	// Plaintext garbage with a key set must also fail (secfile returns
	// it as-is; JSON parsing then fails).
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("setup plaintext file: %v", err)
	}
	if _, err := NewSubscriptionStore(path, key); err == nil {
		t.Fatal("NewSubscriptionStore over corrupt plaintext file should error")
	}
}

func TestSubscriptionStoreMissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	key := testSubscriptionKey(t)
	path := filepath.Join(dir, "does-not-exet.json")
	s, err := NewSubscriptionStore(path, key)
	if err != nil {
		t.Fatalf("NewSubscriptionStore(missing file) should be idempotent, got %v", err)
	}
	if got := s.List(""); len(got) != 0 {
		t.Fatalf("fresh store List = %d, want 0", len(got))
	}
}

func TestSubscriptionStoreChangedCallback(t *testing.T) {
	s, _, _ := testSubscriptionStore(t)
	var mu sync.Mutex
	fires := 0
	s.OnChanged(func() {
		mu.Lock()
		fires++
		mu.Unlock()
		// The callback must be callable without deadlocking even when
		// it re-enters the store.
		_ = s.List("")
	})
	if _, err := s.Add(testSubscriptionBase()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	mu.Lock()
	if fires != 1 {
		t.Fatalf("changed callback fired %d times after Add, want 1", fires)
	}
	mu.Unlock()
	if err := s.Delete("no-such-id"); err == nil {
		t.Fatal("Delete(missing) should error without firing the callback")
	}
	mu.Lock()
	if fires != 1 {
		t.Fatalf("failed Delete fired the callback (fires=%d)", fires)
	}
	mu.Unlock()

	all := s.List("")
	if len(all) == 0 {
		t.Fatal("no subscriptions to delete")
	}
	if err := s.Delete(all[0].ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	mu.Lock()
	if fires != 2 {
		t.Fatalf("changed callback fired %d times after Delete, want 2", fires)
	}
	mu.Unlock()
}
