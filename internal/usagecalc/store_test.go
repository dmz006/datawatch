package usagecalc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var storeTestNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

// fakeClock is an injected clock the tests can move.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// logRecorder collects store log lines.
type logRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (l *logRecorder) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logRecorder) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lines)
}

func fptr(v float64) *float64 { return &v }
func iptr(v int) *int         { return &v }

func typedPercentAt(at time.Time, p float64) Reading {
	return Reading{At: at, Percent: fptr(p), Source: SourceTyped}
}

func openTestStore(t *testing.T, dir string, clock *fakeClock) (*Store, *logRecorder) {
	t.Helper()
	rec := &logRecorder{}
	s, err := OpenStore(dir, clock.Now, WithLogger(rec.logf))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	return s, rec
}

func TestStoreRestartKeepsReadings(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, dir, clock)

	first := Reading{At: storeTestNow.Add(-48 * time.Hour), Percent: fptr(22.5), SessionPercent: fptr(10), Lanes: iptr(2), Source: SourceTyped}
	second := Reading{At: storeTestNow.Add(-24 * time.Hour), SpendUSD: fptr(41.25), Source: SourceTyped}
	for _, r := range []Reading{first, second} {
		if _, err := s.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := s.SetExtraCredit(30); err != nil {
		t.Fatalf("SetExtraCredit: %v", err)
	}
	if err := s.SetRemainingWork(fptr(17.5)); err != nil {
		t.Fatalf("SetRemainingWork: %v", err)
	}
	before := s.Snapshot()

	reopened, rec := openTestStore(t, dir, clock)
	after := reopened.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("snapshot changed across restart:\nbefore %+v\nafter  %+v", before, after)
	}
	if rec.count() != 0 {
		t.Fatalf("clean reopen logged: %v", rec.lines)
	}
	if got := len(after.Account.Readings); got != 2 {
		t.Fatalf("want 2 readings, got %d", got)
	}
	if after.Account.ExtraCreditPoints != 30 || !after.Account.ExtraCreditUpdatedAt.Equal(storeTestNow) {
		t.Fatalf("extra credit not kept: %+v", after.Account)
	}
	if after.Local.RemainingWork == nil || *after.Local.RemainingWork != 17.5 {
		t.Fatalf("remaining work not kept: %+v", after.Local)
	}
	if after.Version != StoreSchemaVersion {
		t.Fatalf("version = %d", after.Version)
	}
}

func TestStoreFileModeAndAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	s, _ := openTestStore(t, dir, newFakeClock(storeTestNow))
	if _, err := s.Append(typedPercentAt(storeTestNow, 5)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "usagecalc", "usage.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestStoreCorruptFile(t *testing.T) {
	cases := map[string]string{
		"not json":       "{not json",
		"empty":          "",
		"wrong version":  `{"version": 99, "account": {"readings": []}}`,
		"missing schema": `{"account": {"readings": []}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			storeDir := filepath.Join(dir, "usagecalc")
			if err := os.MkdirAll(storeDir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(storeDir, "usage.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			clock := newFakeClock(storeTestNow)
			s, rec := openTestStore(t, dir, clock)

			if rec.count() != 1 {
				t.Fatalf("want one logged error, got %d: %v", rec.count(), rec.lines)
			}
			bad, err := os.ReadFile(path + ".bad")
			if err != nil {
				t.Fatalf("bad file missing: %v", err)
			}
			if string(bad) != content {
				t.Fatalf("bad file changed: %q", bad)
			}
			snap := s.Snapshot()
			if len(snap.Account.Readings) != 0 || snap.Account.ExtraCreditPoints != 0 || snap.Local.RemainingWork != nil {
				t.Fatalf("store not empty: %+v", snap)
			}
			if _, err := s.Append(typedPercentAt(storeTestNow, 1)); err != nil {
				t.Fatalf("Append after recovery: %v", err)
			}
			if rec.count() != 1 {
				t.Fatalf("error logged again: %v", rec.lines)
			}
			if again, err := os.ReadFile(path + ".bad"); err != nil || string(again) != content {
				t.Fatalf("bad file lost after write: %v %q", err, again)
			}
		})
	}
}

func TestStoreCorruptFileKeepsEarlierBadFile(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "usagecalc")
	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(storeDir, "usage.json")
	if err := os.WriteFile(path+".bad", []byte("older bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("newer bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	openTestStore(t, dir, newFakeClock(storeTestNow))
	if got, _ := os.ReadFile(path + ".bad"); string(got) != "older bad" {
		t.Fatalf("earlier bad file overwritten: %q", got)
	}
	if got, _ := os.ReadFile(path + ".bad.1"); string(got) != "newer bad" {
		t.Fatalf("newer bad file not kept: %q", got)
	}
}

func TestStoreOpenRequiresInputs(t *testing.T) {
	if _, err := OpenStore("", time.Now); err == nil {
		t.Fatal("empty data directory accepted")
	}
	if _, err := OpenStore(t.TempDir(), nil); err == nil {
		t.Fatal("nil clock accepted")
	}
}

func TestStoreAppendValidation(t *testing.T) {
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, t.TempDir(), clock)
	at := storeTestNow.Add(-time.Hour)

	bad := map[string]Reading{
		"neither figure":          {At: at},
		"both figures":            {At: at, Percent: fptr(10), SpendUSD: fptr(5)},
		"negative percent":        {At: at, Percent: fptr(-0.1)},
		"percent above limit":     {At: at, Percent: fptr(1000.1)},
		"negative spend":          {At: at, SpendUSD: fptr(-1)},
		"session percent too big": {At: at, Percent: fptr(10), SessionPercent: fptr(2000)},
		"no time":                 {Percent: fptr(10)},
		"six minutes ahead":       {At: storeTestNow.Add(6 * time.Minute), Percent: fptr(10)},
	}
	for name, r := range bad {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := s.Append(r)
			if !errors.Is(err, ErrInvalidReading) {
				t.Fatalf("err = %v, want ErrInvalidReading", err)
			}
		})
	}
	if n := len(s.List(time.Time{})); n != 0 {
		t.Fatalf("rejected readings were stored: %d", n)
	}

	good := map[string]Reading{
		"zero percent":     {At: at.Add(1 * time.Second), Percent: fptr(0)},
		"percent at limit": {At: at.Add(2 * time.Second), Percent: fptr(1000)},
		"zero spend":       {At: at.Add(3 * time.Second), SpendUSD: fptr(0)},
		"five minutes":     {At: storeTestNow.Add(5 * time.Minute), Percent: fptr(10)},
	}
	for name, r := range good {
		t.Run("accepts "+name, func(t *testing.T) {
			if _, err := s.Append(r); err != nil {
				t.Fatalf("Append: %v", err)
			}
		})
	}
}

func TestStoreAppendAssignsIDAndKeepsGivenID(t *testing.T) {
	s, _ := openTestStore(t, t.TempDir(), newFakeClock(storeTestNow))
	a, err := s.Append(typedPercentAt(storeTestNow.Add(-2*time.Hour), 10))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Append(typedPercentAt(storeTestNow.Add(-time.Hour), 20))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || b.ID == "" || a.ID == b.ID {
		t.Fatalf("ids = %q %q", a.ID, b.ID)
	}
	given := typedPercentAt(storeTestNow, 30)
	given.ID = "typed-id"
	c, err := s.Append(given)
	if err != nil || c.ID != "typed-id" {
		t.Fatalf("Append with id: %+v %v", c, err)
	}
}

func TestStoreAppendDoesNotAliasCallerData(t *testing.T) {
	s, _ := openTestStore(t, t.TempDir(), newFakeClock(storeTestNow))
	p := 12.0
	r := Reading{At: storeTestNow, Percent: &p}
	if _, err := s.Append(r); err != nil {
		t.Fatal(err)
	}
	p = 99
	if got := *s.List(time.Time{})[0].Percent; got != 12 {
		t.Fatalf("store shares caller memory: %v", got)
	}
	snap := s.Snapshot()
	*snap.Account.Readings[0].Percent = 77
	if got := *s.List(time.Time{})[0].Percent; got != 12 {
		t.Fatalf("snapshot shares store memory: %v", got)
	}
}

func TestStoreReplaceAtSameTimestamp(t *testing.T) {
	s, _ := openTestStore(t, t.TempDir(), newFakeClock(storeTestNow))
	at := storeTestNow.Add(-3 * time.Hour)
	first, err := s.Append(typedPercentAt(at, 10))
	if err != nil {
		t.Fatal(err)
	}
	// the same instant written in another zone is still the same timestamp
	other := time.FixedZone("x", 5*3600)
	second, err := s.Append(typedPercentAt(at.In(other), 15))
	if err != nil {
		t.Fatal(err)
	}
	got := s.List(time.Time{})
	if len(got) != 1 {
		t.Fatalf("want 1 reading, got %d", len(got))
	}
	if *got[0].Percent != 15 {
		t.Fatalf("percent = %v, want 15", *got[0].Percent)
	}
	if second.ID != first.ID {
		t.Fatalf("replacement should keep the id: %q vs %q", second.ID, first.ID)
	}
	// replacing with a spend reading drops the old percent
	if _, err := s.Append(Reading{At: at, SpendUSD: fptr(3)}); err != nil {
		t.Fatal(err)
	}
	got = s.List(time.Time{})
	if len(got) != 1 || got[0].Percent != nil || got[0].SpendUSD == nil {
		t.Fatalf("replacement kept old figure: %+v", got)
	}
}

func TestStoreListSinceAndOrder(t *testing.T) {
	s, _ := openTestStore(t, t.TempDir(), newFakeClock(storeTestNow))
	for _, h := range []int{5, 1, 3} { // out of order on purpose
		if _, err := s.Append(typedPercentAt(storeTestNow.Add(-time.Duration(h)*time.Hour), float64(h))); err != nil {
			t.Fatal(err)
		}
	}
	all := s.List(time.Time{})
	if len(all) != 3 || *all[0].Percent != 5 || *all[1].Percent != 3 || *all[2].Percent != 1 {
		t.Fatalf("not oldest first: %+v", all)
	}
	since := s.List(storeTestNow.Add(-3 * time.Hour))
	if len(since) != 2 || *since[0].Percent != 3 {
		t.Fatalf("since is inclusive: %+v", since)
	}
	if got := s.List(storeTestNow); len(got) != 0 {
		t.Fatalf("want none, got %+v", got)
	}
}

func TestStoreDelete(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, dir, clock)
	keep, _ := s.Append(typedPercentAt(storeTestNow.Add(-2*time.Hour), 10))
	typo, _ := s.Append(typedPercentAt(storeTestNow.Add(-time.Hour), 99))

	if err := s.Delete(typo.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := s.List(time.Time{})
	if len(got) != 1 || got[0].ID != keep.ID {
		t.Fatalf("after delete: %+v", got)
	}
	if err := s.Delete(typo.ID); !errors.Is(err, ErrReadingNotFound) {
		t.Fatalf("second delete err = %v", err)
	}
	if err := s.Delete(""); !errors.Is(err, ErrReadingNotFound) {
		t.Fatalf("empty id err = %v", err)
	}
	reopened, _ := openTestStore(t, dir, clock)
	if n := len(reopened.List(time.Time{})); n != 1 {
		t.Fatalf("delete not persisted: %d", n)
	}
}

func TestStorePrunesOldReadingsOnWrite(t *testing.T) {
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, t.TempDir(), clock)
	old, err := s.Append(typedPercentAt(storeTestNow.Add(-ReadingRetention+time.Hour), 10))
	if err != nil {
		t.Fatal(err)
	}
	recent, err := s.Append(typedPercentAt(storeTestNow.Add(-time.Hour), 20))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(s.List(time.Time{})); n != 2 {
		t.Fatalf("nothing should be pruned yet: %d", n)
	}

	// two days later the first reading is past six weeks; any write prunes it
	clock.Advance(48 * time.Hour)
	if n := len(s.List(time.Time{})); n != 2 {
		t.Fatalf("reads must not prune: %d", n)
	}
	if err := s.SetExtraCredit(0); err != nil {
		t.Fatal(err)
	}
	got := s.List(time.Time{})
	if len(got) != 1 || got[0].ID != recent.ID {
		t.Fatalf("after prune: %+v (old %s)", got, old.ID)
	}

	// a reading exactly at the cutoff stays
	edge := clock.Now().Add(-ReadingRetention)
	if _, err := s.Append(typedPercentAt(edge, 5)); err != nil {
		t.Fatal(err)
	}
	if n := len(s.List(time.Time{})); n != 2 {
		t.Fatalf("reading at the cutoff was pruned: %d", n)
	}
}

func TestStoreExtraCreditAndRemainingWork(t *testing.T) {
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, t.TempDir(), clock)

	for _, bad := range []float64{-1, nanValue(), infValue()} {
		if err := s.SetExtraCredit(bad); !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("SetExtraCredit(%v) err = %v", bad, err)
		}
	}
	if err := s.SetExtraCredit(25); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Hour)
	if err := s.SetExtraCredit(0); err != nil {
		t.Fatalf("zero extra credit: %v", err)
	}
	acct := s.Snapshot().Account
	if acct.ExtraCreditPoints != 0 || !acct.ExtraCreditUpdatedAt.Equal(clock.Now()) {
		t.Fatalf("extra credit = %+v", acct)
	}

	for _, bad := range []float64{-1, nanValue(), infValue()} {
		if err := s.SetRemainingWork(&bad); !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("SetRemainingWork(%v) err = %v", bad, err)
		}
	}
	w := 8.0
	if err := s.SetRemainingWork(&w); err != nil {
		t.Fatal(err)
	}
	w = 99 // must not leak into the store
	if got := s.Snapshot().Local.RemainingWork; got == nil || *got != 8 {
		t.Fatalf("remaining work = %v", got)
	}
	if err := s.SetRemainingWork(nil); err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot().Local.RemainingWork; got != nil {
		t.Fatalf("remaining work not cleared: %v", *got)
	}
}

func TestStoreFailedWriteLeavesStateUnchanged(t *testing.T) {
	dir := t.TempDir()
	s, _ := openTestStore(t, dir, newFakeClock(storeTestNow))
	if _, err := s.Append(typedPercentAt(storeTestNow.Add(-time.Hour), 10)); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()

	storeDir := filepath.Join(dir, "usagecalc")
	if err := os.Chmod(storeDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(storeDir, 0o700) }()
	if f, err := os.CreateTemp(storeDir, "probe"); err == nil {
		_ = f.Close()
		t.Skip("directory permissions are not enforced here")
	}

	if _, err := s.Append(typedPercentAt(storeTestNow, 20)); err == nil {
		t.Fatal("Append succeeded with an unwritable directory")
	}
	if err := s.SetExtraCredit(5); err == nil {
		t.Fatal("SetExtraCredit succeeded with an unwritable directory")
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatalf("failed write changed memory state: %+v", s.Snapshot())
	}
}

func TestStoreConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, dir, clock)

	const workers, perWorker = 8, 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				at := storeTestNow.Add(-time.Duration(w*perWorker+i+1) * time.Minute)
				if _, err := s.Append(typedPercentAt(at, float64(i))); err != nil {
					errs <- err
				}
				_ = s.List(time.Time{})
				_ = s.Snapshot()
			}
		}(w)
	}
	// concurrent setters and a same-timestamp writer share the lock too
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < perWorker; i++ {
			if err := s.SetExtraCredit(float64(i)); err != nil {
				errs <- err
			}
			if err := s.SetRemainingWork(fptr(float64(i))); err != nil {
				errs <- err
			}
		}
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent op failed: %v", err)
	}

	got := s.List(time.Time{})
	if len(got) != workers*perWorker {
		t.Fatalf("want %d readings, got %d", workers*perWorker, len(got))
	}
	seen := map[string]bool{}
	for i, r := range got {
		if seen[r.ID] {
			t.Fatalf("duplicate id %s", r.ID)
		}
		seen[r.ID] = true
		if i > 0 && got[i-1].At.After(r.At) {
			t.Fatalf("readings out of order at %d", i)
		}
	}
	reopened, _ := openTestStore(t, dir, clock)
	if !reflect.DeepEqual(s.Snapshot(), reopened.Snapshot()) {
		t.Fatal("file does not match memory after concurrent writes")
	}
}

func TestStoreConcurrentSameTimestamp(t *testing.T) {
	s, _ := openTestStore(t, t.TempDir(), newFakeClock(storeTestNow))
	at := storeTestNow.Add(-time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.Append(typedPercentAt(at, float64(i))); err != nil {
				t.Errorf("Append: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if n := len(s.List(time.Time{})); n != 1 {
		t.Fatalf("same timestamp must leave one reading, got %d", n)
	}
}

// TestStoreAcceptsRequestLanes: validateReading no longer rejects a lanes value, older
// callers still send one; a bare count with no source is stored as typed.
func TestStoreAcceptsRequestLanes(t *testing.T) {
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, t.TempDir(), clock)
	got, err := s.Append(Reading{At: storeTestNow.Add(-time.Hour), Percent: fptr(10), Lanes: iptr(-1), Source: SourceTyped})
	if err != nil {
		t.Fatalf("Append with lanes: %v", err)
	}
	if got.Lanes == nil || *got.Lanes != -1 || got.LanesSource != LanesTyped {
		t.Fatalf("stored lanes = %v source %q, want the value kept with source typed", got.Lanes, got.LanesSource)
	}
}

// TestStoreLegacyTypedLanesLoad: a reading stored before the stamp, with a typed lane count
// and no lanes_source, loads with source typed and its value unchanged.
func TestStoreLegacyTypedLanesLoad(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, dir, clock)
	at := storeTestNow.Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	legacy := `{"version":` + strconv.Itoa(StoreSchemaVersion) + `,"account":{"readings":[` +
		`{"id":"old1","at":"` + at + `","percent":40,"lanes":2,"source":"typed"},` +
		`{"id":"old2","at":"` + storeTestNow.Add(-time.Hour).UTC().Format(time.RFC3339) + `","percent":45,"source":"typed"}]}}`
	if err := os.WriteFile(s.FilePath(), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	reopened, rec := openTestStore(t, dir, clock)
	if rec.count() != 0 {
		t.Fatalf("legacy file logged: %v", rec.lines)
	}
	got := reopened.Snapshot().Account.Readings
	if len(got) != 2 {
		t.Fatalf("readings = %+v", got)
	}
	if got[0].Lanes == nil || *got[0].Lanes != 2 || got[0].LanesSource != LanesTyped {
		t.Fatalf("legacy reading lanes = %v source %q, want 2 typed", got[0].Lanes, got[0].LanesSource)
	}
	if got[1].Lanes != nil || got[1].LanesSource != "" {
		t.Fatalf("reading without lanes = %v source %q, want untouched", got[1].Lanes, got[1].LanesSource)
	}
}
