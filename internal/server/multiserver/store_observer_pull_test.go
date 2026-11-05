package multiserver

import (
	"testing"
)

func TestObserverPullConfig_EntryDefaultsOff(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Add(&Entry{Name: "peer-a", URL: "https://peer-a"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	e, ok := s.Get("peer-a")
	if !ok {
		t.Fatal("Get: entry not found")
	}
	if e.ObserverPull {
		t.Errorf("ObserverPull = %v, want false (default off)", e.ObserverPull)
	}
	if e.ObserverPullIntervalS != 0 {
		t.Errorf("ObserverPullIntervalS = %d, want 0 (0 = use global default)", e.ObserverPullIntervalS)
	}
	if e.ObserverPullTimeoutS != 0 {
		t.Errorf("ObserverPullTimeoutS = %d, want 0 (0 = use global default)", e.ObserverPullTimeoutS)
	}
}

func TestObserverPullConfig_PersistAndReload(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	want := &Entry{
		Name:                  "peer-pull",
		URL:                   "https://peer-pull.example",
		Token:                 "sekrit-token-value",
		Enabled:               true,
		Federated:             true,
		ObserverPull:          true,
		ObserverPullIntervalS: 15,
		ObserverPullTimeoutS:  3,
	}
	if err := s.Add(want); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := s.Get("peer-pull")
	if !ok {
		t.Fatal("Get: entry not found")
	}
	if got.ObserverPull != want.ObserverPull {
		t.Errorf("ObserverPull = %v, want %v", got.ObserverPull, want.ObserverPull)
	}
	if got.ObserverPullIntervalS != want.ObserverPullIntervalS {
		t.Errorf("ObserverPullIntervalS = %d, want %d", got.ObserverPullIntervalS, want.ObserverPullIntervalS)
	}
	if got.ObserverPullTimeoutS != want.ObserverPullTimeoutS {
		t.Errorf("ObserverPullTimeoutS = %d, want %d", got.ObserverPullTimeoutS, want.ObserverPullTimeoutS)
	}

	// Reload from the same data dir (the persist/load path) and confirm
	// the observer-pull fields survive.
	s2, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	again, ok := s2.Get("peer-pull")
	if !ok {
		t.Fatal("Get (reload): entry not found — persist path dropped it")
	}
	if !again.ObserverPull {
		t.Errorf("after reload: ObserverPull = %v, want true", again.ObserverPull)
	}
	if again.ObserverPullIntervalS != 15 {
		t.Errorf("after reload: ObserverPullIntervalS = %d, want 15", again.ObserverPullIntervalS)
	}
	if again.ObserverPullTimeoutS != 3 {
		t.Errorf("after reload: ObserverPullTimeoutS = %d, want 3", again.ObserverPullTimeoutS)
	}

	// Update must also carry the fields through persist.
	upd := *again
	upd.ObserverPullIntervalS = 45
	upd.ObserverPullTimeoutS = 8
	if err := s2.Update("peer-pull", &upd); err != nil {
		t.Fatalf("Update: %v", err)
	}
	s3, err := NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore (reload after update): %v", err)
	}
	updated, ok := s3.Get("peer-pull")
	if !ok {
		t.Fatal("Get (after update): entry not found")
	}
	if updated.ObserverPullIntervalS != 45 || updated.ObserverPullTimeoutS != 8 {
		t.Errorf("after update+reload: got interval=%d timeout=%d, want 45/8",
			updated.ObserverPullIntervalS, updated.ObserverPullTimeoutS)
	}
}

func TestObserverPullConfig_TokenStaysRedacted(t *testing.T) {
	e := &Entry{
		Name:                  "peer-redact",
		URL:                   "https://peer-redact.example",
		Token:                 "super-secret-token",
		ObserverPull:          true,
		ObserverPullIntervalS: 15,
		ObserverPullTimeoutS:  3,
	}
	r := e.Redacted()
	if r.Token != "" {
		t.Errorf("Redacted().Token = %q, want empty (token must stay hidden)", r.Token)
	}
	if !r.TokenPresent {
		t.Errorf("Redacted().TokenPresent = %v, want true", r.TokenPresent)
	}
	if r.TokenPrefix != "supe" {
		t.Errorf("Redacted().TokenPrefix = %q, want first 4 chars %q", r.TokenPrefix, "supe")
	}
	// The observer-pull fields are not sensitive and must survive.
	if !r.ObserverPull {
		t.Errorf("Redacted().ObserverPull = %v, want true", r.ObserverPull)
	}
	if r.ObserverPullIntervalS != 15 {
		t.Errorf("Redacted().ObserverPullIntervalS = %d, want 15", r.ObserverPullIntervalS)
	}
	if r.ObserverPullTimeoutS != 3 {
		t.Errorf("Redacted().ObserverPullTimeoutS = %d, want 3", r.ObserverPullTimeoutS)
	}

	// The original entry is untouched by Redacted().
	if e.Token != "super-secret-token" {
		t.Errorf("original Token mutated by Redacted(): %q", e.Token)
	}

	// List-level redaction applies the same guarantee.
	list := RedactedList([]*Entry{e})
	if list[0].Token != "" {
		t.Errorf("RedactedList()[0].Token = %q, want empty", list[0].Token)
	}
	if !list[0].ObserverPull {
		t.Errorf("RedactedList()[0].ObserverPull = %v, want true", list[0].ObserverPull)
	}
}
