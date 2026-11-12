package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// overlapSpec is the spec both the earlier Automaton and each create carry,
// so every term overlaps and the search finds the earlier one.
const overlapSpec = "Search the existing Automata before creating an Automaton so duplicate plans are flagged early."

// overlapFakeAutonomous keeps Automata in memory. Create stores the new one;
// list and get return what is stored, so the real search has something to find.
type overlapFakeAutonomous struct {
	fakeOrchAutonomous
	store map[string]*autonomous.PRD
	order []string
}

func newOverlapFake(seed ...*autonomous.PRD) *overlapFakeAutonomous {
	f := &overlapFakeAutonomous{store: map[string]*autonomous.PRD{}}
	for _, p := range seed {
		f.put(p)
	}
	return f
}

func (f *overlapFakeAutonomous) put(p *autonomous.PRD) {
	f.store[p.ID] = p
	f.order = append(f.order, p.ID)
}

func (f *overlapFakeAutonomous) CreatePRD(spec, projectDir, backend, model, effort string) (any, error) {
	p := &autonomous.PRD{
		ID:         fmt.Sprintf("new-%d", len(f.order)+1),
		Title:      spec,
		Spec:       spec,
		ProjectDir: projectDir,
	}
	f.put(p)
	return p, nil
}

func (f *overlapFakeAutonomous) GetPRD(id string) (any, bool) {
	p, ok := f.store[id]
	if !ok {
		return nil, false
	}
	return p, true
}

func (f *overlapFakeAutonomous) ListPRDs() []any {
	out := make([]any, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.store[id])
	}
	return out
}

// overlapEarlier is the Automaton stored before the create, with the same spec.
func overlapEarlier() *autonomous.PRD {
	return &autonomous.PRD{ID: "prd-old", Title: overlapSpec, Spec: overlapSpec, ProjectDir: "/work/elsewhere"}
}

// overlapRun is one server with its own fake store and audit log, so each
// case starts from the same state.
type overlapRun struct {
	t     *testing.T
	fake  *overlapFakeAutonomous
	s     *Server
	dir   string
	calls int
}

// newOverlapRun builds a server in the given create mode. A nil seam leaves
// the real search in place; a non-nil seam replaces it and is counted.
func newOverlapRun(t *testing.T, mode string, seam overlapCreateSearch) *overlapRun {
	t.Helper()
	run := &overlapRun{t: t, fake: newOverlapFake(overlapEarlier()), dir: t.TempDir()}
	log, err := audit.New(run.dir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })
	run.s = &Server{
		autonomousMgr: run.fake,
		cfg: &config.Config{Autonomous: config.AutonomousConfig{
			Automation: config.AutonomousAutomationConfig{CreateSearchMode: mode},
		}},
	}
	run.s.SetAuditLog(log)
	if seam != nil {
		run.s.overlapSearch = func(p *autonomous.PRD, threshold float64) ([]autonomous.OverlapWarning, error) {
			run.calls++
			return seam(p, threshold)
		}
	}
	return run
}

// overlapProjectDir is the project directory every create names. It does not
// exist, so the registry and branch sources read as unavailable and never
// touch a real repository, and it is the same in every run so replies can be
// compared byte for byte.
const overlapProjectDir = "/nonexistent-overlap-project-dir"

// post creates an Automaton through the route and returns the reply body.
func (r *overlapRun) post() []byte {
	r.t.Helper()
	body := fmt.Sprintf(`{"spec":%q,"project_dir":%q}`, overlapSpec, overlapProjectDir)
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds", strings.NewReader(body))
	rr := httptest.NewRecorder()
	r.s.handleAutonomousPRDs(rr, req)
	if rr.Code != http.StatusOK {
		r.t.Fatalf("create status = %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	return rr.Body.Bytes()
}

// searchEntries returns the audit lines with the create-search action.
func (r *overlapRun) searchEntries() []string {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.dir, "audit.log"))
	if err != nil {
		r.t.Fatalf("read audit.log: %v", err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.Contains(line, `"action":"automaton_create_search"`) {
			out = append(out, line)
		}
	}
	return out
}

// storedJSON is the stored record of id as JSON, the copy the tests compare.
func (r *overlapRun) storedJSON(id string) []byte {
	r.t.Helper()
	b, err := json.Marshal(r.fake.store[id])
	if err != nil {
		r.t.Fatalf("marshal stored %s: %v", id, err)
	}
	return b
}

// TestOverlapCreateOffModeUnchangedNoAudit: off mode runs nothing, so the
// reply is the plain one and no search entry is written.
func TestOverlapCreateOffModeUnchangedNoAudit(t *testing.T) {
	run := newOverlapRun(t, config.CreateSearchModeOff, func(*autonomous.PRD, float64) ([]autonomous.OverlapWarning, error) {
		return nil, nil
	})
	body := run.post()

	if run.calls != 0 {
		t.Errorf("search ran %d times in off mode, want 0", run.calls)
	}
	if bytes.Contains(body, []byte("overlap_warnings")) {
		t.Errorf("off-mode reply carries overlap_warnings: %s", body)
	}
	if n := len(run.searchEntries()); n != 0 {
		t.Errorf("search audit entries = %d, want 0", n)
	}
}

// TestOverlapCreateShadowModeUnchangedOneAuditEntry: shadow mode searches,
// writes one audit entry with the would-be warnings, and leaves the reply
// byte-for-byte what off mode gives.
func TestOverlapCreateShadowModeUnchangedOneAuditEntry(t *testing.T) {
	off := newOverlapRun(t, config.CreateSearchModeOff, nil).post()
	shadow := newOverlapRun(t, config.CreateSearchModeShadow, nil)
	shadowBody := shadow.post()

	if !bytes.Equal(off, shadowBody) {
		t.Errorf("shadow reply differs from off reply:\noff:    %s\nshadow: %s", off, shadowBody)
	}
	entries := shadow.searchEntries()
	if len(entries) != 1 {
		t.Fatalf("search audit entries = %d, want 1: %v", len(entries), entries)
	}
	var e struct {
		Subject string `json:"subject"`
		Details struct {
			Mode     string `json:"mode"`
			Warnings []struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"warnings"`
		} `json:"details"`
	}
	if err := json.Unmarshal([]byte(entries[0]), &e); err != nil {
		t.Fatalf("decode audit entry: %v", err)
	}
	if e.Subject != "new-2" {
		t.Errorf("subject = %q, want the created id new-2", e.Subject)
	}
	if e.Details.Mode != config.CreateSearchModeShadow {
		t.Errorf("mode = %q, want shadow", e.Details.Mode)
	}
	found := false
	for _, w := range e.Details.Warnings {
		if w.Kind == "automaton" && w.ID == "prd-old" {
			found = true
		}
	}
	if !found {
		t.Errorf("would-be warnings do not name prd-old: %+v", e.Details.Warnings)
	}
}

// TestOverlapCreateOnModeCarriesWarnings: on mode adds overlap_warnings to
// the reply, naming the earlier Automaton, and writes no audit entry.
func TestOverlapCreateOnModeCarriesWarnings(t *testing.T) {
	run := newOverlapRun(t, config.CreateSearchModeOn, nil)
	body := run.post()

	var reply struct {
		ID              string `json:"id"`
		OverlapWarnings []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"overlap_warnings"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatalf("decode reply: %v\n%s", err, body)
	}
	if reply.ID != "new-2" {
		t.Errorf("reply id = %q, want new-2", reply.ID)
	}
	found := false
	for _, w := range reply.OverlapWarnings {
		if w.Kind == "automaton" && w.ID == "prd-old" {
			found = true
		}
	}
	if !found {
		t.Errorf("overlap_warnings do not name the earlier Automaton prd-old: %s", body)
	}
	if n := len(run.searchEntries()); n != 0 {
		t.Errorf("on mode wrote %d search audit entries, want 0 (warnings are not persisted)", n)
	}
}

// TestOverlapCreateOnModeNoOverlapIsEmptyList: with nothing to match, the
// on-mode reply still carries overlap_warnings, as an empty list.
func TestOverlapCreateOnModeNoOverlapIsEmptyList(t *testing.T) {
	run := newOverlapRun(t, config.CreateSearchModeOn, func(*autonomous.PRD, float64) ([]autonomous.OverlapWarning, error) {
		return []autonomous.OverlapWarning{}, nil
	})
	body := run.post()

	var reply map[string]json.RawMessage
	if err := json.Unmarshal(body, &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if got := string(reply["overlap_warnings"]); got != "[]" {
		t.Errorf("overlap_warnings = %s, want []", got)
	}
}

// TestOverlapCreateFailingFinderKeepsSuccess: a search that returns an error
// still gives the success reply mode off gives, and the stored Automaton
// equals a copy taken before the search ran.
func TestOverlapCreateFailingFinderKeepsSuccess(t *testing.T) {
	off := newOverlapRun(t, config.CreateSearchModeOff, nil).post()

	var before []byte
	run := newOverlapRun(t, config.CreateSearchModeOn, nil)
	run.s.overlapSearch = func(p *autonomous.PRD, _ float64) ([]autonomous.OverlapWarning, error) {
		before = run.storedJSON(p.ID)
		return nil, errors.New("finder unavailable")
	}
	body := run.post()

	if !bytes.Equal(off, body) {
		t.Errorf("failing finder changed the reply:\noff: %s\ngot: %s", off, body)
	}
	if !bytes.Equal(before, run.storedJSON("new-2")) {
		t.Errorf("stored Automaton changed by the search:\nbefore: %s\nafter:  %s", before, run.storedJSON("new-2"))
	}
}

// TestOverlapCreatePanickingFinderKeepsSuccess: a search that panics is
// recovered; the create still succeeds with the off-mode reply and leaves the
// stored Automaton as it was before the search.
func TestOverlapCreatePanickingFinderKeepsSuccess(t *testing.T) {
	off := newOverlapRun(t, config.CreateSearchModeOff, nil).post()

	var before []byte
	run := newOverlapRun(t, config.CreateSearchModeShadow, nil)
	run.s.overlapSearch = func(p *autonomous.PRD, _ float64) ([]autonomous.OverlapWarning, error) {
		before = run.storedJSON(p.ID)
		panic("finder exploded")
	}
	body := run.post()

	if !bytes.Equal(off, body) {
		t.Errorf("panicking finder changed the reply:\noff: %s\ngot: %s", off, body)
	}
	if !bytes.Equal(before, run.storedJSON("new-2")) {
		t.Errorf("stored Automaton changed by the search:\nbefore: %s\nafter:  %s", before, run.storedJSON("new-2"))
	}
	if n := len(run.searchEntries()); n != 0 {
		t.Errorf("failed search wrote %d audit entries, want 0", n)
	}
}
