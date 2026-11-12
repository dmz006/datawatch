package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeInheritedReader is the fake session list for the inherited-tag tests:
// it maps a PRD ID to the system tags carried by each of the sessions that
// carry that PRD ID (sessions own the list; the Automaton never does).
type fakeInheritedReader struct {
	mu       sync.Mutex
	sessions map[string]map[string][]string // prdID -> sessionID -> system tags
}

// addSession records one session's system tags under its PRD ID.
func (f *fakeInheritedReader) addSession(prdID, sessionID string, tags ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sessions == nil {
		f.sessions = map[string]map[string][]string{}
	}
	if f.sessions[prdID] == nil {
		f.sessions[prdID] = map[string][]string{}
	}
	f.sessions[prdID][sessionID] = append([]string(nil), tags...)
}

// SystemTagsByPRD returns every system tag of every session that carries
// prdID, with repeats, like *session.Manager does.
func (f *fakeInheritedReader) SystemTagsByPRD(prdID string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, tags := range f.sessions[prdID] {
		out = append(out, tags...)
	}
	return out
}

func TestPlacementTags_InheritedByAutomaton(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reader := &fakeInheritedReader{}
	m.SetInheritedTagReader(reader)

	// prd-1 owns two sessions whose system tags overlap; prd-2 owns no
	// sessions; prd-3 owns one session with no system tags at all.
	reader.addSession("prd-1", "s1", "node-a", "model-x")
	reader.addSession("prd-1", "s2", "model-x", "node-b")
	reader.addSession("prd-3", "s3")
	for _, p := range []*PRD{{ID: "prd-1"}, {ID: "prd-2"}, {ID: "prd-3"}} {
		if err := m.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}

	// The union of the sessions' system tags: de-duplicated and sorted.
	got := m.InheritedTagsForPRD("prd-1")
	want := []string{"model-x", "node-a", "node-b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("inherited tags of prd-1 = %v, want the sorted union %v", got, want)
	}

	// No sessions, or sessions without system tags: the field is absent
	// (nil), so the view omits it.
	if got := m.InheritedTagsForPRD("prd-2"); got != nil {
		t.Errorf("inherited tags of prd-2 (no sessions) = %v, want none", got)
	}
	if got := m.InheritedTagsForPRD("prd-3"); got != nil {
		t.Errorf("inherited tags of prd-3 (no system tags) = %v, want none", got)
	}

	// The summary view carries the field as a separate read-only list, next
	// to the PRD's own tags.
	view := BuildSummaryView(&PRD{ID: "prd-1", TagIDs: []string{"own-tag"}})
	view.InheritedTags = m.InheritedTagsForPRD("prd-1")
	if !reflect.DeepEqual(view.InheritedTags, want) {
		t.Errorf("view inherited_tags = %v, want %v", view.InheritedTags, want)
	}

	// The field is not part of the stored PRD record and never lands there:
	// it is a read computed from the session records, so the record's own
	// tag list is the only stored one.
	stored, ok := m.Store().GetPRD("prd-1")
	if !ok {
		t.Fatal("prd-1 not stored")
	}
	if len(stored.TagIDs) != 0 {
		t.Errorf("stored prd-1 TagIDs = %v, want none (the read never writes)", stored.TagIDs)
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "inherited_tags") {
		t.Errorf("stored PRD serialises inherited_tags: %s", raw)
	}

	// The Automation-level union includes the inherited tags of its members:
	// each member's own tags plus the system tags of its sessions, once,
	// sorted. prd-1 carries no own tags here, so the union is the inherited
	// list.
	auto := &Automation{ID: "auto", Members: []AutomationMember{
		{ID: "m1", PRDID: "prd-1"}, {ID: "m2", PRDID: "prd-2"},
	}}
	autoView := m.AutomationViewOf(auto)
	wantAuto := []string{"model-x", "node-a", "node-b"}
	if !reflect.DeepEqual(autoView.Tags, wantAuto) {
		t.Errorf("automation tags = %v, want the union with inherited tags %v", autoView.Tags, wantAuto)
	}

	// Unwired reader: the field reads as empty, as before this feature.
	m.SetInheritedTagReader(nil)
	if got := m.InheritedTagsForPRD("prd-1"); got != nil {
		t.Errorf("inherited tags with no reader = %v, want none", got)
	}
}
