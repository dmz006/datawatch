package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestPendingStore(t *testing.T) (*PendingItemStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewPendingItemStore(dir)
	if err != nil {
		t.Fatalf("NewPendingItemStore: %v", err)
	}
	return s, dir
}

func wantErrPrefix(t *testing.T, err error, prefix string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error starting %q, got nil", prefix)
	}
	if !strings.HasPrefix(err.Error(), prefix) {
		t.Fatalf("error %q does not start with %q", err.Error(), prefix)
	}
}

func TestPendingItemStoreCreateAndGet(t *testing.T) {
	s, _ := newTestPendingStore(t)
	p, err := s.Create("  Write the importer  ", " ext-9 ", PendingItemSourceInternal)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(p.ID) != 8 {
		t.Fatalf("id %q is not 8 hex characters", p.ID)
	}
	for _, c := range p.ID {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("id %q has a non-hex character", p.ID)
		}
	}
	if p.Title != "Write the importer" || p.Ref != "ext-9" {
		t.Fatalf("title/ref not trimmed: %q %q", p.Title, p.Ref)
	}
	if p.State != PendingItemWaiting || p.Source != PendingItemSourceInternal {
		t.Fatalf("state/source = %s/%s", p.State, p.Source)
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.AttachedAt != nil || p.DoneAt != nil {
		t.Fatalf("timestamps wrong: %+v", p)
	}
	got, ok := s.Get(p.ID)
	if !ok || got.Title != p.Title {
		t.Fatalf("Get = %+v, %v", got, ok)
	}
	// The returned value is a copy.
	got.Title = "changed"
	again, _ := s.Get(p.ID)
	if again.Title != "Write the importer" {
		t.Fatalf("Get returned a shared pointer")
	}
	if _, ok := s.Get("00000000"); ok {
		t.Fatalf("Get of an unknown id succeeded")
	}
	// An empty source defaults to internal.
	q, err := s.Create("another", "", "")
	if err != nil || q.Source != PendingItemSourceInternal {
		t.Fatalf("default source: %+v, %v", q, err)
	}
	// 300 characters is allowed, 301 is not.
	if _, err := s.Create(strings.Repeat("é", 300), "", ""); err != nil {
		t.Fatalf("300 characters refused: %v", err)
	}
	_, err = s.Create(strings.Repeat("é", 301), "", "")
	wantErrPrefix(t, err, "400:")
	_, err = s.Create("x", "", "bogus")
	wantErrPrefix(t, err, "400:")
}

func TestPendingItemStorePersistsAcrossReopen(t *testing.T) {
	s, dir := newTestPendingStore(t)
	a, _ := s.Create("first", "r1", PendingItemSourceFile)
	b, _ := s.Create("second", "", PendingItemSourceInternal)
	if _, err := s.Update(a.ID, func(p *PendingItem) error {
		p.State = PendingItemAttached
		p.AutomatonID = "abcd1234"
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "pending_items.json"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	s2, err := NewPendingItemStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok := s2.Get(a.ID)
	if !ok || got.State != PendingItemAttached || got.AutomatonID != "abcd1234" || got.AttachedAt == nil {
		t.Fatalf("reopened item = %+v, %v", got, ok)
	}
	if got.Ref != "r1" || got.Source != PendingItemSourceFile {
		t.Fatalf("ref/source lost: %+v", got)
	}
	if _, ok := s2.Get(b.ID); !ok {
		t.Fatalf("second item lost")
	}
	if err := s2.Delete(b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	s3, _ := NewPendingItemStore(dir)
	if _, ok := s3.Get(b.ID); ok {
		t.Fatalf("deleted item came back")
	}
	// A corrupt file is an error, not an empty store.
	if err := os.WriteFile(filepath.Join(dir, "pending_items.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPendingItemStore(dir); err == nil {
		t.Fatalf("corrupt file accepted")
	}
}

func TestPendingItemStoreTransitions(t *testing.T) {
	states := []PendingItemState{PendingItemWaiting, PendingItemAttached, PendingItemDone}
	allowed := map[[2]PendingItemState]bool{
		{PendingItemWaiting, PendingItemAttached}: true,
		{PendingItemWaiting, PendingItemDone}:     true,
		{PendingItemAttached, PendingItemDone}:    true,
	}
	for _, from := range states {
		for _, to := range states {
			if from == to {
				continue
			}
			s, _ := newTestPendingStore(t)
			p, _ := s.Create("item", "", "")
			// Walk the item to the starting state.
			switch from {
			case PendingItemAttached:
				if _, err := s.Update(p.ID, func(x *PendingItem) error {
					x.State, x.AutomatonID = PendingItemAttached, "a1b2c3d4"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			case PendingItemDone:
				if _, err := s.Update(p.ID, func(x *PendingItem) error { x.State = PendingItemDone; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Update(p.ID, func(x *PendingItem) error {
				x.State = to
				if to == PendingItemAttached {
					x.AutomatonID = "a1b2c3d4"
				}
				return nil
			})
			if allowed[[2]PendingItemState{from, to}] {
				if err != nil {
					t.Fatalf("%s -> %s refused: %v", from, to, err)
				}
				if got.State != to {
					t.Fatalf("%s -> %s left state %s", from, to, got.State)
				}
				if to == PendingItemAttached && got.AttachedAt == nil {
					t.Fatalf("attached_at not set")
				}
				if to == PendingItemDone && got.DoneAt == nil {
					t.Fatalf("done_at not set")
				}
				continue
			}
			wantErrPrefix(t, err, "409:")
			cur, _ := s.Get(p.ID)
			if cur.State != from {
				t.Fatalf("%s -> %s refused but state is now %s", from, to, cur.State)
			}
		}
	}
	// An unknown state is a bad request.
	s, _ := newTestPendingStore(t)
	p, _ := s.Create("item", "", "")
	_, err := s.Update(p.ID, func(x *PendingItem) error { x.State = "bogus"; return nil })
	wantErrPrefix(t, err, "400:")
	// Attaching needs an automaton id.
	_, err = s.Update(p.ID, func(x *PendingItem) error { x.State = PendingItemAttached; return nil })
	wantErrPrefix(t, err, "400:")
	// Id, source and created_at cannot be changed through Update.
	got, err := s.Update(p.ID, func(x *PendingItem) error {
		x.ID, x.Source = "ffffffff", PendingItemSourceFile
		return nil
	})
	if err != nil || got.ID != p.ID || got.Source != PendingItemSourceInternal {
		t.Fatalf("fixed fields changed: %+v, %v", got, err)
	}
	// A mutate error leaves the item alone.
	_, err = s.Update(p.ID, func(x *PendingItem) error { x.Title = "lost"; return os.ErrInvalid })
	if err == nil {
		t.Fatalf("mutate error swallowed")
	}
	if cur, _ := s.Get(p.ID); cur.Title != "item" {
		t.Fatalf("failed update changed the title to %q", cur.Title)
	}
}

func TestPendingItemStoreDuplicateRefRejected(t *testing.T) {
	s, _ := newTestPendingStore(t)
	a, err := s.Create("one", "dup", PendingItemSourceFile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Create("two", "dup", PendingItemSourceFile)
	wantErrPrefix(t, err, "409:")
	// Same ref in another source is fine.
	b, err := s.Create("three", "dup", PendingItemSourceInternal)
	if err != nil {
		t.Fatalf("same ref in another source refused: %v", err)
	}
	// Items without a ref never collide.
	if _, err := s.Create("four", "", PendingItemSourceFile); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("five", "", PendingItemSourceFile); err != nil {
		t.Fatal(err)
	}
	// Update cannot move an item onto an existing ref in its source.
	c, _ := s.Create("six", "other", PendingItemSourceFile)
	_, err = s.Update(c.ID, func(p *PendingItem) error { p.Ref = "dup"; return nil })
	wantErrPrefix(t, err, "409:")
	// Keeping its own ref is not a collision.
	if _, err := s.Update(a.ID, func(p *PendingItem) error { p.Title = "renamed"; return nil }); err != nil {
		t.Fatalf("self ref collision: %v", err)
	}
	if got, ok := s.FindByRef(PendingItemSourceFile, "dup"); !ok || got.ID != a.ID {
		t.Fatalf("FindByRef file = %+v, %v", got, ok)
	}
	if got, ok := s.FindByRef(PendingItemSourceInternal, "dup"); !ok || got.ID != b.ID {
		t.Fatalf("FindByRef internal = %+v, %v", got, ok)
	}
	if _, ok := s.FindByRef(PendingItemSourceFile, "missing"); ok {
		t.Fatalf("FindByRef found a missing ref")
	}
	if _, ok := s.FindByRef(PendingItemSourceFile, ""); ok {
		t.Fatalf("FindByRef matched an empty ref")
	}
	// Deleting frees the ref.
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("again", "dup", PendingItemSourceFile); err != nil {
		t.Fatalf("ref not freed by delete: %v", err)
	}
}

func TestPendingItemStoreUnknownIDErrors(t *testing.T) {
	s, _ := newTestPendingStore(t)
	_, err := s.Update("deadbeef", func(p *PendingItem) error { return nil })
	wantErrPrefix(t, err, "404:")
	wantErrPrefix(t, s.Delete("deadbeef"), "404:")
	if _, ok := s.Get("deadbeef"); ok {
		t.Fatalf("Get found an unknown id")
	}
	m := &Manager{pendingItems: s}
	_, err = m.GetPendingItem("deadbeef")
	wantErrPrefix(t, err, "404:")
}

func TestPendingItemStoreListFilters(t *testing.T) {
	s, _ := newTestPendingStore(t)
	a, _ := s.Create("a", "ra", PendingItemSourceInternal)
	b, _ := s.Create("b", "rb", PendingItemSourceFile)
	c, _ := s.Create("c", "", PendingItemSourceFile)
	if _, err := s.Update(b.ID, func(p *PendingItem) error { p.State = PendingItemDone; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(c.ID, func(p *PendingItem) error {
		p.State, p.AutomatonID = PendingItemAttached, "11223344"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ids := func(l []*PendingItem) string {
		var out []string
		for _, p := range l {
			out = append(out, p.Title)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		f    PendingItemFilter
		want string
	}{
		{PendingItemFilter{}, "a,b,c"},
		{PendingItemFilter{State: PendingItemWaiting}, "a"},
		{PendingItemFilter{State: PendingItemDone}, "b"},
		{PendingItemFilter{State: PendingItemAttached}, "c"},
		{PendingItemFilter{Source: PendingItemSourceFile}, "b,c"},
		{PendingItemFilter{Source: PendingItemSourceInternal}, "a"},
		{PendingItemFilter{State: PendingItemAttached, Source: PendingItemSourceFile}, "c"},
		{PendingItemFilter{State: PendingItemWaiting, Source: PendingItemSourceFile}, ""},
	}
	for _, tc := range cases {
		if got := ids(s.List(tc.f)); got != tc.want {
			t.Errorf("List(%+v) = %q, want %q", tc.f, got, tc.want)
		}
	}
	// The Manager methods read the same registry and audit a create.
	var audited []string
	m := &Manager{pendingItems: s, auditFn: func(action string, _ map[string]any) { audited = append(audited, action) }}
	made, err := m.CreatePendingItem("via manager", "mref", "tester")
	if err != nil || made.Source != PendingItemSourceInternal {
		t.Fatalf("CreatePendingItem: %+v, %v", made, err)
	}
	if len(audited) != 1 || audited[0] != "pending_item_create" {
		t.Fatalf("audit = %v", audited)
	}
	if got, err := m.GetPendingItem(made.ID); err != nil || got.Title != "via manager" {
		t.Fatalf("GetPendingItem: %+v, %v", got, err)
	}
	if n := len(m.ListPendingItems(PendingItemFilter{State: PendingItemWaiting})); n != 2 {
		t.Fatalf("waiting count = %d, want 2", n)
	}
	_ = a
}

func TestPendingItemStoreBlankTitleRejected(t *testing.T) {
	s, _ := newTestPendingStore(t)
	for _, title := range []string{"", "   ", "\t\n"} {
		_, err := s.Create(title, "", "")
		wantErrPrefix(t, err, "400:")
	}
	if n := len(s.List(PendingItemFilter{})); n != 0 {
		t.Fatalf("a refused create left %d items", n)
	}
	p, _ := s.Create("fine", "", "")
	_, err := s.Update(p.ID, func(x *PendingItem) error { x.Title = "  "; return nil })
	wantErrPrefix(t, err, "400:")
	if cur, _ := s.Get(p.ID); cur.Title != "fine" {
		t.Fatalf("blank-title update changed the title to %q", cur.Title)
	}
	m := &Manager{pendingItems: s}
	_, err = m.CreatePendingItem(" ", "", "x")
	wantErrPrefix(t, err, "400:")
}
