package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func persistedPair(t *testing.T) (path string, mk func() (*Backoff, *fakeClock)) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "sub", "backoff.json")
	clk := &fakeClock{t: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)}
	return path, func() (*Backoff, *fakeClock) {
		b := NewBackoffPersisted(path)
		b.Clock = clk.Now
		return b, clk
	}
}

func TestBackoffPersist_RoundTripAllKinds(t *testing.T) {
	path, mk := persistedPair(t)
	b, clk := mk()
	until := clk.Now().Add(time.Hour)
	b.Set("claude-code", until, "rate-limit — reset 13:00", BackoffSourceRateLimit)
	b.SetWithReset("pi", until, BackoffReasonNoResetTime, BackoffSourceRateLimit, false)
	b.Set("ollama", until, "paused", BackoffSourceOperator)
	b.SetWithRung("burn", until, "hold", "burn_guard", true, "hold_claude")
	if err := b.SaveErr(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stat %v perm %v", err, fi)
	}

	b2, clk2 := mk()
	got, err := b2.Load(clk2.Now())
	if err != nil || len(got) != 4 {
		t.Fatalf("load: %v %v", got, err)
	}
	want := b.Active()
	have := b2.Active()
	for i := range want {
		if want[i] != have[i] {
			t.Errorf("entry %d: want %+v got %+v", i, want[i], have[i])
		}
	}
	for _, name := range []string{"claude-code", "pi", "ollama", "burn"} {
		if ok, _ := b2.Blocked(name); !ok {
			t.Errorf("%s not blocked after load", name)
		}
	}
	if e := b2.Active()[0]; e.Backend != "burn" || e.Rung != "hold_claude" {
		t.Errorf("rung lost: %+v", e)
	}
}

func TestBackoffPersist_ExpiredDroppedOnLoad(t *testing.T) {
	_, mk := persistedPair(t)
	b, clk := mk()
	b.Set("old", clk.Now().Add(10*time.Minute), "r", BackoffSourceOperator)
	b.Set("new", clk.Now().Add(2*time.Hour), "r", BackoffSourceOperator)
	clk.Advance(time.Hour)
	b2, _ := mk()
	got, err := b2.Load(clk.Now())
	if err != nil || len(got) != 1 || got[0].Backend != "new" {
		t.Fatalf("got %v err %v", got, err)
	}
	if ok, _ := b2.Blocked("old"); ok {
		t.Error("expired entry installed")
	}
}

func TestBackoffPersist_ClearAndClearAllPersisted(t *testing.T) {
	_, mk := persistedPair(t)
	b, clk := mk()
	until := clk.Now().Add(time.Hour)
	b.Set("a", until, "r", BackoffSourceOperator)
	b.Set("b", until, "r", BackoffSourceOperator)
	b.Clear("a")
	b2, _ := mk()
	if got, _ := b2.Load(clk.Now()); len(got) != 1 || got[0].Backend != "b" {
		t.Fatalf("after Clear: %v", got)
	}
	b.ClearAll()
	b3, _ := mk()
	if got, err := b3.Load(clk.Now()); err != nil || len(got) != 0 {
		t.Fatalf("after ClearAll: %v %v", got, err)
	}
}

func TestBackoffPersist_SweepPersisted(t *testing.T) {
	_, mk := persistedPair(t)
	b, clk := mk()
	b.Set("a", clk.Now().Add(time.Minute), "r", BackoffSourceOperator)
	b.Set("b", clk.Now().Add(time.Hour), "r", BackoffSourceOperator)
	clk.Advance(5 * time.Minute)
	b.Sweep(clk.Now())
	b2, _ := mk()
	if got, _ := b2.Load(clk.Now()); len(got) != 1 || got[0].Backend != "b" {
		t.Fatalf("after sweep: %v", got)
	}
}

func TestBackoffPersist_CorruptAndMissing(t *testing.T) {
	path, mk := persistedPair(t)
	b, clk := mk()
	if got, err := b.Load(clk.Now()); err != nil || len(got) != 0 {
		t.Fatalf("missing: %v %v", got, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := b.Load(clk.Now())
	if err == nil || len(got) != 0 || len(b.Active()) != 0 {
		t.Fatalf("corrupt: %v %v", got, err)
	}
	// Registry still usable and overwrites the corrupt file.
	b.Set("x", clk.Now().Add(time.Hour), "r", BackoffSourceOperator)
	b2, _ := mk()
	if got, err := b2.Load(clk.Now()); err != nil || len(got) != 1 {
		t.Fatalf("recover: %v %v", got, err)
	}
}
