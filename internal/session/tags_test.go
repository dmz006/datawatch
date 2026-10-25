package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeTagRegistry is a tag registry that knows only the IDs it was given.
type fakeTagRegistry struct{ known map[string]bool }

var errFakeTagUnknown = errors.New("fake tag unknown")

func (f *fakeTagRegistry) RequireTag(id string) error {
	if !f.known[id] {
		return fmt.Errorf("%w: %q", errFakeTagUnknown, id)
	}
	return nil
}

// fakeOwnerLookup maps an Automaton's PRD ID to its tag IDs. Tests change
// the map to show that inheritance follows the owner.
type fakeOwnerLookup struct{ owners map[string][]string }

func (f *fakeOwnerLookup) AutomatonTagIDs(prdID string) ([]string, bool) {
	ids, ok := f.owners[prdID]
	return ids, ok
}

// newTagsTestManager returns a manager with a fake registry that knows the
// given tag IDs and a fake owner lookup holding the given Automata.
func newTagsTestManager(t *testing.T, known []string, owners map[string][]string) (*Manager, *fakeOwnerLookup) {
	t.Helper()
	mgr, _ := newTestManagerWithFake(t)
	reg := &fakeTagRegistry{known: map[string]bool{}}
	for _, id := range known {
		reg.known[id] = true
	}
	lookup := &fakeOwnerLookup{owners: owners}
	mgr.SetTagRegistry(reg)
	mgr.SetTagOwnerLookup(lookup)
	return mgr, lookup
}

// saveTagsTestSession stores a session with the given ID, PRD link and tags.
func saveTagsTestSession(t *testing.T, mgr *Manager, id, prdID string, tags []string) *Session {
	t.Helper()
	sess := &Session{ID: id, FullID: "testhost-" + id, State: StateRunning,
		PRDID: prdID, Tags: tags, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	return sess
}

func TestSessionTagsPersistAcrossStoreReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	st, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := st.Save(&Session{ID: "ab12", FullID: "host-ab12", State: StateRunning,
		Tags: []string{"tag-a", "tag-b"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewStore(path)
	if err != nil {
		t.Fatalf("reopen NewStore: %v", err)
	}
	defer reopened.Close()
	got, ok := reopened.Get("host-ab12")
	if !ok {
		t.Fatal("session missing after reload")
	}
	if !reflect.DeepEqual(got.Tags, []string{"tag-a", "tag-b"}) {
		t.Errorf("own tags after reload = %v, want [tag-a tag-b]", got.Tags)
	}
}

func TestSessionTagsInheritedTagsNeverStored(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := st.Save(&Session{ID: "cd34", FullID: "host-cd34", State: StateRunning,
		PRDID: "prd-1", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The store may write one file or a records layout, so scan everything it wrote.
	wrote := 0
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		wrote++
		if strings.Contains(string(data), "inherited_tags") {
			t.Errorf("%s persisted inherited_tags: %s", p, data)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk store dir: %v", err)
	}
	if wrote == 0 {
		t.Fatal("store wrote no files; the check would pass vacuously")
	}
}

func TestSessionTagsInheritedFollowsOwningAutomaton(t *testing.T) {
	owners := map[string][]string{"prd-1": {"tag-a"}}
	mgr, lookup := newTagsTestManager(t, []string{"tag-a", "tag-b"}, owners)
	child := saveTagsTestSession(t, mgr, "ef56", "prd-1", nil)

	if got := mgr.InheritedTags(child); !reflect.DeepEqual(got, []string{"tag-a"}) {
		t.Fatalf("inherited before change = %v, want [tag-a]", got)
	}

	// The Automaton's tags change; the child follows on its next read.
	lookup.owners["prd-1"] = []string{"tag-b"}
	if got := mgr.InheritedTags(child); !reflect.DeepEqual(got, []string{"tag-b"}) {
		t.Errorf("inherited after owner change = %v, want [tag-b]", got)
	}
	if len(child.Tags) != 0 {
		t.Errorf("inheritance leaked into own tags: %v", child.Tags)
	}
}

func TestSessionTagsNoAutomatonHasEmptyInherited(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"tag-a"}, map[string][]string{"prd-1": {"tag-a"}})

	outside := saveTagsTestSession(t, mgr, "1a2b", "", []string{"tag-a"})
	got := mgr.InheritedTags(outside)
	if got == nil || len(got) != 0 {
		t.Errorf("session outside any Automaton: inherited = %#v, want empty non-nil", got)
	}

	orphaned := saveTagsTestSession(t, mgr, "3c4d", "prd-gone", nil)
	if got := mgr.InheritedTags(orphaned); got == nil || len(got) != 0 {
		t.Errorf("session whose Automaton is gone: inherited = %#v, want empty non-nil", got)
	}
	if got := mgr.InheritedTags(nil); got == nil || len(got) != 0 {
		t.Errorf("nil session: inherited = %#v, want empty non-nil", got)
	}
}

func TestSessionTagsRefusesUnknownTagID(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"tag-a"}, nil)
	sess := saveTagsTestSession(t, mgr, "5e6f", "", []string{"tag-a"})

	_, err := mgr.SetSessionTags(sess.ID, []string{"tag-a", "nope"})
	if !errors.Is(err, errFakeTagUnknown) {
		t.Fatalf("SetSessionTags unknown id: err = %v, want unknown-tag refusal", err)
	}
	if !reflect.DeepEqual(sess.Tags, []string{"tag-a"}) {
		t.Errorf("refused update changed tags to %v", sess.Tags)
	}

	if _, err := mgr.Start(context.Background(), "task", "", t.TempDir(),
		&StartOptions{Tags: []string{"nope"}}); !errors.Is(err, errFakeTagUnknown) {
		t.Errorf("Start with unknown tag: err = %v, want unknown-tag refusal", err)
	}
}

func TestSessionTagsRegistryUnavailableRefusesTags(t *testing.T) {
	mgr, _ := newTestManagerWithFake(t)
	sess := saveTagsTestSession(t, mgr, "7a8b", "", nil)
	if _, err := mgr.SetSessionTags(sess.ID, []string{"tag-a"}); !errors.Is(err, ErrTagRegistryUnavailable) {
		t.Errorf("no registry wired: err = %v, want ErrTagRegistryUnavailable", err)
	}
}

func TestSessionTagsDedupesFirstWins(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"tag-a", "tag-b", "tag-c"}, nil)
	sess := saveTagsTestSession(t, mgr, "9b0c", "", nil)

	out, err := mgr.SetSessionTags(sess.ID, []string{"tag-b", "tag-a", "tag-b", "tag-c", "tag-a"})
	if err != nil {
		t.Fatalf("SetSessionTags: %v", err)
	}
	if want := []string{"tag-b", "tag-a", "tag-c"}; !reflect.DeepEqual(out.Tags, want) {
		t.Errorf("tags after dedupe = %v, want %v", out.Tags, want)
	}
}

func TestSessionTagsSetByShortOrFullID(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"tag-a"}, nil)
	saveTagsTestSession(t, mgr, "1d2e", "", nil)

	if _, err := mgr.SetSessionTags("testhost-1d2e", []string{"tag-a"}); err != nil {
		t.Errorf("set by full id: %v", err)
	}
	if _, err := mgr.SetSessionTags("1d2e", nil); err != nil {
		t.Errorf("set by short id: %v", err)
	}
	if _, err := mgr.SetSessionTags("ffff", nil); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("unknown session: err = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionTagsCountsSessionsForDeleteInUse(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"tag-a", "tag-b"}, map[string][]string{"prd-1": {"tag-a"}})
	saveTagsTestSession(t, mgr, "2f3a", "", []string{"tag-a"})
	saveTagsTestSession(t, mgr, "4b5c", "", []string{"tag-a", "tag-a"})
	// Inherited from the Automaton, not an own tag: not counted.
	saveTagsTestSession(t, mgr, "6d7e", "prd-1", nil)

	if n := mgr.CountSessionsWithTag("tag-a"); n != 2 {
		t.Errorf("CountSessionsWithTag(tag-a) = %d, want 2", n)
	}
	if n := mgr.CountSessionsWithTag("tag-b"); n != 0 {
		t.Errorf("CountSessionsWithTag(tag-b) = %d, want 0", n)
	}
}

func TestSessionTagsStartStoresOwnTags(t *testing.T) {
	mgr, _ := newTagsTestManager(t, []string{"tag-a", "tag-b"}, nil)
	sess, err := mgr.Start(context.Background(), "task", "", t.TempDir(),
		&StartOptions{Tags: []string{"tag-a", "tag-b", "tag-a"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if want := []string{"tag-a", "tag-b"}; !reflect.DeepEqual(sess.Tags, want) {
		t.Errorf("started session tags = %v, want %v", sess.Tags, want)
	}
}
