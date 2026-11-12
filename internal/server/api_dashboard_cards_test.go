// Duplicate-automata-card fix (docs/plans/2026-11-02-dashboard-card-audit.md
// §"Duplicate Automata card"): the stored layout (~/.datawatch/
// dashboard-layout.json) had no id-uniqueness invariant — the unguarded
// append path and the wholesale PUT replacement both admitted a second
// `tree`, and mergeSystemCards (add-missing-cards only) could never
// collapse it. Now every read and every write dedupes by id (first
// occurrence and its cs/rs win), and the dashboard.hide_inactive_cards
// setting round-trips through PUT/GET /api/config.
//
// All tests pin $HOME to a temp dir so the real stored layout and the
// real config are never read or written.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// dashTestServer isolates the layout file (os.UserHomeDir()-rooted) into
// a per-test temp dir via $HOME and returns a bare Server; the layout
// handlers need nothing else off it.
func dashTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return &Server{}, home
}

func writeDashFile(t *testing.T, home string, cards []dashCard) {
	t.Helper()
	dir := filepath.Join(home, ".datawatch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	b, err := json.Marshal(dashLayout{Cards: cards})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dashboard-layout.json"), b, 0o600); err != nil {
		t.Fatalf("write layout: %v", err)
	}
}

func readDashFile(t *testing.T, home string) []dashCard {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, ".datawatch", "dashboard-layout.json"))
	if err != nil {
		t.Fatalf("read layout: %v", err)
	}
	var layout dashLayout
	if err := json.Unmarshal(b, &layout); err != nil {
		t.Fatalf("unmarshal layout: %v", err)
	}
	return layout.Cards
}

func countID(cards []dashCard, id string) (n int, first dashCard) {
	for _, c := range cards {
		if c.ID == id && n == 0 {
			first = c
		}
		if c.ID == id {
			n++
		}
	}
	return
}

// A stored layout listing the same id twice (the observed duplicate
// Automata/"tree" card) reads back with exactly one id, keeping the
// first occurrence's cs/rs.
func TestDashLayoutReadDedupesDuplicateID(t *testing.T) {
	s, home := dashTestServer(t)
	writeDashFile(t, home, []dashCard{
		{ID: "tree", CS: 4, RS: 2, System: true},
		{ID: "orbital", CS: 6, RS: 2, System: true},
		{ID: "tree", CS: 3, RS: 1, System: true}, // later duplicate
		{ID: "widget", CS: 2, RS: 1},
	})
	layout, err := s.readDashLayout()
	if err != nil {
		t.Fatalf("readDashLayout: %v", err)
	}
	n, first := countID(layout.Cards, "tree")
	if n != 1 {
		t.Fatalf("tree count = %d, want 1; cards=%v", n, layout.Cards)
	}
	if first.CS != 4 || first.RS != 2 {
		t.Errorf("first duplicate must win: tree = cs %d rs %d, want cs 4 rs 2", first.CS, first.RS)
	}
}

// First-duplicate-wins on read, stated explicitly for the stored-file
// healing path: the healed layout is persisted, so the duplicate does
// not come back on the next read.
func TestDashLayoutFirstDuplicateWinsOnRead(t *testing.T) {
	s, home := dashTestServer(t)
	writeDashFile(t, home, []dashCard{
		{ID: "tree", CS: 2, RS: 2, System: true},
		{ID: "tree", CS: 12, RS: 1, System: true},
	})
	if _, err := s.readDashLayout(); err != nil {
		t.Fatalf("readDashLayout: %v", err)
	}
	cards := readDashFile(t, home)
	n, first := countID(cards, "tree")
	if n != 1 {
		t.Fatalf("stored tree count after read = %d, want 1", n)
	}
	if first.CS != 2 || first.RS != 2 {
		t.Errorf("stored tree = cs %d rs %d, want the first occurrence cs 2 rs 2", first.CS, first.RS)
	}
}

// PUT /api/dashboard/layout used to replace the file wholesale with the
// client's card array, inheriting any duplicate; the PUT branch now
// dedupes by id (first occurrence and its cs/rs win) before writing.
func TestDashboardDedupePutDuplicateID(t *testing.T) {
	s, home := dashTestServer(t)
	body := dashLayout{Cards: []dashCard{
		{ID: "tree", CS: 8, RS: 1}, // first occurrence
		{ID: "widget", CS: 2, RS: 1},
		{ID: "tree", CS: 4, RS: 3}, // later duplicate
		{ID: "widget", CS: 1, RS: 1},
	}}
	b, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	s.handleDashboardLayout(rr, httptest.NewRequest(http.MethodPut, "/api/dashboard/layout", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	cards := readDashFile(t, home)
	for _, id := range []string{"tree", "widget"} {
		if n, _ := countID(cards, id); n != 1 {
			t.Errorf("%s count after PUT = %d, want 1; cards=%v", id, n, cards)
		}
	}
	if n, first := countID(cards, "tree"); n == 1 && (first.CS != 8 || first.RS != 1) {
		t.Errorf("tree after PUT = cs %d rs %d, want first occurrence cs 8 rs 1", first.CS, first.RS)
	}
}

// System cards (tree, orbital, events, sparklines, gantt, heatmap,
// guardrails, ekg, smoke, memory-scope) stay always present: a layout
// that removed some of them reads back with all of them restored, and
// the restoration is persisted.
func TestDashLayoutSystemCardsRestoredAfterRemoval(t *testing.T) {
	s, home := dashTestServer(t)
	writeDashFile(t, home, []dashCard{{ID: "widget", CS: 2, RS: 1}})
	layout, err := s.readDashLayout()
	if err != nil {
		t.Fatalf("readDashLayout: %v", err)
	}
	for _, d := range defaultDashCards() {
		n, _ := countID(layout.Cards, d.ID)
		if n != 1 {
			t.Errorf("system card %q present %d times, want 1; cards=%v", d.ID, n, layout.Cards)
		}
	}
	n, _ := countID(layout.Cards, "widget")
	if n != 1 {
		t.Errorf("widget present %d times, want 1; cards=%v", n, layout.Cards)
	}
	cards := readDashFile(t, home)
	if n, _ := countID(cards, "gantt"); n != 1 {
		t.Errorf("restored gantt not persisted: %d times in file, want 1", n)
	}
	if n, _ := countID(cards, "timeline"); n != 1 {
		t.Errorf("restored timeline not persisted: %d times in file, want 1", n)
	}
}

// The unguarded card-append path: POSTing a card whose id is already in
// the layout must not create a second copy; the stored card keeps the
// first occurrence's cs/rs.
func TestDashCardsAddDuplicateIDFirstWins(t *testing.T) {
	s, home := dashTestServer(t)
	writeDashFile(t, home, []dashCard{
		{ID: "tree", CS: 2, RS: 2, System: true},
		{ID: "widget", CS: 2, RS: 1},
	})
	b, _ := json.Marshal(dashCard{ID: "tree", CS: 9, RS: 9})
	rr := httptest.NewRecorder()
	s.dashCardsAddHandler(rr, httptest.NewRequest(http.MethodPost, "/api/dashboard/cards", bytes.NewReader(b)))
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	cards := readDashFile(t, home)
	n, first := countID(cards, "tree")
	if n != 1 {
		t.Fatalf("tree count after append-duplicate = %d, want 1; cards=%v", n, cards)
	}
	if first.CS != 2 || first.RS != 2 {
		t.Errorf("stored tree = cs %d rs %d, want original cs 2 rs 2", first.CS, first.RS)
	}
}

// The six opt-in series cards are addable from Edit → Add Card but never part
// of the factory defaults or the system-card merge: POST stores each one as an
// ordinary (non-system) card, and a read of a layout without them does not
// put them back.
func TestDashOptInSeriesCardsStayOutOfDefaults(t *testing.T) {
	optIn := []string{
		"lane-utilization", "throughput-forecast", "verifier-health",
		"release-train", "review-backlog", "defects-per-day",
	}
	for _, id := range defaultDashCards() {
		for _, o := range optIn {
			if id.ID == o {
				t.Fatalf("opt-in card %q must not be a factory default", o)
			}
		}
	}
	s, home := dashTestServer(t)
	writeDashFile(t, home, []dashCard{{ID: "tree", CS: 2, RS: 2, System: true}})
	for _, o := range optIn {
		b, _ := json.Marshal(dashCard{ID: o, CS: 6, RS: 1})
		rr := httptest.NewRecorder()
		s.dashCardsAddHandler(rr, httptest.NewRequest(http.MethodPost, "/api/dashboard/cards", bytes.NewReader(b)))
		if rr.Code != http.StatusOK {
			t.Fatalf("POST %s: want 200, got %d body=%s", o, rr.Code, rr.Body.String())
		}
	}
	cards := readDashFile(t, home)
	for _, o := range optIn {
		n, c := countID(cards, o)
		if n != 1 {
			t.Fatalf("%s count after add = %d, want 1", o, n)
		}
		if c.System {
			t.Errorf("%s stored as a system card; opt-in cards must be removable", o)
		}
	}
	// A read of the stored layout keeps the opt-in cards (no re-add, no dup).
	if n, _ := countID(readDashFile(t, home), "defects-per-day"); n != 1 {
		t.Fatalf("defects-per-day after re-read = %d, want 1", n)
	}
}

// dashboard.hide_inactive_cards round trip: absent key reads its
// default (true), PUT false then GET shows false, saved to disk it
// loads back false; the default restores when the key is absent.
func TestDashboardHideInactiveCardsRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(home, "config.yaml")
	s := &Server{cfg: &config.Config{Git: config.GitConfig{ReleaseCIWindowMinutes: 60}}, cfgPath: cfgPath}

	get := func() (bool, bool) {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d", rr.Code)
		}
		var got struct {
			Dashboard struct {
				HideInactiveCards bool `json:"hide_inactive_cards"`
			} `json:"dashboard"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		return got.Dashboard.HideInactiveCards, true
	}

	put := func(val bool) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"dashboard.hide_inactive_cards": val})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d body=%s", val, rr.Code, rr.Body.String())
		}
	}

	if got, _ := get(); !got {
		t.Fatalf("before any PUT, hide_inactive_cards = %v, want the default true", got)
	}

	put(false)
	if got, _ := get(); got {
		t.Fatalf("after PUT false, hide_inactive_cards = %v, want false", got)
	}
	saved, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load saved config: %v", err)
	}
	if saved.Dashboard.HideInactiveCards == nil || *saved.Dashboard.HideInactiveCards != false {
		t.Fatalf("saved hide_inactive_cards = %v, want explicit false", saved.Dashboard.HideInactiveCards)
	}

	put(true)
	if got, _ := get(); !got {
		t.Fatalf("after PUT true, hide_inactive_cards = %v, want true", got)
	}
}
