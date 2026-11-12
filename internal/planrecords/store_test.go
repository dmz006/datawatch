package planrecords

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type logSink struct {
	mu   sync.Mutex
	msgs []string
}

func (l *logSink) logf(f string, a ...any) {
	l.mu.Lock()
	l.msgs = append(l.msgs, fmt.Sprintf(f, a...))
	l.mu.Unlock()
}

type auditEv struct {
	action, subject string
	details         map[string]any
}

func newTestStore(t *testing.T, sealer Sealer) (*Store, string, *[]auditEv, *logSink) {
	t.Helper()
	dir := t.TempDir()
	var evs []auditEv
	var mu sync.Mutex
	ls := &logSink{}
	tick := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	s, err := Open(dir, sealer, Options{
		Logf: ls.logf,
		Audit: func(a, sub string, d map[string]any) {
			mu.Lock()
			evs = append(evs, auditEv{a, sub, d})
			mu.Unlock()
		},
		Now: func() time.Time { mu.Lock(); defer mu.Unlock(); tick = tick.Add(time.Minute); return tick },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, dir, &evs, ls
}

func row(title string) Record {
	return Record{Kind: KindRegistryRow, Project: "datawatch", Title: title}
}

func mustCreate(t *testing.T, s *Store, r Record) Record {
	t.Helper()
	out, err := s.Create(r)
	if err != nil {
		t.Fatalf("create %q: %v", r.Title, err)
	}
	return out
}

func TestCreateGetRoundTrip(t *testing.T) {
	s, dir, evs, _ := newTestStore(t, nil)
	in := row("Ship the registry")
	in.Body, in.LegacyNumber, in.LegacySource = "body text", 42, "docs/plans/README.md#row-42"
	in.Links = []Link{{"doc", "docs/x.md"}}
	in.ShippedBy, in.ShippedVersion = "4bec0001", "v9.1.0"
	in.ID, in.Actor = "", "operator"
	got := mustCreate(t, s, in)
	if got.UID == "" || got.ID != "ship-the-registry" || got.State != StateOpen || got.CreatedAt == "" || got.CreatedAt != got.UpdatedAt || got.ClosedAt != "" {
		t.Fatalf("bad create result %+v", got)
	}
	if got.Actor != "" {
		t.Fatal("actor must not be returned or stored")
	}
	for _, ref := range []string{got.ID, got.UID, "row 42", "ship the registry", "  SHIP THE REGISTRY ", "ship the"} {
		r, err := s.Get(ref)
		if err != nil || r.UID != got.UID {
			t.Fatalf("Get(%q) = %v, %v", ref, r.ID, err)
		}
	}
	// Reopen from disk: same record, with all fields.
	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s2.Get(got.UID)
	if err != nil || r2.Body != "body text" || r2.LegacyNumber != 42 || r2.Links[0].Ref != "docs/x.md" || r2.ShippedVersion != "v9.1.0" {
		t.Fatalf("reload mismatch %+v %v", r2, err)
	}
	if s2.Len() != 1 {
		t.Fatal("len")
	}
	if len(*evs) != 1 || (*evs)[0].action != AuditCreated || (*evs)[0].subject != "Ship the registry" || (*evs)[0].details["actor"] != "operator" {
		t.Fatalf("audit %+v", *evs)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "plan-records", "registry_row", got.UID+".json")); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Fatal("file missing or wrong mode")
	}
}

func TestCreateDefaultsAndClosedAt(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	f := mustCreate(t, s, Record{Kind: KindFinding, Project: "datawatch", Title: "Crash on start", State: StateFixed})
	if f.ClosedAt == "" {
		t.Fatal("a finding created closed needs closed_at")
	}
	if (*evs)[0].details["actor"] != "system" {
		t.Fatal("default actor")
	}
	g := mustCreate(t, s, Record{Kind: KindFinding, Project: "datawatch", Title: "!!!"})
	if g.ID != "finding" {
		t.Fatalf("symbol-only title id %q", g.ID)
	}
	h := mustCreate(t, s, Record{Kind: KindRegistryRow, Project: "datawatch", Title: "???", Aliases: []string{"???", "old", "OLD", " "}})
	if h.ID != "row" || len(h.Aliases) != 1 || h.Aliases[0] != "old" {
		t.Fatalf("%+v", h)
	}
}

func TestCreateValidationAndIDs(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	var inv *ErrInvalid
	if _, err := s.Create(Record{Kind: KindRegistryRow, Project: "datawatch"}); !errors.As(err, &inv) {
		t.Fatalf("want invalid, got %v", err)
	}
	a := mustCreate(t, s, row("Same Slug"))
	b := mustCreate(t, s, Record{Kind: KindRegistryRow, Project: "other", Title: "Same Slug"})
	if a.ID != "same-slug" || b.ID == a.ID || !strings.HasPrefix(b.ID, "same-slug-") {
		t.Fatalf("ids %q %q", a.ID, b.ID)
	}
	c := mustCreate(t, s, Record{Kind: KindRegistryRow, Project: "third", Title: "Same Slug"})
	if c.ID == a.ID || c.ID == b.ID {
		t.Fatal("ids must be unique across the installation")
	}
	in := row("Explicit")
	in.ID = "my-id"
	if got := mustCreate(t, s, in); got.ID != "my-id" {
		t.Fatalf("explicit id lost: %s", got.ID)
	}
	in2 := row("Explicit Two")
	in2.ID = "my-id"
	if _, err := s.Create(in2); !errors.As(err, &inv) {
		t.Fatalf("duplicate explicit id: %v", err)
	}
}

func TestTitleUniquePerProjectReuseAcrossProjects(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	first := mustCreate(t, s, row("Registry Move"))
	_, err := s.Create(row("  registry   MOVE "))
	var tc *ErrTitleConflict
	if !errors.As(err, &tc) || tc.Existing.UID != first.UID || !strings.Contains(tc.Error(), first.ID) {
		t.Fatalf("want ErrTitleConflict naming the existing record, got %v", err)
	}
	// finding in the same project shares the title namespace
	if _, err := s.Create(Record{Kind: KindFinding, Project: "datawatch", Title: "Registry Move"}); !errors.As(err, &tc) {
		t.Fatalf("title space is per project, not per kind: %v", err)
	}
	if _, err := s.Create(Record{Kind: KindRegistryRow, Project: "datawatch-app", Title: "Registry Move"}); err != nil {
		t.Fatalf("reuse across projects must work: %v", err)
	}
}

func TestLegacyNumberConflict(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := row("One")
	a.LegacyNumber = 5
	mustCreate(t, s, a)
	b := row("Two")
	b.LegacyNumber = 5
	var lc *ErrLegacyNumberConflict
	if _, err := s.Create(b); !errors.As(err, &lc) || lc.Number != 5 || !strings.Contains(lc.Error(), "row 5") {
		t.Fatalf("got %v", err)
	}
	b.Project = "other"
	if _, err := s.Create(b); err != nil {
		t.Fatalf("same number in another project: %v", err)
	}
}

func TestUpdateRenameAliasAndConflict(t *testing.T) {
	s, _, evs, _ := newTestStore(t, nil)
	r := mustCreate(t, s, row("Old Name"))
	other := mustCreate(t, s, row("Taken"))
	newT := "New Name"
	up, err := s.Update(r.ID, Patch{Title: &newT, Actor: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if up.Title != "New Name" || len(up.Aliases) != 1 || up.Aliases[0] != "Old Name" || up.UpdatedAt == r.UpdatedAt || up.ID != r.ID {
		t.Fatalf("%+v", up)
	}
	got, err := s.Get("old name")
	if err != nil || got.UID != r.UID {
		t.Fatalf("alias must resolve: %v %v", got.ID, err)
	}
	// the old title is free for another record now? It is an alias only: allowed, resolved by exact-title first.
	if _, err := s.Create(row("Old Name")); err != nil {
		t.Fatalf("old title becomes reusable: %v", err)
	}
	if got, _ := s.Get("old name"); got.UID == r.UID {
		t.Fatal("exact title must win over alias")
	}
	taken := "taken"
	var tc *ErrTitleConflict
	if _, err := s.Update(r.ID, Patch{Title: &taken}); !errors.As(err, &tc) || tc.Existing.UID != other.UID {
		t.Fatalf("want conflict, got %v", err)
	}
	// case-only change keeps the record, no alias, no conflict with itself
	caps := "NEW NAME"
	up, err = s.Update(r.ID, Patch{Title: &caps})
	if err != nil || up.Title != "NEW NAME" || len(up.Aliases) != 1 {
		t.Fatalf("%+v %v", up, err)
	}
	// renaming back to an old title now held by another record is refused
	nEvents := len(*evs)
	back := "Old Name"
	if _, err := s.Update(r.ID, Patch{Title: &back}); err == nil {
		t.Fatal("old title is now held by another record")
	}
	if len(*evs) != nEvents {
		t.Fatalf("a failed update must not audit (%d events, want %d)", len(*evs), nEvents)
	}
	var upd auditEv
	for _, e := range *evs {
		if e.action == AuditUpdated {
			upd = e
			break
		}
	}
	if upd.details["actor"] != "mcp" || upd.subject != "New Name" {
		t.Fatalf("update audit %+v", upd)
	}
	if ch, _ := upd.details["changed"].([]string); len(ch) != 1 || ch[0] != "title" {
		t.Fatalf("changed %v", upd.details["changed"])
	}
}

func TestUpdateFieldsStateAndNoop(t *testing.T) {
	s, dir, evs, _ := newTestStore(t, nil)
	r := mustCreate(t, s, row("Thing"))
	n0 := len(*evs)
	same := "Thing"
	if got, err := s.Update(r.ID, Patch{Title: &same}); err != nil || got.UpdatedAt != r.UpdatedAt || len(*evs) != n0 {
		t.Fatal("a no-op update must not write or audit")
	}
	body, src, sb, rb, sv, why := "b", "src", "auto1", "auto2", "v1", "why"
	links := []Link{{"automaton", "auto1"}}
	st, nr := StateShipped, true
	got, err := s.Update(r.ID, Patch{Body: &body, LegacySource: &src, ShippedBy: &sb, RaisedBy: &rb, ShippedVersion: &sv,
		ReviewReason: &why, Links: &links, State: &st, NeedsReview: &nr, AddAliases: []string{"Alt", "alt"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "b" || got.State != StateShipped || got.ClosedAt == "" || !got.NeedsReview || len(got.Aliases) != 1 || got.Links[0].Ref != "auto1" {
		t.Fatalf("%+v", got)
	}
	closedAt := got.ClosedAt
	// updating while closed keeps closed_at
	b2 := "b2"
	got, _ = s.Update(r.ID, Patch{Body: &b2})
	if got.ClosedAt != closedAt {
		t.Fatal("closed_at must not move while still closed")
	}
	// reopening clears closed_at; never deleted
	op := StateInProgress
	got, _ = s.Update(r.ID, Patch{State: &op})
	if got.ClosedAt != "" || got.State != StateInProgress {
		t.Fatalf("%+v", got)
	}
	bad := StateFixed
	var inv *ErrInvalid
	if _, err := s.Update(r.ID, Patch{State: &bad}); !errors.As(err, &inv) {
		t.Fatalf("finding state on a row: %v", err)
	}
	if _, err := s.Update("nope", Patch{}); !errors.Is(err, ErrNotFound) {
		t.Fatal("update of unknown ref")
	}
	// persisted
	s2, _ := Open(dir, nil)
	if g, _ := s2.Get(r.UID); g.Body != "b2" || g.State != StateInProgress {
		t.Fatalf("not persisted %+v", g)
	}
}

func TestResolveOrderPrefixAndAmbiguity(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := mustCreate(t, s, row("Alpha Plan"))
	b := mustCreate(t, s, row("Alpha Report"))
	c := mustCreate(t, s, Record{Kind: KindRegistryRow, Project: "other", Title: "Alpha Plan"})
	mustCreate(t, s, row("Beta"))
	// unambiguous prefix
	if r, err := s.Resolve("alpha rep", ""); err != nil || r.UID != b.UID {
		t.Fatalf("prefix: %v %v", r.ID, err)
	}
	// ambiguous prefix returns candidates
	_, err := s.Resolve("alpha", "")
	var amb *ErrAmbiguous
	if !errors.As(err, &amb) || len(amb.Candidates) != 3 || !strings.Contains(amb.Error(), "which one") {
		t.Fatalf("want ambiguity, got %v", err)
	}
	// same exact title in two projects is ambiguous without a project, resolved with one
	if _, err := s.Resolve("alpha plan", ""); !errors.As(err, &amb) || len(amb.Candidates) != 2 {
		t.Fatalf("cross-project exact title: %v", err)
	}
	if r, err := s.Resolve("alpha plan", "other"); err != nil || r.UID != c.UID {
		t.Fatalf("project scoped: %v", err)
	}
	if _, err := s.Resolve(a.ID, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatal("an id outside the project must not resolve when scoped")
	}
	if _, err := s.Resolve(a.UID, "other"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a uid outside the project must not resolve when scoped")
	}
	if r, _ := s.Resolve(b.UID, "datawatch"); r.UID != b.UID {
		t.Fatal("uid in project")
	}
	for _, ref := range []string{"", "  ", "zzz", "row 99"} {
		if _, err := s.Resolve(ref, ""); !errors.Is(err, ErrNotFound) {
			t.Errorf("Resolve(%q) = %v", ref, err)
		}
	}
	// a title that looks like a row ref still resolves as a title when no row matches
	d := mustCreate(t, s, row("Row 7 cleanup"))
	if r, err := s.Resolve("row 7 cleanup", ""); err != nil || r.UID != d.UID {
		t.Fatalf("%v", err)
	}
	// two records with the same alias are ambiguous
	x := mustCreate(t, s, row("X1"))
	y := mustCreate(t, s, row("Y1"))
	for _, r := range []Record{x, y} {
		if _, err := s.Update(r.ID, Patch{AddAliases: []string{"shared old"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Resolve("shared old", ""); !errors.As(err, &amb) {
		t.Fatalf("alias ambiguity: %v", err)
	}
}

func TestRowRefAcrossProjects(t *testing.T) {
	s, _, _, _ := newTestStore(t, nil)
	a := row("A")
	a.LegacyNumber = 9
	mustCreate(t, s, a)
	b := Record{Kind: KindRegistryRow, Project: "other", Title: "B", LegacyNumber: 9}
	mustCreate(t, s, b)
	var amb *ErrAmbiguous
	if _, err := s.Get("row 9"); !errors.As(err, &amb) {
		t.Fatalf("%v", err)
	}
	if r, err := s.Resolve("row 9", "other"); err != nil || r.Title != "B" {
		t.Fatalf("%v", err)
	}
}

func TestConcurrentCreates(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n*2)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Create(row(fmt.Sprintf("Concurrent %d", i)))
			errs <- err
			// same title from every goroutine: exactly one wins
			_, err = s.Create(row("Contended"))
			if err != nil {
				var tc *ErrTitleConflict
				if !errors.As(err, &tc) {
					errs <- err
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if s.Len() != n+1 {
		t.Fatalf("len %d want %d", s.Len(), n+1)
	}
	s2, _ := Open(dir, nil)
	if s2.Len() != n+1 || len(s2.Skipped()) != 0 {
		t.Fatalf("reload len %d skipped %v", s2.Len(), s2.Skipped())
	}
	ids := map[string]bool{}
	recs, _ := s2.List(Filter{Limit: MaxLimit})
	for _, r := range recs {
		if ids[r.ID] {
			t.Fatalf("duplicate id %s", r.ID)
		}
		ids[r.ID] = true
	}
}

func TestLeftoverTempFileIgnored(t *testing.T) {
	s, dir, _, ls := newTestStore(t, nil)
	r := mustCreate(t, s, row("Real"))
	kindDir := filepath.Join(dir, "plan-records", "registry_row")
	// a crash mid-write leaves a partial temp file next to the real one
	if err := os.WriteFile(filepath.Join(kindDir, r.UID+".json.tmp-12345"), []byte(`{"id":"half`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kindDir, "stray.tmp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(kindDir, "sub.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir, nil, Options{Logf: ls.logf})
	if err != nil {
		t.Fatal(err)
	}
	if s2.Len() != 1 || len(s2.Skipped()) != 0 {
		t.Fatalf("temp file must be ignored: len=%d skipped=%v", s2.Len(), s2.Skipped())
	}
	// writes leave no temp files behind
	entries, _ := os.ReadDir(kindDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") && !strings.HasSuffix(e.Name(), "12345") {
			t.Fatalf("leftover temp %s", e.Name())
		}
	}
}

func TestCorruptFilesSkippedAndLogged(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	good := mustCreate(t, s, row("Good"))
	dup := mustCreate(t, s, row("Dup Source"))
	kindDir := filepath.Join(dir, "plan-records", "registry_row")
	write := func(name, content string) { _ = os.WriteFile(filepath.Join(kindDir, name), []byte(content), 0o600) }
	write("corrupt.json", `{not json`)
	write("noid.json", `{"kind":"registry_row","project":"p","state":"open","title":"x"}`)
	write("invalid.json", `{"id":"i","uid":"invalid","kind":"registry_row","project":"p","state":"fixed","title":"x"}`)
	write("wrong-uid.json", `{"id":"w","uid":"other","kind":"registry_row","project":"p","state":"open","title":"w"}`)
	write("wrongkind.json", `{"id":"k","uid":"wrongkind","kind":"finding","project":"p","state":"open","title":"k"}`)
	write("sealed.json", `{"v":1,"meta":{},"sealed":"abc"}`)
	// a duplicate id under a different uid
	b, _ := os.ReadFile(filepath.Join(kindDir, dup.UID+".json"))
	var d Record
	_ = json.Unmarshal(b, &d)
	d.UID = "zzz-dupuid"
	d.Title = "Dup Copy"
	nb, _ := json.Marshal(d)
	write("zzz-dupuid.json", string(nb))
	// a same-title record under a different id is kept but logged
	d2 := Record{ID: "other-id", UID: "zzz-sametitle", Kind: KindRegistryRow, Project: "datawatch", State: StateOpen, Title: "Good"}
	nb, _ = json.Marshal(d2)
	write("zzz-sametitle.json", string(nb))

	ls := &logSink{}
	s2, err := Open(dir, nil, Options{Logf: ls.logf})
	if err != nil {
		t.Fatal(err)
	}
	if s2.Len() != 3 {
		t.Fatalf("len %d (good, dup source and the same-title record are kept)", s2.Len())
	}
	if len(s2.Skipped()) != 7 {
		t.Fatalf("skipped %v", s2.Skipped())
	}
	if r, err := s2.Get(good.UID); err != nil || r.Title != "Good" {
		t.Fatal("good record lost")
	}
	logged := strings.Join(ls.msgs, "\n")
	for _, want := range []string{"corrupt.json", "noid.json", "invalid.json", "wrong-uid.json", "wrongkind.json", "sealed.json", "zzz-dupuid.json", "shares a title"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log lacks %q:\n%s", want, logged)
		}
	}
}

func TestOpenDefaultsAndErrors(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.opts.ImportConflictPolicy != PolicySuffix {
		t.Fatal("default policy must be suffix")
	}
	if s.now().IsZero() {
		t.Fatal("clock")
	}
	s.logf("default logger %d", 1) // falls back to log.Printf
	// the data dir is a file: cannot create the store dir
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, []byte("x"), 0o600)
	if _, err := Open(f, nil); err == nil {
		t.Fatal("expected an error opening under a file")
	}
	// an unreadable kind directory is logged, not fatal
	dir := t.TempDir()
	st, _ := Open(dir, nil)
	_ = os.RemoveAll(filepath.Join(dir, "plan-records", "finding"))
	ls := &logSink{}
	st2 := &Store{root: st.root, byUID: map[string]*Record{}, byID: map[string]string{}, byTitle: map[string]string{}, opts: Options{Logf: ls.logf}}
	st2.loadKind(KindFinding)
	if len(st2.Skipped()) != 1 {
		t.Fatal("missing dir should be skipped and logged")
	}
}

func TestWriteFailureLeavesIndexUntouched(t *testing.T) {
	s, dir, _, _ := newTestStore(t, nil)
	r := mustCreate(t, s, row("Keep"))
	kindDir := filepath.Join(dir, "plan-records", "registry_row")
	if err := os.Chmod(kindDir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	defer os.Chmod(kindDir, 0o700)
	if f, err := os.CreateTemp(kindDir, "probe"); err == nil {
		f.Close()
		t.Skip("running with privileges that ignore directory modes")
	}
	if _, err := s.Create(row("Fails")); err == nil {
		t.Fatal("create should fail")
	}
	nb := "changed"
	if _, err := s.Update(r.ID, Patch{Body: &nb}); err == nil {
		t.Fatal("update should fail")
	}
	if got, _ := s.Get(r.ID); got.Body != "" || s.Len() != 1 {
		t.Fatal("failed writes must not change the index")
	}
}

func TestKeySealerAndSecureMode(t *testing.T) {
	if _, err := KeySealer([]byte("short")); err == nil {
		t.Fatal("short key must be refused")
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	sealer, err := KeySealer(key)
	if err != nil {
		t.Fatal(err)
	}
	s, dir, _, _ := newTestStore(t, sealer)
	in := row("Secret Plan Title")
	in.Body, in.LegacyNumber, in.ShippedBy = "very secret body", 77, "automaton-xyz"
	in.Aliases = []string{"hidden alias"}
	in.Links = []Link{{"doc", "hidden/link.md"}}
	in.LegacySource = "hidden/source.md"
	r := mustCreate(t, s, in)
	closed := StateDone
	r, _ = s.Update(r.ID, Patch{State: &closed})

	path := filepath.Join(dir, "plan-records", "registry_row", r.UID+".json")
	raw, _ := os.ReadFile(path)
	for _, secret := range []string{"Secret Plan Title", "very secret body", "automaton-xyz", "hidden alias", "hidden/link.md", "hidden/source.md", "shipped_by"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("sealed file leaks %q:\n%s", secret, raw)
		}
	}
	// plaintext metadata only: exactly the documented set
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	_ = json.Unmarshal(env["meta"], &meta)
	want := map[string]bool{"kind": true, "project": true, "state": true, "id": true, "uid": true, "legacy_number": true, "created_at": true, "updated_at": true, "closed_at": true}
	if len(meta) != len(want) {
		t.Fatalf("meta fields %v", meta)
	}
	for k := range meta {
		if !want[k] {
			t.Fatalf("unexpected readable field %q", k)
		}
	}
	if meta["state"] != "done" || meta["id"] != r.ID || meta["uid"] != r.UID || meta["legacy_number"] != float64(77) || meta["closed_at"] == "" {
		t.Fatalf("meta %v", meta)
	}
	// round trip with the key
	s2, err := Open(dir, sealer)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get("secret plan title")
	if err != nil || got.Body != "very secret body" || got.Aliases[0] != "hidden alias" {
		t.Fatalf("sealed round trip %+v %v", got, err)
	}
	// without a key or with the wrong key the file is skipped and logged
	ls := &logSink{}
	s3, _ := Open(dir, nil, Options{Logf: ls.logf})
	if s3.Len() != 0 || len(s3.Skipped()) != 1 || !strings.Contains(ls.msgs[0], "no key") {
		t.Fatalf("no key: len=%d %v", s3.Len(), ls.msgs)
	}
	wrong, _ := KeySealer([]byte("ffffffffffffffffffffffffffffffff"))
	ls = &logSink{}
	s4, _ := Open(dir, wrong, Options{Logf: ls.logf})
	if s4.Len() != 0 || len(s4.Skipped()) != 1 || !strings.Contains(ls.msgs[0], "cannot open") {
		t.Fatalf("wrong key: len=%d %v", s4.Len(), ls.msgs)
	}
	// a plaintext file is still readable under a key (migration)
	plain, _, _, _ := newTestStore(t, nil)
	pr := mustCreate(t, plain, row("Plain One"))
	pdir := filepath.Dir(filepath.Dir(plain.path(pr.Kind, pr.UID)))
	s5, _ := Open(filepath.Dir(pdir), sealer)
	if s5.Len() != 1 {
		t.Fatal("plaintext record must load under a key")
	}
	// corrupt envelope
	_ = os.WriteFile(path, []byte(`{"sealed": "x", "meta": "bad"}`), 0o600)
	ls = &logSink{}
	s6, _ := Open(dir, sealer, Options{Logf: ls.logf})
	if s6.Len() != 0 || !strings.Contains(strings.Join(ls.msgs, ""), "corrupt envelope") {
		t.Fatalf("%v", ls.msgs)
	}
}

type failSealer struct{}

func (failSealer) Seal([]byte) (string, error) { return "", errors.New("boom") }
func (failSealer) Open(string) ([]byte, error) { return nil, errors.New("boom") }

func TestSealFailureIsAnError(t *testing.T) {
	s, _, _, _ := newTestStore(t, failSealer{})
	if _, err := s.Create(row("X")); err == nil || !strings.Contains(err.Error(), "seal") {
		t.Fatalf("got %v", err)
	}
	if s.Len() != 0 {
		t.Fatal("failed create must not be indexed")
	}
}
